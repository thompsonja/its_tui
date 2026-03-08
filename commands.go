package tui

import (
	"context"
	"fmt"
	"strings"
	"tui/step"
)

// switchToInstance cancels the current instance context, clears panel content,
// and sets the new instance name. Callers are responsible for calling
// registerPipeline and starting watchers / executeStart as needed.
func (m *model) switchToInstance(name string) {
	cancelInstance()
	instanceCtx, cancelInstance = context.WithCancel(context.Background())
	m.instance.Name = name
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
	sp := m.statePath
	cpu := inst.CPU
	if cpu == "" {
		cpu = "4"
	}
	ram := inst.RAM
	if ram == "" {
		ram = "4g"
	}
	mode := inst.Mode
	if mode == "" {
		mode = "dev"
	}

	defs := []StepDef{
		{
			Step:  &MinikubeStep{CPU: cpu, RAM: ram},
			Panel: PanelTopLeft,
			Label: "Minikube",
		},
		{
			Step:         &KubectlStep{},
			Panel:        PanelTopLeft,
			Label:        "kubectl",
			WaitFor:      "minikube",
			AutoActivate: true,
			Hidden:       true,
			OnReady:      func() { _ = MarkActive(sp) },
		},
	}

	if m.cfg.GenerateSkaffold != nil {
		if path, err := m.cfg.GenerateSkaffold(inst.Components); err == nil && path != "" {
			defs = append(defs, StepDef{
				Step:    &SkaffoldStep{Path: path, Mode: mode},
				Panel:   PanelTopRight,
				Label:   "Skaffold",
				WaitFor: "minikube",
			})
		}
	}

	if mfeCmd := m.cfg.mfeCommand(inst.MFE); mfeCmd.Cmd != "" {
		defs = append(defs, StepDef{
			Step:  &MFEStep{Cmd: mfeCmd},
			Panel: PanelBottomRight,
			Label: "MFE",
		})
	}

	return defs
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
		m.overlay = overlayWizard
		m.wizard = newStartWizard(m)
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
		m.startStep("mfe-stop", "stopping MFE")
		m.startStep("cluster-delete", "deleting cluster")
		go func() {
			prog.Send(cmdActiveMsg(+1))
			if state, err := LoadState(sp); err == nil && state.Instance != nil {
				step.KillProcessGroup(state.Instance.MFEPGID)
			}
			prog.Send(stepDoneMsg{id: "mfe-stop", ok: true, label: "MFE stopped"})
			(&MinikubeStep{}).Stop(context.Background(), name)
			prog.Send(stepDoneMsg{id: "cluster-delete", ok: true, label: "cluster deleted"})
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
	name := m.instanceName()
	cpu := cpuOptions[wiz.cpuIdx]
	ram := ramOptions[wiz.ramIdx]
	mode := skaffoldModes[wiz.modeIdx]
	sp := m.statePath

	defs := []StepDef{
		{
			Step:  &MinikubeStep{CPU: cpu, RAM: ram},
			Panel: PanelTopLeft,
			Label: "Minikube",
		},
		{
			Step:         &KubectlStep{},
			Panel:        PanelTopLeft,
			Label:        "kubectl",
			WaitFor:      "minikube",
			AutoActivate: true,
			Hidden:       true,
			OnReady:      func() { _ = MarkActive(sp) },
		},
	}

	if m.cfg.GenerateSkaffold != nil {
		path, err := m.cfg.GenerateSkaffold(wiz.selectedComps)
		if err != nil {
			prog.Send(commandLineMsg("skaffold generate error: " + err.Error()))
			return
		}
		if path != "" {
			defs = append(defs, StepDef{
				Step:    &SkaffoldStep{Path: path, Mode: mode},
				Panel:   PanelTopRight,
				Label:   "Skaffold",
				WaitFor: "minikube",
			})
		}
	}

	if mfeCmd := m.cfg.mfeCommand(wiz.selectedMFE); mfeCmd.Cmd != "" {
		defs = append(defs, StepDef{
			Step:  &MFEStep{Cmd: mfeCmd},
			Panel: PanelBottomRight,
			Label: "MFE",
		})
	}

	// Persist wizard selections.
	go func() {
		_ = SaveInstanceState(sp, InstanceState{
			CPU:        cpu,
			RAM:        ram,
			Components: wiz.selectedComps,
			MFE:        wiz.selectedMFE,
			Mode:       mode,
		})
	}()

	m.switchToInstance(name)
	m.registerPipeline(defs)
	m.fullscreenTarget = 0
	ctx := instanceCtx
	for _, def := range defs {
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
