package tui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/thompsonja/its_tui/config"
	"github.com/thompsonja/its_tui/step"
)

// dispatchCommand routes typed text to internal command handlers.
func (m *model) dispatchCommand(line string) {
	parts := strings.Fields(line)
	if len(parts) == 0 {
		return
	}
	sp := m.statePath
	go func() {
		if err := AppendCommandHistory(sp, line); err != nil {
			debugLog("AppendCommandHistory: %v", err)
		}
	}()
	switch parts[0] {
	case "help":
		m.overlay = overlayHelp
		m.flipTarget = 1.0

	case "start":
		// Pre-populate the wizard from the last saved session, if any.
		var initial WizardValues
		if state, err := LoadState(sp); err == nil && state.Instance != nil {
			initial = wizardValuesFromState(state.Instance)
		}
		m.overlay = overlayWizard
		m.wizard = newStartWizard(m, initial)
		m.flipTarget = 1.0

	case "stop":
		name := m.instanceName
		workspaceDir := m.workspaceDir
		m.printLine("$ stop")
		if name == "" {
			m.printLine("  no active instance — run: start")
			break
		}
		m.steps = map[string]*commandStep{}
		cancelInstance()
		instanceCtx, cancelInstance = context.WithCancel(context.Background())
		m.instanceName = ""
		// Clear panel bufs (keep defs so stop output routes correctly).
		for i := range m.panels {
			for j := range m.panels[i].bufs {
				m.panels[i].bufs[j] = nil
			}
			m.panels[i].activeIdx = 0
		}
		cancelTest()
		m.testBuf = nil
		m.testRunning = false
		m.testVP.SetContent("")
		m.debugPorts = nil
		m.fwdPorts = nil
		for i := range m.panelVPs {
			m.panelVPs[i].SetContent("")
		}
		// Collect stop tasks from active defs in reverse order (last-in, first-out).
		type stopTask struct {
			id    string
			label string
			step  Step
		}
		var stopTasks []stopTask
		for i := len(m.activeDefs) - 1; i >= 0; i-- {
			def := m.activeDefs[i]
			label := "stopping " + def.meta.label
			if def.meta.label == "" {
				label = "stopping step"
			}
			stopTasks = append(stopTasks, stopTask{
				id:    fmt.Sprintf("stop-%d", i),
				label: label,
				step:  def.Step,
			})
		}
		// Check if MFE is running before adding it to the stop list.
		var mfePGID int
		if state, err := LoadState(sp); err == nil && state.Instance != nil {
			mfePGID = state.Instance.MFEPGID
		}
		if mfePGID > 0 {
			m.startStep("mfe-stop", "stopping MFE")
		}
		for _, t := range stopTasks {
			m.startStep(t.id, t.label)
		}
		go func() {
			prog.Send(cmdActiveMsg(+1))
			// Kill MFE process group if running.
			if mfePGID > 0 {
				step.KillProcessGroup(mfePGID)
				prog.Send(stepDoneMsg{id: "mfe-stop", ok: true, label: "MFE stopped"})
			}
			// Stop steps in reverse order.
			for _, t := range stopTasks {
				if err := t.step.Stop(context.Background(), name); err != nil {
					prog.Send(stepDoneMsg{id: t.id, ok: false, label: t.label + " failed: " + err.Error()})
				} else {
					prog.Send(stepDoneMsg{id: t.id, ok: true, label: t.label + " done"})
				}
			}
			// Remove TUI-managed debug configs from .vscode/launch.json.
			if err := removeVSCodeLaunchConfigs(workspaceDir, name); err != nil {
				prog.Send(commandLineMsg(fmt.Sprintf("  warning: failed to update .vscode/launch.json: %v", err)))
			}
			prog.Send(cmdActiveMsg(-1))
			if err := MarkInactive(sp); err != nil {
				debugLog("MarkInactive: %v", err)
			}
			prog.Send(instanceStoppedMsg{})
			prog.Send(clearActiveDefsMsg{})
		}()

	case "restart":
		m.printLine("$ " + strings.Join(parts, " "))
		if m.instanceName == "" {
			m.printLine("  no active instance — run: start")
			break
		}
		if len(parts) < 2 {
			m.printLine("  usage: restart <step-id>")
			break
		}
		id := parts[1]
		def, ok := m.findDef(id)
		if !ok {
			m.printLine("  unknown step: " + id)
			break
		}
		name := m.instanceName
		// Cancel the existing step context.
		if e, exists := m.stepCtxs[id]; exists {
			e.cancel()
		}
		// Clear the panel buffer for this step.
		if pid, idx, ok := m.panelAndIdx(id); ok {
			m.panels[pid].bufs[idx] = nil
			if m.panels[pid].activeIdx == idx {
				m.panelVPs[pid].SetContent("")
			}
		}
		// Create a new per-step context.
		stepCtx, stepCancel := context.WithCancel(instanceCtx)
		if m.stepCtxs == nil {
			m.stepCtxs = make(map[string]stepEntry)
		}
		m.stepCtxs[id] = stepEntry{ctx: stepCtx, cancel: stepCancel}
		// Clear step state before restart
		if err := UpdateStepState(sp, id, config.StepStatusPending, nil); err != nil {
			debugLog("restart: UpdateStepState %q pending: %v", id, err)
		}
		// Truncate log file if any.
		if lp := def.Step.LogPath(name); lp != "" {
			if err := os.Truncate(lp, 0); err != nil {
				debugLog("restart: truncate log %q: %v", lp, err)
			}
			go step.WatchStep(stepCtx, def.Step, name)
		}
		// Re-register spinner and start the step.
		m.startStep(id, def.effectiveLabel())
		go func() {
			if err := UpdateStepState(sp, id, config.StepStatusRunning, nil); err != nil {
				debugLog("restart: UpdateStepState %q running: %v", id, err)
			}
			if err := def.Step.Start(stepCtx, name); err != nil {
				if stateErr := UpdateStepState(sp, id, config.StepStatusFailed, err); stateErr != nil {
					debugLog("restart: UpdateStepState %q failed: %v", id, stateErr)
				}
				prog.Send(stepDoneMsg{id: id, ok: false, label: def.effectiveLabel() + " failed: " + err.Error()})
				return
			}
			if err := UpdateStepState(sp, id, config.StepStatusCompleted, nil); err != nil {
				debugLog("restart: UpdateStepState %q completed: %v", id, err)
			}
			prog.Send(stepDoneMsg{id: id, ok: true, label: def.effectiveLabel() + " running"})
		}()

	case "logs":
		m.printLine("$ logs")
		if m.instanceName == "" {
			m.printLine("  no active instance — run: start")
		} else {
			name := m.instanceName
			for _, pv := range m.panels {
				for _, def := range pv.defs {
					lp := def.Step.LogPath(name)
					if lp != "" {
						m.printLine(fmt.Sprintf("  %-10s %s", def.Step.ID()+":", lp))
					}
				}
			}
		}

	case "status":
		m.printLine("$ status")
		if m.instanceName == "" {
			m.printLine("  no active instance — run: start")
		} else {
			// Load step states from the state file
			state, err := LoadState(sp)
			if err != nil || state.Instance == nil || len(state.Instance.StepStates) == 0 {
				m.printLine("  no step status available")
			} else {
				// Display startup time only if startup completed
				if state.Instance.StartedAt != "" && state.Instance.ReadyAt != "" {
					started, err1 := time.Parse(time.RFC3339, state.Instance.StartedAt)
					ready, err2 := time.Parse(time.RFC3339, state.Instance.ReadyAt)
					if err1 == nil && err2 == nil {
						duration := ready.Sub(started)
						m.printLine(fmt.Sprintf("  Instance startup took: %s", duration.Round(time.Millisecond)))
						m.printLine("")
					}
				}

				m.printLine("  Step Status:")
				// Show steps in order from the active defs
				for _, def := range m.activeDefs {
					id := def.Step.ID()
					if ss, ok := state.Instance.StepStates[id]; ok {
						statusIcon := "○"
						switch ss.Status {
						case config.StepStatusCompleted:
							statusIcon = "✓"
						case config.StepStatusRunning:
							statusIcon = "▶"
						case config.StepStatusFailed:
							statusIcon = "✗"
						case config.StepStatusSkipped:
							statusIcon = "⊘"
						case config.StepStatusPending:
							statusIcon = "○"
						}
						label := def.effectiveLabel()
						statusLine := fmt.Sprintf("  %s %-15s %s", statusIcon, label, ss.Status)
						if ss.Error != "" {
							statusLine += fmt.Sprintf(" - %s", ss.Error)
						}
						m.printLine(statusLine)
					}
				}
			}
		}

	case "test":
		m.printLine("$ test")
		if m.instanceName == "" {
			m.printLine("  no active instance — run: start")
			break
		}
		if len(m.cfg.Tests) == 0 {
			m.printLine("  no tests configured")
			break
		}
		if m.testRunning {
			m.printLine("  test already running")
			break
		}
		// Resolve which test template to run.
		var tmpl *TestTemplate
		if len(parts) > 1 {
			label := strings.Join(parts[1:], " ")
			for i := range m.cfg.Tests {
				if m.cfg.Tests[i].Label == label {
					tmpl = &m.cfg.Tests[i]
					break
				}
			}
			if tmpl == nil {
				m.printLine("  unknown test: " + label)
				m.printLine("  available:")
				for _, t := range m.cfg.Tests {
					m.printLine("    " + t.Label)
				}
				break
			}
		} else if len(m.cfg.Tests) == 1 {
			tmpl = &m.cfg.Tests[0]
		} else {
			m.printLine("  multiple test suites configured — use: test <label>")
			for _, t := range m.cfg.Tests {
				m.printLine("    " + t.Label)
			}
			break
		}
		// Build wizard values from saved state (same selections used at start).
		var values WizardValues
		if state, err := LoadState(sp); err == nil && state.Instance != nil {
			values = wizardValuesFromState(state.Instance)
		}
		tc, err := tmpl.Build(values)
		if err != nil {
			m.printLine("  error building test command: " + err.Error())
			break
		}
		if tc.Cmd == "" {
			m.printLine("  test template returned empty command")
			break
		}
		// Switch BottomRight to the Tests tab and clear previous output.
		m.testBuf = nil
		m.testVP.SetContent("")
		m.panels[PanelBottomRight].activeIdx = len(m.panels[PanelBottomRight].defs)
		m.testRunning = true
		cancelTest()
		var testCtx context.Context
		testCtx, cancelTest = context.WithCancel(instanceCtx)
		label := tmpl.Label
		go func() {
			cmd := exec.Command(tc.Cmd, tc.Args...)
			cmd.Dir = tc.Dir
			if len(tc.Env) > 0 {
				cmd.Env = os.Environ()
				for k, v := range tc.Env {
					cmd.Env = append(cmd.Env, k+"="+v)
				}
			}
			ok := true
			step.StreamCmd(testCtx, cmd, func(line string) {
				if strings.HasPrefix(line, "[exited:") {
					ok = false
				}
				prog.Send(testLineMsg(line))
			})
			if testCtx.Err() != nil {
				prog.Send(testLineMsg("  [" + label + " cancelled]"))
				prog.Send(testDoneMsg{ok: false})
				return
			}
			prog.Send(testDoneMsg{ok: ok})
		}()

	case "theme":
		m.printLine("$ " + line)
		if len(parts) < 2 {
			m.printLine("themes: " + themeNames())
		} else {
			name := parts[1]
			found := false
			for _, t := range presets {
				if t.Name == name {
					currentTheme = t
					m.helpOverlayVP.SetContent(m.helpContent(m.helpOverlayVP.Width))
					m.printLine("theme set to: " + name)
					found = true
					go func() {
					if err := SaveTheme(sp, name); err != nil {
						debugLog("SaveTheme %q: %v", name, err)
					}
				}()
					break
				}
			}
			if !found {
				m.printLine("unknown theme: " + name + " (try: " + themeNames() + ")")
			}
		}

	default:
		cmdName := parts[0]
		args := parts[1:]

		// Try custom commands
		if cmd, ok := m.customCommands[cmdName]; ok {
			m.printLine("$ " + line)

			// Get current wizard values for the handler
			var values WizardValues
			if state, err := LoadState(sp); err == nil && state.Instance != nil {
				values = wizardValuesFromState(state.Instance)
			}

			// Execute handler in goroutine (don't block UI)
			go func() {
				prog.Send(cmdActiveMsg(+1))
				err := cmd.Handler(args, m.instanceName, values)
				prog.Send(cmdActiveMsg(-1))

				if err != nil {
					prog.Send(commandLineMsg(fmt.Sprintf("  error: %v", err)))
				}
			}()
		} else {
			m.printLine("$ " + line)
			m.printLine("unknown command: " + cmdName + " (try: help)")
		}
	}
}
