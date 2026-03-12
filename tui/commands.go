package tui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/thompsonja/its_tui/step"
)

// copyToClipboard writes text to the system clipboard by piping to the first
// available clipboard tool: wl-copy (Wayland), xclip, xsel (X11), pbcopy (macOS).
func copyToClipboard(text string) error {
	tools := [][]string{
		{"wl-copy"},
		{"xclip", "-selection", "clipboard"},
		{"xsel", "--clipboard", "--input"},
		{"pbcopy"},
	}
	for _, args := range tools {
		path, err := exec.LookPath(args[0])
		if err != nil {
			continue
		}
		cmd := exec.Command(path, args[1:]...)
		cmd.Stdin = strings.NewReader(text)
		if err := cmd.Run(); err == nil {
			return nil
		}
	}
	return fmt.Errorf("no clipboard tool found (install xclip, xsel, or wl-clipboard)")
}

// debugRuntime returns a human-readable runtime name for a skaffold portName.
func debugRuntime(portName string) string {
	switch portName {
	case "dlv":
		return "dlv/Go"
	case "jvm":
		return "jvm/Java"
	case "ptvsd", "debugpy":
		return "debugpy/Python"
	case "node", "nodejs":
		return "node/Node.js"
	default:
		if portName != "" {
			return portName
		}
		return "unknown"
	}
}

// vscodeLaunchConfig returns the lines of a VSCode launch configuration object
// (without the surrounding braces) for the given debug port.
func vscodeLaunchConfig(p step.DebugPortMsg, addr string) []string {
	name := p.ResourceName
	if name == "" {
		name = fmt.Sprintf("port-%d", p.LocalPort)
	}
	switch p.PortName {
	case "dlv":
		return []string{
			`{`,
			fmt.Sprintf(`  "name": "Attach %s",`, name),
			`  "type": "go",`,
			`  "request": "attach",`,
			`  "mode": "remote",`,
			fmt.Sprintf(`  "port": %d,`, p.LocalPort),
			fmt.Sprintf(`  "host": "%s"`, addr),
		}
	case "jvm":
		return []string{
			`{`,
			fmt.Sprintf(`  "name": "Attach %s",`, name),
			`  "type": "java",`,
			`  "request": "attach",`,
			fmt.Sprintf(`  "hostName": "%s",`, addr),
			fmt.Sprintf(`  "port": %d`, p.LocalPort),
		}
	case "ptvsd", "debugpy":
		return []string{
			`{`,
			fmt.Sprintf(`  "name": "Attach %s",`, name),
			`  "type": "python",`,
			`  "request": "attach",`,
			`  "connect": {`,
			fmt.Sprintf(`    "host": "%s",`, addr),
			fmt.Sprintf(`    "port": %d`, p.LocalPort),
			`  }`,
		}
	case "node", "nodejs":
		return []string{
			`{`,
			fmt.Sprintf(`  "name": "Attach %s",`, name),
			`  "type": "node",`,
			`  "request": "attach",`,
			fmt.Sprintf(`  "address": "%s",`, addr),
			fmt.Sprintf(`  "port": %d,`, p.LocalPort),
			`  "localRoot": "${workspaceFolder}",`,
			`  "remoteRoot": "/app"`,
		}
	default:
		return []string{
			`{`,
			fmt.Sprintf(`  "name": "Attach %s",`, name),
			fmt.Sprintf(`  "port": %d,`, p.LocalPort),
			fmt.Sprintf(`  "address": "%s"`, addr),
		}
	}
}

// wizardValuesFromState reconstructs a WizardValues from a saved InstanceState.
func wizardValuesFromState(inst *InstanceState) WizardValues {
	str := inst.StringValues
	strs := inst.SliceValues
	if str == nil {
		str = map[string]string{}
	}
	if strs == nil {
		strs = map[string][]string{}
	}
	return WizardValues{str: str, strs: strs}
}

// switchToInstance cancels the current instance context, clears panel content,
// and sets the new instance name. Callers are responsible for calling
// registerPipeline and starting watchers / executeStart as needed.
func (m *model) switchToInstance(name string) {
	cancelInstance()
	instanceCtx, cancelInstance = context.WithCancel(context.Background())
	m.instanceName = name
	m.debugPorts = nil
	// Clear panel buffers (defs are reset by a subsequent registerPipeline call).
	for i := range m.panels {
		for j := range m.panels[i].bufs {
			m.panels[i].bufs[j] = nil
		}
		m.panels[i].activeIdx = 0
	}
	for i := range m.panelVPs {
		m.panelVPs[i].SetContent("")
	}
}

// buildPipelineFromState reconstructs the step graph from a saved InstanceState,
// used when restoring a previously-running instance after a restart.
func (m *model) buildPipelineFromState(instanceName string, inst *InstanceState) []StepDef {
	values := wizardValuesFromState(inst)
	defs, _ := m.buildDefsFromTemplates(values)
	return defs
}

// buildDefsFromTemplates builds a StepDef slice from all templates using values.
// On error it returns the error; callers that want best-effort (session restore)
// can ignore it.
func (m *model) buildDefsFromTemplates(values WizardValues) ([]StepDef, error) {
	sp := m.statePath
	var defs []StepDef
	for _, tmpl := range m.cfg.Steps {
		s, err := tmpl.Build(values)
		if err != nil {
			label := tmpl.Label
			if label == "" {
				label = "step"
			}
			return nil, fmt.Errorf("%s: %w", label, err)
		}
		if s == nil {
			continue
		}
		label := tmpl.Label
		if tmpl.LabelFunc != nil {
			label = tmpl.LabelFunc(values)
		}
		var onReady func()
		if tmpl.OnReady != nil {
			fn := tmpl.OnReady
			onReady = func() { fn(sp) }
		}
		defs = append(defs, StepDef{
			Step:         s,
			Panel:        tmpl.Panel,
			Label:        label,
			WaitFor:      tmpl.WaitFor,
			AutoActivate: tmpl.AutoActivate,
			Hidden:       tmpl.Hidden,
			OnReady:      onReady,
		})
	}
	return defs, nil
}

// dispatchCommand routes typed text to internal command handlers.
func (m *model) dispatchCommand(line string) {
	parts := strings.Fields(line)
	if len(parts) == 0 {
		return
	}
	sp := m.statePath
	go func() { _ = AppendCommandHistory(sp, line) }()
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
		// Collect stop tasks from templates in reverse order (last-in, first-out).
		type stopTask struct {
			id    string
			label string
			fn    func(context.Context, string)
		}
		var stopTasks []stopTask
		for i := len(m.cfg.Steps) - 1; i >= 0; i-- {
			tmpl := m.cfg.Steps[i]
			if tmpl.StopFunc == nil {
				continue
			}
			label := tmpl.StopLabel
			if label == "" {
				l := tmpl.Label
				if l == "" {
					l = "step"
				}
				label = "stopping " + l
			}
			stopTasks = append(stopTasks, stopTask{
				id:    fmt.Sprintf("stop-%d", i),
				label: label,
				fn:    tmpl.StopFunc,
			})
		}
		m.startStep("mfe-stop", "stopping MFE")
		for _, t := range stopTasks {
			m.startStep(t.id, t.label)
		}
		go func() {
			prog.Send(cmdActiveMsg(+1))
			// Kill MFE process group (state-based; runs before template StopFuncs).
			if state, err := LoadState(sp); err == nil && state.Instance != nil {
				step.KillProcessGroup(state.Instance.MFEPGID)
			}
			prog.Send(stepDoneMsg{id: "mfe-stop", ok: true, label: "MFE stopped"})
			// Run template stop functions in reverse template order.
			for _, t := range stopTasks {
				t.fn(context.Background(), name)
				prog.Send(stepDoneMsg{id: t.id, ok: true, label: t.label + " done"})
			}
			prog.Send(cmdActiveMsg(-1))
			_ = MarkInactive(sp)
			prog.Send(instanceStoppedMsg{})
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
		if pid, idx := m.panelAndIdx(id); idx >= 0 {
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
		// Truncate log file if any.
		if lp := def.Step.LogPath(name); lp != "" {
			_ = os.Truncate(lp, 0)
			go watchStep(stepCtx, def, name)
		}
		// Re-register spinner and start the step.
		m.startStep(id, def.effectiveLabel())
		go func() {
			if err := def.Step.Start(stepCtx, name); err != nil {
				prog.Send(stepDoneMsg{id: id, ok: false, label: def.effectiveLabel() + " failed: " + err.Error()})
				return
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
					m.helpOverlayVP.SetContent(helpContent(m.helpOverlayVP.Width))
					m.printLine("theme set to: " + name)
					found = true
					go func() { _ = SaveTheme(sp, name) }()
					break
				}
			}
			if !found {
				m.printLine("unknown theme: " + name + " (try: " + themeNames() + ")")
			}
		}

	default:
		m.printLine("$ " + line)
		m.printLine("unknown command: " + parts[0] + " (try: help)")
	}
}

func (m *model) executeStartFromWizard() {
	wiz := m.wizard
	sp := m.statePath
	name := m.configuredName()

	m.printLine("$ start")
	values := wiz.buildValues()
	defs, err := m.buildDefsFromTemplates(values)
	if err != nil {
		prog.Send(commandLineMsg("error: " + err.Error()))
		return
	}

	// Persist wizard selections.
	go func() {
		_ = SaveInstanceState(sp, InstanceState{
			StringValues: values.str,
			SliceValues:  values.strs,
		})
	}()

	m.switchToInstance(name)
	m.registerPipeline(defs)
	m.fullscreenTarget = 0

	// Truncate log files before executeStart (which creates per-step contexts).
	for _, def := range defs {
		if lp := def.Step.LogPath(name); lp != "" {
			_ = os.Truncate(lp, 0)
		}
	}

	// executeStart creates per-step contexts in m.stepCtxs.
	m.executeStart(defs)

	// Start watchers using the per-step contexts created by executeStart.
	for _, def := range defs {
		id := def.Step.ID()
		if e, ok := m.stepCtxs[id]; ok {
			go watchStep(e.ctx, def, name)
		}
	}
}

// executeStart launches all step processes with dependency ordering.
// Steps with WaitFor set block until their dependency signals ready.
func (m *model) executeStart(defs []StepDef) {
	m.steps = map[string]*commandStep{}
	m.stepCtxs = make(map[string]stepEntry)
	name := m.instanceName

	// Create per-step contexts and build a ready channel for each step.
	ready := make(map[string]chan struct{}, len(defs))
	for _, def := range defs {
		id := def.Step.ID()
		ready[id] = make(chan struct{})
		stepCtx, stepCancel := context.WithCancel(instanceCtx)
		m.stepCtxs[id] = stepEntry{ctx: stepCtx, cancel: stepCancel}
	}

	// Register visible steps in the commands panel tracker.
	for _, def := range defs {
		if def.Hidden {
			continue
		}
		label := def.effectiveLabel()
		if len(def.WaitFor) == 0 {
			m.startStep(def.Step.ID(), label)
		} else {
			m.startPendingStep(def.Step.ID(), label, def.WaitFor)
		}
	}

	for _, def := range defs {
		def := def
		id := def.Step.ID()
		stepCtx := m.stepCtxs[id].ctx
		go func() {
			// Wait for all dependencies in parallel, crossing each off as it completes.
			if len(def.WaitFor) > 0 {
				remaining := make(chan struct{}, len(def.WaitFor))
				for _, dep := range def.WaitFor {
					dep := dep
					go func() {
						if ch, ok := ready[dep]; ok {
							select {
							case <-ch:
								prog.Send(stepDepReadyMsg{id: id, dep: dep})
								remaining <- struct{}{}
							case <-instanceCtx.Done():
							}
						} else {
							remaining <- struct{}{}
						}
					}()
				}
				for range def.WaitFor {
					select {
					case <-remaining:
					case <-instanceCtx.Done():
						return
					}
				}
				// Activate this step (triggers spinner + AutoActivate if set).
				prog.Send(stepActivateMsg{id: id})
			}

			// Start the step.
			if err := def.Step.Start(stepCtx, name); err != nil {
				if !def.Hidden {
					prog.Send(stepDoneMsg{
						id:    id,
						ok:    false,
						label: def.effectiveLabel() + " failed: " + err.Error(),
					})
				}
				return
			}

			// Signal ready to unblock any dependents.
			close(ready[id])

			// Invoke the OnReady callback.
			if def.OnReady != nil {
				go def.OnReady()
			}

			if !def.Hidden {
				prog.Send(stepDoneMsg{
					id:    id,
					ok:    true,
					label: def.effectiveLabel() + " running",
				})
			}
		}()
	}
}
