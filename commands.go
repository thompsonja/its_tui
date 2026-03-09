package tui

import (
	"context"
	"fmt"
	"os"
	"strings"
	"tui/step"
)

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

// switchToInstance cancels the current instance context, clears panel content,
// and sets the new instance name. Callers are responsible for calling
// registerPipeline and starting watchers / executeStart as needed.
func (m *model) switchToInstance(name string) {
	cancelInstance()
	instanceCtx, cancelInstance = context.WithCancel(context.Background())
	m.instance.Name = name
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
	str := inst.StringValues
	strs := inst.SliceValues
	if str == nil {
		str = map[string]string{}
	}
	if strs == nil {
		strs = map[string][]string{}
	}
	values := WizardValues{str: str, strs: strs}
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
			str := state.Instance.StringValues
			strs := state.Instance.SliceValues
			if str == nil {
				str = map[string]string{}
			}
			if strs == nil {
				strs = map[string][]string{}
			}
			initial = WizardValues{str: str, strs: strs}
		}
		m.overlay = overlayWizard
		m.wizard = newStartWizard(m, initial)
		m.flipTarget = 1.0

	case "stop":
		name := m.instance.Name
		m.printLine("$ stop")
		if name == "" {
			m.printLine("  no active instance")
			break
		}
		m.steps = map[string]*commandStep{}
		cancelInstance()
		instanceCtx, cancelInstance = context.WithCancel(context.Background())
		m.instance.Name = ""
		// Clear panel bufs (keep defs so stop output routes correctly).
		for i := range m.panels {
			for j := range m.panels[i].bufs {
				m.panels[i].bufs[j] = nil
			}
			m.panels[i].activeIdx = 0
		}
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

	case "logs":
		m.printLine("$ logs")
		if m.instance.Name == "" {
			m.printLine("  no active instance")
		} else {
			name := m.instance.Name
			for _, pv := range m.panels {
				for _, def := range pv.defs {
					lp := def.Step.LogPath(name)
					if lp != "" {
						m.printLine(fmt.Sprintf("  %-10s %s", def.Step.ID()+":", lp))
					}
				}
			}
		}

	case "ports":
		m.printLine("$ ports")
		if m.instance.Name == "" {
			m.printLine("  no active instance")
			break
		}
		if len(m.debugPorts) == 0 {
			m.printLine("  no debug ports (is skaffold running in debug mode?)")
			break
		}
		m.printLine("  Debug ports:")
		for _, p := range m.debugPorts {
			addr := p.Address
			if addr == "" {
				addr = "127.0.0.1"
			}
			m.printLine(fmt.Sprintf("    %-24s %s:%d  (%s)", p.ResourceName, addr, p.LocalPort, debugRuntime(p.PortName)))
		}
		m.printLine("")
		m.printLine("  VSCode launch.json:")
		m.printLine(`    {`)
		m.printLine(`      "version": "0.2.0",`)
		m.printLine(`      "configurations": [`)
		for i, p := range m.debugPorts {
			addr := p.Address
			if addr == "" {
				addr = "127.0.0.1"
			}
			comma := ","
			if i == len(m.debugPorts)-1 {
				comma = ""
			}
			for _, line := range vscodeLaunchConfig(p, addr) {
				m.printLine("        " + line)
			}
			m.printLine("      }" + comma)
		}
		m.printLine(`      ]`)
		m.printLine(`    }`)

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
	name := m.instanceName()

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
	ctx := instanceCtx
	for _, def := range defs {
		// Truncate any existing log file before starting the watcher so that
		// tail -F doesn't replay old content before Start creates a fresh one.
		if lp := def.Step.LogPath(name); lp != "" {
			_ = os.Truncate(lp, 0)
		}
		go watchStep(ctx, def, name)
	}
	m.executeStart(defs)
}

// executeStart launches all step processes with dependency ordering.
// Steps with WaitFor set block until their dependency signals ready.
func (m *model) executeStart(defs []StepDef) {
	m.steps = map[string]*commandStep{}
	name := m.instance.Name

	// Build a ready channel for each step.
	ready := make(map[string]chan struct{}, len(defs))
	for _, def := range defs {
		ready[def.Step.ID()] = make(chan struct{})
	}

	// Register visible steps in the commands panel tracker.
	for _, def := range defs {
		if def.Hidden {
			continue
		}
		label := def.effectiveLabel()
		if def.WaitFor == "" {
			m.startStep(def.Step.ID(), label)
		} else {
			m.startPendingStep(def.Step.ID(), label+" (waiting for "+def.WaitFor+")")
		}
	}

	ctx := instanceCtx
	for _, def := range defs {
		def := def
		go func() {
			// Wait for dependency if any.
			if def.WaitFor != "" {
				if ch, ok := ready[def.WaitFor]; ok {
					select {
					case <-ch:
					case <-ctx.Done():
						return
					}
				}
				// Activate this step (triggers spinner + AutoActivate if set).
				prog.Send(stepActivateMsg{id: def.Step.ID()})
			}

			// Start the step.
			if err := def.Step.Start(ctx, name); err != nil {
				if !def.Hidden {
					prog.Send(stepDoneMsg{
						id:    def.Step.ID(),
						ok:    false,
						label: def.effectiveLabel() + " failed: " + err.Error(),
					})
				}
				return
			}

			// Signal ready to unblock any dependents.
			close(ready[def.Step.ID()])

			// Invoke the OnReady callback.
			if def.OnReady != nil {
				go def.OnReady()
			}

			if !def.Hidden {
				prog.Send(stepDoneMsg{
					id:    def.Step.ID(),
					ok:    true,
					label: def.effectiveLabel() + " running",
				})
			}
		}()
	}
}
