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

// dispatchCommand routes a typed line to its handler.
func (m *model) dispatchCommand(line string) {
	parts := strings.Fields(line)
	if len(parts) == 0 {
		return
	}
	sp := m.app.statePath
	go func() {
		if err := AppendCommandHistory(sp, line); err != nil {
			debugLog("AppendCommandHistory: %v", err)
		}
	}()
	switch parts[0] {
	case "help":
		m.handleHelp()
	case "start":
		m.handleStart()
	case "stop":
		m.handleStop()
	case "restart":
		m.handleRestart(parts)
	case "logs":
		m.handleLogs()
	case "status":
		m.handleStatus()
	case "test":
		m.handleTest(parts)
	case "theme":
		m.handleTheme(parts)
	default:
		m.handleCustom(parts)
	}
}

func (m *model) handleHelp() {
	m.vs.overlay = overlayHelp
	m.vs.flipTarget = 1.0
}

func (m *model) handleStart() {
	var initial WizardValues
	if state, err := LoadState(m.app.statePath); err == nil && state.Instance != nil {
		initial = wizardValuesFromState(state.Instance)
	}
	m.vs.overlay = overlayWizard
	m.vs.wizard = newStartWizard(m.app.cfg, m.vs.commandsVP.Width, initial)
	m.vs.flipTarget = 1.0
}

func (m *model) handleStop() {
	name := m.app.instanceName
	workspaceDir := m.app.workspaceDir
	sp := m.app.statePath
	m.printLine("$ stop")
	if name == "" {
		m.printLine("  no active instance — run: start")
		return
	}
	m.vs.steps = map[string]*commandStep{}
	cancelInstance()
	instanceCtx, cancelInstance = context.WithCancel(context.Background())
	m.app.instanceName = ""
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
	for i := len(m.app.activeDefs) - 1; i >= 0; i-- {
		def := m.app.activeDefs[i]
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
			if err := t.step.Stop(context.Background(), name); err != nil {
				prog.Send(stepDoneMsg{id: t.id, ok: false, label: t.label + " failed: " + err.Error()})
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
}

func (m *model) handleRestart(parts []string) {
	m.printLine("$ " + strings.Join(parts, " "))
	if m.app.instanceName == "" {
		m.printLine("  no active instance — run: start")
		return
	}
	if len(parts) < 2 {
		m.printLine("  usage: restart <step-id>")
		return
	}
	id := parts[1]
	def, ok := m.app.findDef(id)
	if !ok {
		m.printLine("  unknown step: " + id)
		return
	}
	name := m.app.instanceName
	sp := m.app.statePath
	if e, exists := m.app.stepCtxs[id]; exists {
		e.cancel()
	}
	if pid, idx, ok := m.app.panelAndIdx(id); ok {
		m.app.panels[pid].bufs[idx] = nil
		if m.app.panels[pid].activeIdx == idx {
			m.vs.panelVPs[pid].SetContent("")
		}
	}
	stepCtx, stepCancel := context.WithCancel(instanceCtx)
	if m.app.stepCtxs == nil {
		m.app.stepCtxs = make(map[string]stepEntry)
	}
	m.app.stepCtxs[id] = stepEntry{ctx: stepCtx, cancel: stepCancel}
	if err := UpdateStepState(sp, id, config.StepStatusPending, nil); err != nil {
		debugLog("restart: UpdateStepState %q pending: %v", id, err)
	}
	if lp := def.Step.LogPath(name); lp != "" {
		if err := os.Truncate(lp, 0); err != nil {
			debugLog("restart: truncate log %q: %v", lp, err)
		}
		go step.WatchStep(stepCtx, def.Step, name)
	}
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
}

func (m *model) handleLogs() {
	m.printLine("$ logs")
	if m.app.instanceName == "" {
		m.printLine("  no active instance — run: start")
		return
	}
	name := m.app.instanceName
	for _, pv := range m.app.panels {
		for _, def := range pv.defs {
			lp := def.Step.LogPath(name)
			if lp != "" {
				m.printLine(fmt.Sprintf("  %-10s %s", def.Step.ID()+":", lp))
			}
		}
	}
}

func (m *model) handleStatus() {
	m.printLine("$ status")
	sp := m.app.statePath
	if m.app.instanceName == "" {
		m.printLine("  no active instance — run: start")
		return
	}
	state, err := LoadState(sp)
	if err != nil || state.Instance == nil || len(state.Instance.StepStates) == 0 {
		m.printLine("  no step status available")
		return
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
	for _, def := range m.app.activeDefs {
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

func (m *model) handleTest(parts []string) {
	m.printLine("$ test")
	sp := m.app.statePath
	if m.app.instanceName == "" {
		m.printLine("  no active instance — run: start")
		return
	}
	if len(m.app.cfg.Tests) == 0 {
		m.printLine("  no tests configured")
		return
	}
	if m.app.testRunning {
		m.printLine("  test already running")
		return
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
			return
		}
	} else if len(m.app.cfg.Tests) == 1 {
		tmpl = &m.app.cfg.Tests[0]
	} else {
		m.printLine("  multiple test suites configured — use: test <label>")
		for _, t := range m.app.cfg.Tests {
			m.printLine("    " + t.Label)
		}
		return
	}
	var values WizardValues
	if state, err := LoadState(sp); err == nil && state.Instance != nil {
		values = wizardValuesFromState(state.Instance)
	}
	tc, err := tmpl.Build(values)
	if err != nil {
		m.printLine("  error building test command: " + err.Error())
		return
	}
	if tc.Cmd == "" {
		m.printLine("  test template returned empty command")
		return
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
}

func (m *model) handleTheme(parts []string) {
	m.printLine("$ " + strings.Join(parts, " "))
	sp := m.app.statePath
	if len(parts) < 2 {
		m.printLine("themes: " + themeNames())
		return
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
			return
		}
	}
	m.printLine("unknown theme: " + name + " (try: " + themeNames() + ")")
}

func (m *model) handleCustom(parts []string) {
	cmdName := parts[0]
	args := parts[1:]
	sp := m.app.statePath
	cmd, ok := m.app.customCmds[cmdName]
	if !ok {
		m.printLine("$ " + strings.Join(parts, " "))
		m.printLine("unknown command: " + cmdName + " (try: help)")
		return
	}
	m.printLine("$ " + strings.Join(parts, " "))
	var values WizardValues
	if state, err := LoadState(sp); err == nil && state.Instance != nil {
		values = wizardValuesFromState(state.Instance)
	}
	instanceName := m.app.instanceName
	go func() {
		prog.Send(cmdActiveMsg(+1))
		err := cmd.Handler(args, instanceName, values)
		prog.Send(cmdActiveMsg(-1))
		if err != nil {
			prog.Send(commandLineMsg(fmt.Sprintf("  error: %v", err)))
		}
	}()
}
