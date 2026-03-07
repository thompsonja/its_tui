package tui

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// startParams carries the configured steps needed to boot an instance.
// skaffold and mfe are nil when not configured for this instance.
type startParams struct {
	minikube *MinikubeStep
	skaffold *SkaffoldStep // nil if no skaffold configured
	mfe      *MFEStep      // nil if no MFE configured
}

// switchToInstance cancels the current instance context, sets the new instance
// name, clears stale panel content, and starts fresh watchers.
func (m *model) switchToInstance(name string) {
	cancelInstance()
	instanceCtx, cancelInstance = context.WithCancel(context.Background())
	m.instance.Name = name
	m.minikubeBuf = nil
	m.minikubeLogBuf = nil
	m.skaffoldBuf = nil
	m.minikubeVP.SetContent("")
	m.skaffoldVP.SetContent("")
	ctx := instanceCtx
	go watchKubectl(ctx, name)
	go watchStep(ctx, &MinikubeStep{}, name)
	go watchStep(ctx, &SkaffoldStep{}, name)
	go watchStep(ctx, &MFEStep{}, name)
	go SetCurrentInstance(m.statePath, name) //nolint:errcheck
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

	case "list":
		m.printLine("$ list")
		state, _ := LoadState(m.statePath)
		nameSet := map[string]bool{}
		for n := range m.configs {
			nameSet[n] = true
		}
		for n := range state.Instances {
			nameSet[n] = true
		}
		if len(nameSet) == 0 {
			m.printLine("  no instances configured")
			m.printLine("  create " + DefaultConfigPath() + " to get started")
		} else {
			names := make([]string, 0, len(nameSet))
			for n := range nameSet {
				names = append(names, n)
			}
			sort.Strings(names)
			for _, n := range names {
				marker := "○"
				if _, active := state.Instances[n]; active {
					marker = "●"
				}
				suffix := ""
				if n == m.instance.Name {
					suffix = "  ← current"
				}
				m.printLine(fmt.Sprintf("  %s  %s%s", marker, n, suffix))
			}
		}

	case "use":
		m.printLine("$ " + line)
		if len(parts) < 2 {
			m.printLine("  usage: use <instance-name>")
		} else {
			m.switchToInstance(parts[1])
			m.printLine("  using: " + parts[1])
			m.fullscreenTarget = 0
		}

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
		delete(m.configs, name)
		m.minikubeBuf = nil
		m.minikubeLogBuf = nil
		m.skaffoldBuf = nil
		m.minikubeShowLog = true
		m.minikubeVP.SetContent("")
		m.skaffoldVP.SetContent("")
		m.startStep("mfe-stop", "stopping MFE")
		m.startStep("cluster-delete", "deleting cluster")
		sp := m.statePath
		go func() {
			prog.Send(cmdActiveMsg(+1))
			if state, err := LoadState(sp); err == nil {
				if inst, ok := state.Instances[name]; ok {
					killProcessGroup(inst.MFEPGID)
				}
			}
			prog.Send(stepDoneMsg{id: "mfe-stop", ok: true, label: "MFE stopped"})
			(&MinikubeStep{}).Stop(context.Background(), name)
			prog.Send(stepDoneMsg{id: "cluster-delete", ok: true, label: "cluster deleted"})
			prog.Send(cmdActiveMsg(-1))
			_ = MarkInactive(sp, name)
			_ = SetCurrentInstance(sp, "")
			prog.Send(instanceStoppedMsg{})
		}()

	case "logs":
		m.printLine("$ logs")
		if m.instance.Name == "" {
			m.printLine("  no active instance")
		} else {
			name := m.instance.Name
			for _, s := range []Step{&MinikubeStep{}, &SkaffoldStep{}, &MFEStep{}} {
				m.printLine(fmt.Sprintf("  %-10s %s", s.ID()+":", s.LogPath(name)))
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
					sp := m.statePath
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
	if m.wizard != nil && m.wizard.screen == wizScreenCustom {
		m.executeStartFromCustomWizard()
		return
	}
	m.executeStartFromFileWizard()
}

func (m *model) executeStartFromCustomWizard() {
	wiz := m.wizard
	name := strings.TrimSpace(wiz.custName.Value())
	if name == "" {
		name = wiz.custName.Placeholder
	}
	if name == "" {
		name = "instance"
	}

	cpu := cpuOptions[wiz.cpuIdx]
	ram := ramOptions[wiz.ramIdx]
	mode := skaffoldModes[wiz.custModeIdx]

	minikube := &MinikubeStep{CPU: cpu, RAM: ram}

	// Resolve the skaffold path via the GenerateSkaffold callback.
	var skaffold *SkaffoldStep
	if m.cfg.GenerateSkaffold != nil {
		path, err := m.cfg.GenerateSkaffold(wiz.selectedComps)
		if err != nil {
			prog.Send(commandLineMsg("skaffold generate error: " + err.Error()))
			return
		}
		if path != "" {
			skaffold = &SkaffoldStep{Path: path, Mode: mode}
		}
	}

	var mfe *MFEStep
	if mfeCmd := m.cfg.mfeCommand(wiz.selectedMFE); mfeCmd.Cmd != "" {
		mfe = &MFEStep{Cmd: mfeCmd}
	}

	// Persist the wizard selections.
	sel := CustomInstanceConfig{
		Instance:   name,
		CPU:        cpu,
		RAM:        ram,
		Components: wiz.selectedComps,
		MFE:        wiz.selectedMFE,
		Mode:       mode,
	}
	sp := m.statePath
	go func() {
		if err := WriteCustomConfig(sp, name, sel); err != nil {
			prog.Send(commandLineMsg("warning: could not save selections: " + err.Error()))
		} else {
			prog.Send(commandLineMsg("selections saved"))
		}
	}()

	m.switchToInstance(name)
	m.fullscreenTarget = 0
	m.executeStart(startParams{minikube: minikube, skaffold: skaffold, mfe: mfe})
}

// executeStartFromFileWizard reads wizard fields, switches the active instance,
// restarts watchers, and calls executeStart.
func (m *model) executeStartFromFileWizard() {
	wiz := m.wizard
	name := strings.TrimSpace(wiz.nameInput.Value())
	if name == "" {
		name = wiz.nameInput.Placeholder
	}
	if name == "" {
		name = "instance"
	}

	configPath := strings.TrimSpace(wiz.configInput.Value())
	mode := skaffoldModes[wiz.modeIdx]

	// Load the chosen config file.
	var instanceCfg InstanceConfig
	if configPath != "" {
		if configs, err := LoadConfigs(configPath); err != nil {
			m.commandsBuf = appendLine(m.commandsBuf, "config error: "+err.Error())
			m.commandsVP.SetContent(wrapContent(m.commandsBuf, m.commandsVP.Width))
			m.commandsVP.GotoBottom()
		} else {
			m.configs = configs
			var ok bool
			instanceCfg, ok = configs[name]
			if !ok && len(configs) > 0 {
				cfgNames := make([]string, 0, len(configs))
				for n := range configs {
					cfgNames = append(cfgNames, n)
				}
				sort.Strings(cfgNames)
				instanceCfg = configs[cfgNames[0]]
			}
		}
	}

	// Populate each step from the loaded config (defaults applied first).
	minikube := &MinikubeStep{CPU: "4", RAM: "4g"}
	minikube.ReadConfig(instanceCfg)

	var skaffold *SkaffoldStep
	if instanceCfg.Skaffold.Path != "" {
		skaffold = &SkaffoldStep{Mode: mode}
		skaffold.ReadConfig(instanceCfg)
	}

	var mfe *MFEStep
	if instanceCfg.MFE.Path != "" {
		mfe = &MFEStep{}
		mfe.ReadConfig(instanceCfg)
	}

	m.switchToInstance(name)
	m.fullscreenTarget = 0
	m.executeStart(startParams{minikube: minikube, skaffold: skaffold, mfe: mfe})
}

// executeStart launches all instance processes:
//   - MFE starts immediately (no dependency on minikube).
//   - minikube start runs (output → /tmp log, visible in the Minikube panel).
//   - skaffold starts once minikube completes.
func (m *model) executeStart(p startParams) {
	m.steps = map[string]*commandStep{}
	m.minikubeShowLog = true
	m.minikubeAutoSwitched = false
	m.minikubeVP.SetContent(wrapContent(m.minikubeLogBuf, m.minikubeVP.Width))

	name := m.instance.Name
	sp := m.statePath

	m.startStep(p.minikube.ID(), "starting minikube")
	if p.skaffold != nil {
		m.startPendingStep(p.skaffold.ID(), "skaffold (waiting for minikube)")
	}
	if p.mfe != nil {
		mfe := p.mfe
		m.startStep(mfe.ID(), "starting MFE")
		go func() {
			if err := mfe.Start(instanceCtx, name); err != nil {
				prog.Send(stepDoneMsg{id: mfe.ID(), ok: false, label: "MFE failed to start"})
				return
			}
			prog.Send(stepDoneMsg{id: mfe.ID(), ok: true, label: "MFE running"})
		}()
	}

	minikube := p.minikube
	skaffold := p.skaffold
	go func() {
		prog.Send(cmdActiveMsg(+1))
		if err := minikube.Start(instanceCtx, name); err != nil {
			prog.Send(stepDoneMsg{id: minikube.ID(), ok: false, label: "minikube failed: " + err.Error()})
			prog.Send(cmdActiveMsg(-1))
			return
		}
		_ = MarkActive(sp, name)
		prog.Send(stepDoneMsg{id: minikube.ID(), ok: true, label: "minikube ready"})
		if minikube.IsReady(instanceCtx, name) {
			prog.Send(minikubeReadyMsg{})
		}
		if skaffold != nil {
			prog.Send(stepActivateMsg{id: skaffold.ID()})
			go func() {
				if err := skaffold.Start(instanceCtx, name); err != nil {
					prog.Send(stepDoneMsg{id: skaffold.ID(), ok: false, label: "skaffold failed to start"})
					return
				}
				prog.Send(stepDoneMsg{id: skaffold.ID(), ok: true, label: "skaffold running"})
			}()
		}
		prog.Send(cmdActiveMsg(-1))
	}()
}

