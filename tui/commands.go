package tui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/thompsonja/its_tui/config"
	"github.com/thompsonja/its_tui/step"
)

// dispatchCommand routes a typed line to its handler and returns any resulting tea.Cmd.
func (m *model) dispatchCommand(line string) tea.Cmd {
	parts := strings.Fields(line)
	if len(parts) == 0 {
		return nil
	}
	sp := m.app.statePath
	go func() {
		if err := AppendCommandHistory(sp, line); err != nil {
			debugLog("AppendCommandHistory: %v", err)
		}
	}()
	var cmds []tea.Cmd
	switch parts[0] {
	case "help":
		cmds = append(cmds, m.handleHelp())
	case "start":
		cmds = append(cmds, m.handleStart())
	case "stop":
		cmds = append(cmds, m.handleStop())
	case "restart":
		cmds = append(cmds, m.handleRestart(parts))
	case "logs":
		cmds = append(cmds, m.handleLogs())
	case "status":
		cmds = append(cmds, m.handleStatus())
	case "test":
		cmds = append(cmds, m.handleTest(parts))
	case "theme":
		cmds = append(cmds, m.handleTheme(parts))
	default:
		cmds = append(cmds, m.handleCustom(parts))
	}
	// Filter nils before batching.
	var nonNil []tea.Cmd
	for _, c := range cmds {
		if c != nil {
			nonNil = append(nonNil, c)
		}
	}
	if len(nonNil) == 0 {
		return nil
	}
	return tea.Batch(nonNil...)
}

func (m *model) handleHelp() tea.Cmd {
	m.app.overlay = overlayHelp
	m.vs.flipTarget = 1.0
	return nil
}

func (m *model) handleStart() tea.Cmd {
	var initial WizardValues
	if state, err := LoadState(m.app.statePath); err == nil && state.Instance != nil {
		initial = wizardValuesFromState(state.Instance)
	}
	m.app.overlay = overlayWizard
	m.app.wizard = newStartWizard(m.app.cfg, m.vs.commandsVP.Width, initial)
	m.vs.flipTarget = 1.0
	return nil
}

func (m *model) handleStop() tea.Cmd {
	name := m.app.inst.name
	workspaceDir := m.app.workspaceDir
	sp := m.app.statePath
	m.printLine("$ stop")
	if name == "" {
		m.printLine("  no active instance — run: start")
		return nil
	}
	m.snapshotHistoricalBars()
	m.vs.steps = map[string]*commandStep{}
	m.vs.stepLabelWidth = 0
	m.app.inst.startedAt = time.Now()
	cancelInstance()
	instanceCtx, cancelInstance = context.WithCancel(context.Background())
	m.app.inst.name = ""
	for i := range m.app.panels {
		for j := range m.app.panels[i].bufs {
			m.app.panels[i].bufs[j] = nil
		}
		m.app.panels[i].activeIdx = 0
	}
	cancelTest()
	m.app.testBuf = nil
	m.app.testRunning = false
	m.vs.testVP.SetContent("")
	m.app.debugPorts = nil
	m.app.fwdPorts = nil
	for i := range m.vs.panelVPs {
		m.vs.panelVPs[i].SetContent("")
	}

	type stopTask struct {
		id    string
		label string
		step  Step
	}
	var stopTasks []stopTask
	for i := len(m.app.inst.activeDefs) - 1; i >= 0; i-- {
		def := m.app.inst.activeDefs[i]
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
	var mfePGID int
	if state, err := LoadState(sp); err == nil && state.Instance != nil {
		mfePGID = state.Instance.MFEPGID
	}

	// Compute aligned label width for stop steps.
	maxStopLabelW := 0
	if mfePGID > 0 {
		if w := len([]rune("stopping MFE")); w > maxStopLabelW {
			maxStopLabelW = w
		}
	}
	for _, t := range stopTasks {
		if w := len([]rune(t.label)); w > maxStopLabelW {
			maxStopLabelW = w
		}
	}
	m.vs.stepLabelWidth = maxStopLabelW

	if mfePGID > 0 {
		m.startStep("mfe-stop", "stopping MFE")
	}
	for _, t := range stopTasks {
		m.startStep(t.id, t.label)
	}
	go func() {
		prog.Send(cmdActiveMsg(+1))
		if mfePGID > 0 {
			step.KillProcessGroup(mfePGID)
			prog.Send(stepDoneMsg{id: "mfe-stop", ok: true, label: "MFE stopped"})
		}
		for _, t := range stopTasks {
			if stopper, ok := t.step.(step.Stopper); ok {
				if err := stopper.Stop(context.Background(), name); err != nil {
					prog.Send(stepDoneMsg{id: t.id, ok: false, label: t.label + " failed: " + err.Error()})
				} else {
					prog.Send(stepDoneMsg{id: t.id, ok: true, label: t.label + " done"})
				}
			} else {
				prog.Send(stepDoneMsg{id: t.id, ok: true, label: t.label + " done"})
			}
		}
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
	return nil
}

func (m *model) handleRestart(parts []string) tea.Cmd {
	m.printLine("$ " + strings.Join(parts, " "))
	if m.app.inst.name == "" {
		m.printLine("  no active instance — run: start")
		return nil
	}
	if len(parts) < 2 {
		m.printLine("  usage: restart <step-id>")
		return nil
	}
	id := parts[1]
	def, ok := m.app.findDef(id)
	if !ok {
		m.printLine("  unknown step: " + id)
		return nil
	}
	name := m.app.inst.name
	sp := m.app.statePath
	if e, exists := m.app.inst.stepCtxs[id]; exists {
		e.cancel()
	}
	if pid, idx, ok := m.app.panelAndIdx(id); ok {
		m.app.panels[pid].bufs[idx] = nil
		if m.app.panels[pid].activeIdx == idx {
			m.vs.panelVPs[pid].SetContent("")
		}
	}
	stepCtx, stepCancel := context.WithCancel(instanceCtx)
	if m.app.inst.stepCtxs == nil {
		m.app.inst.stepCtxs = make(map[string]stepEntry)
	}
	m.app.inst.stepCtxs[id] = stepEntry{ctx: stepCtx, cancel: stepCancel}
	if err := UpdateStepState(sp, id, config.StepStatusPending, nil); err != nil {
		debugLog("restart: UpdateStepState %q pending: %v", id, err)
	}
	if lp := def.Step.LogPath(name); lp != "" {
		if err := os.Truncate(lp, 0); err != nil {
			debugLog("restart: truncate log %q: %v", lp, err)
		}
		go step.WatchStep(stepCtx, def.Step, name)
	}
	label := def.effectiveLabel()
	if w := len([]rune(label)); w > m.vs.stepLabelWidth {
		m.vs.stepLabelWidth = w
	}
	m.startStep(id, label)
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
	return nil
}

func (m *model) handleLogs() tea.Cmd {
	m.printLine("$ logs")
	if m.app.inst.name == "" {
		m.printLine("  no active instance — run: start")
		return nil
	}
	name := m.app.inst.name
	for _, pv := range m.app.panels {
		for _, def := range pv.defs {
			lp := def.Step.LogPath(name)
			if lp != "" {
				m.printLine(fmt.Sprintf("  %-10s %s", def.Step.ID()+":", lp))
			}
		}
	}
	return nil
}

func (m *model) handleStatus() tea.Cmd {
	m.printLine("$ status")
	sp := m.app.statePath
	if m.app.inst.name == "" {
		m.printLine("  no active instance — run: start")
		return nil
	}
	state, err := LoadState(sp)
	if err != nil || state.Instance == nil || len(state.Instance.StepStates) == 0 {
		m.printLine("  no step status available")
		return nil
	}
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
	for _, def := range m.app.inst.activeDefs {
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
	return nil
}

func (m *model) handleTest(parts []string) tea.Cmd {
	m.printLine("$ test")
	sp := m.app.statePath
	if m.app.inst.name == "" {
		m.printLine("  no active instance — run: start")
		return nil
	}
	if len(m.app.cfg.Tests) == 0 {
		m.printLine("  no tests configured")
		return nil
	}
	if m.app.testRunning {
		m.printLine("  test already running")
		return nil
	}
	var tmpl *TestTemplate
	if len(parts) > 1 {
		label := strings.Join(parts[1:], " ")
		for i := range m.app.cfg.Tests {
			if m.app.cfg.Tests[i].Label == label {
				tmpl = &m.app.cfg.Tests[i]
				break
			}
		}
		if tmpl == nil {
			m.printLine("  unknown test: " + label)
			m.printLine("  available:")
			for _, t := range m.app.cfg.Tests {
				m.printLine("    " + t.Label)
			}
			return nil
		}
	} else if len(m.app.cfg.Tests) == 1 {
		tmpl = &m.app.cfg.Tests[0]
	} else {
		m.printLine("  multiple test suites configured — use: test <label>")
		for _, t := range m.app.cfg.Tests {
			m.printLine("    " + t.Label)
		}
		return nil
	}
	var values WizardValues
	if state, err := LoadState(sp); err == nil && state.Instance != nil {
		values = wizardValuesFromState(state.Instance)
	}
	tc, err := tmpl.Build(values)
	if err != nil {
		m.printLine("  error building test command: " + err.Error())
		return nil
	}
	if tc.Cmd == "" {
		m.printLine("  test template returned empty command")
		return nil
	}
	m.app.testBuf = nil
	m.vs.testVP.SetContent("")
	m.app.panels[PanelBottomRight].activeIdx = len(m.app.panels[PanelBottomRight].defs)
	m.app.testRunning = true
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
	return nil
}

func (m *model) handleTheme(parts []string) tea.Cmd {
	m.printLine("$ " + strings.Join(parts, " "))
	sp := m.app.statePath
	if len(parts) < 2 {
		m.printLine("themes: " + themeNames())
		return nil
	}
	name := parts[1]
	for _, t := range presets {
		if t.Name == name {
			currentTheme = t
			m.vs.helpOverlayVP.SetContent(m.vs.helpContent(m.vs.helpOverlayVP.Width, m.app.customCmds))
			m.printLine("theme set to: " + name)
			go func() {
				if err := SaveTheme(sp, name); err != nil {
					debugLog("SaveTheme %q: %v", name, err)
				}
			}()
			return nil
		}
	}
	m.printLine("unknown theme: " + name + " (try: " + themeNames() + ")")
	return nil
}

func (m *model) handleCustom(parts []string) tea.Cmd {
	cmdName := parts[0]
	args := parts[1:]
	sp := m.app.statePath
	cmd, ok := m.app.customCmds[cmdName]
	if !ok {
		m.printLine("$ " + strings.Join(parts, " "))
		m.printLine("unknown command: " + cmdName + " (try: help)")
		return nil
	}
	m.printLine("$ " + strings.Join(parts, " "))
	var values WizardValues
	if state, err := LoadState(sp); err == nil && state.Instance != nil {
		values = wizardValuesFromState(state.Instance)
	}
	instanceName := m.app.inst.name
	go func() {
		prog.Send(cmdActiveMsg(+1))
		err := cmd.Handler(args, instanceName, values)
		prog.Send(cmdActiveMsg(-1))
		if err != nil {
			prog.Send(commandLineMsg(fmt.Sprintf("  error: %v", err)))
		}
	}()
	return nil
}
