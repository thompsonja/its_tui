package main

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
)

// flipStep controls animation speed: how much flipProgress advances per 60fps tick.
// 0.10 ≈ 10 frames (~166ms) for a full transition.
const flipStep = 0.10

// fullscreenStep: 0.12 ≈ 8 frames (~133ms) for a full fullscreen transition.
const fullscreenStep = 0.12

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.ready = true
		m.resizePanels()
		return m, tea.Batch(cmds...)

	case cmdActiveMsg:
		m.runningCmds += int(msg)
		if m.runningCmds < 0 {
			m.runningCmds = 0
		}

	case tickMsg:
		cmds = append(cmds, tickCmd())
		if m.runningCmds > 0 {
			m.spinnerTick++
		}
		// Advance card-flip animation toward target.
		if m.flipProgress != m.flipTarget {
			if m.flipTarget > m.flipProgress {
				m.flipProgress += flipStep
				if m.flipProgress >= m.flipTarget {
					m.flipProgress = m.flipTarget
				}
			} else {
				m.flipProgress -= flipStep
				if m.flipProgress <= m.flipTarget {
					m.flipProgress = m.flipTarget
					if m.flipTarget == 0 {
						// Animation completed returning to commands — clean up overlay.
						m.overlay = overlayNone
						m.wizard = nil
					}
				}
			}
		}
		// Advance fullscreen animation toward target.
		if m.fullscreenProgress != m.fullscreenTarget {
			if m.fullscreenTarget > m.fullscreenProgress {
				m.fullscreenProgress += fullscreenStep
				if m.fullscreenProgress >= m.fullscreenTarget {
					m.fullscreenProgress = m.fullscreenTarget
					m.resizePanels() // settle viewports at fullscreen size
				}
			} else {
				m.fullscreenProgress -= fullscreenStep
				if m.fullscreenProgress <= m.fullscreenTarget {
					m.fullscreenProgress = m.fullscreenTarget
					m.resizePanels() // settle viewports at normal size
				}
			}
		}

	// ── Streaming log ingestion ──────────────────────────────────────────────

	case minikubeLineMsg:
		m.minikubeBuf = appendLine(m.minikubeBuf, string(msg))
		m.minikubeVP.SetContent(wrapContent(m.minikubeBuf, m.minikubeVP.Width))
		m.minikubeVP.GotoBottom()

	case minikubeSetMsg:
		m.minikubeBuf = []string(msg)
		m.minikubeVP.SetContent(wrapContent(m.minikubeBuf, m.minikubeVP.Width))
		// No GotoBottom — preserve scroll position while the user reads.

	case skaffoldLineMsg:
		m.skaffoldBuf = appendLine(m.skaffoldBuf, string(msg))
		m.skaffoldVP.SetContent(wrapContent(m.skaffoldBuf, m.skaffoldVP.Width))
		m.skaffoldVP.GotoBottom()

	case commandLineMsg:
		m.commandsBuf = appendLine(m.commandsBuf, string(msg))
		m.commandsVP.SetContent(wrapContent(m.commandsBuf, m.commandsVP.Width))
		m.commandsVP.GotoBottom()

	case mfeLineMsg:
		m.mfeBuf = appendLine(m.mfeBuf, string(msg))
		m.mfeVP.SetContent(wrapContent(m.mfeBuf, m.mfeVP.Width))
		m.mfeVP.GotoBottom()

	// ── Key handling ─────────────────────────────────────────────────────────

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit

		case "ctrl+f":
			if m.instance.Name != "" {
				if m.fullscreenTarget == 1 {
					m.fullscreenTarget = 0
				} else {
					m.fullscreenTarget = 1
				}
			}
			return m, tea.Batch(cmds...)

		case "esc":
			// Close component picker before anything else.
			if m.wizard != nil && m.wizard.compPickerOpen {
				m.wizard.compPickerOpen = false
				m.wizard.syncFocus()
				return m, tea.Batch(cmds...)
			}
			if m.flipTarget == 1.0 {
				m.flipTarget = 0.0
				return m, tea.Batch(cmds...)
			}
			if m.fullscreenTarget == 1 {
				m.fullscreenTarget = 0
			}
			return m, tea.Batch(cmds...)

		case "tab":
			if m.instance.Name != "" {
				// Tab exits the help overlay before cycling.
				if m.flipTarget == 1.0 {
					m.flipTarget = 0.0
				}
				m.cycleFocus(+1)
			}
			return m, tea.Batch(cmds...)

		case "shift+tab":
			if m.instance.Name != "" {
				if m.flipTarget == 1.0 {
					m.flipTarget = 0.0
				}
				m.cycleFocus(-1)
			}
			return m, tea.Batch(cmds...)

		default:
			var cmd tea.Cmd
			switch m.focused {
			case panelMinikube:
				m.minikubeVP, cmd = m.minikubeVP.Update(msg)
			case panelSkaffold:
				m.skaffoldVP, cmd = m.skaffoldVP.Update(msg)
			case panelCommands:
				if m.flipTarget == 1.0 {
					switch m.overlay {
					case overlayHelp:
						m.helpOverlayVP, cmd = m.helpOverlayVP.Update(msg)
					case overlayWizard:
						m.handleWizardKey(msg)
					}
				} else if msg.String() == "enter" {
					if line := m.input.Value(); line != "" {
						m.input.Reset()
						m.addToHistory(line)
						m.dispatchCommand(line)
					}
				} else if msg.String() == "up" {
					m.historyUp()
				} else if msg.String() == "down" {
					m.historyDown()
				} else {
					m.input, cmd = m.input.Update(msg)
				}
			case panelMFE:
				m.mfeVP, cmd = m.mfeVP.Update(msg)
			}
			cmds = append(cmds, cmd)
		}

	}

	return m, tea.Batch(cmds...)
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
		m.commandsBuf = appendLine(m.commandsBuf, "$ list")
		state, _ := LoadState(m.statePath)
		// Collect all known names: union of config and state.
		nameSet := map[string]bool{}
		for n := range m.configs {
			nameSet[n] = true
		}
		for n := range state.Instances {
			nameSet[n] = true
		}
		if len(nameSet) == 0 {
			m.commandsBuf = appendLine(m.commandsBuf, "  no instances configured")
			m.commandsBuf = appendLine(m.commandsBuf, "  create "+DefaultConfigPath()+" to get started")
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
				m.commandsBuf = appendLine(m.commandsBuf, fmt.Sprintf("  %s  %s%s", marker, n, suffix))
			}
		}
		m.commandsVP.SetContent(wrapContent(m.commandsBuf, m.commandsVP.Width))
		m.commandsVP.GotoBottom()

	case "use":
		m.commandsBuf = appendLine(m.commandsBuf, "$ "+line)
		if len(parts) < 2 {
			m.commandsBuf = appendLine(m.commandsBuf, "  usage: use <instance-name>")
		} else {
			name := parts[1]
			// Cancel existing watchers and start fresh ones for the new instance.
			cancelInstance()
			instanceCtx, cancelInstance = context.WithCancel(context.Background())
			m.instance.Name = name
			// Clear stale panel content.
			m.minikubeBuf = nil
			m.skaffoldBuf = nil
			m.minikubeVP.SetContent("")
			m.skaffoldVP.SetContent("")
			// Start passive watchers (kubectl + skaffold log tail).
			ctx := instanceCtx
			go watchKubectl(ctx, name)
			go watchSkaffoldLog(ctx, skaffoldLogPath(name), name)
			// Persist the selected instance.
			go SetCurrentInstance(m.statePath, name) //nolint:errcheck
			m.commandsBuf = appendLine(m.commandsBuf, "  using: "+name)
			m.fullscreenTarget = 0
		}
		m.commandsVP.SetContent(wrapContent(m.commandsBuf, m.commandsVP.Width))
		m.commandsVP.GotoBottom()

	case "start":
		// Always open the wizard — it pre-populates from the current instance.
		m.overlay = overlayWizard
		m.wizard = newStartWizard(m)
		m.flipTarget = 1.0

	case "stop":
		name := m.instance.Name
		m.commandsBuf = appendLine(m.commandsBuf, "$ stop")
		if name == "" {
			m.commandsBuf = appendLine(m.commandsBuf, "  no active instance")
			m.commandsVP.SetContent(wrapContent(m.commandsBuf, m.commandsVP.Width))
			m.commandsVP.GotoBottom()
			break
		}
		m.commandsBuf = appendLine(m.commandsBuf, fmt.Sprintf("  stopping %s …", name))
		m.commandsVP.SetContent(wrapContent(m.commandsBuf, m.commandsVP.Width))
		m.commandsVP.GotoBottom()

		// Cancel instanceCtx — kills skaffold, MFE, and log watchers.
		cancelInstance()
		instanceCtx, cancelInstance = context.WithCancel(context.Background())
		// No watchers restarted — there is no active instance after stop.

		// Clear instance state immediately.
		m.instance.Name = ""
		delete(m.configs, name)
		m.minikubeBuf = nil
		m.skaffoldBuf = nil
		m.minikubeVP.SetContent("")
		m.skaffoldVP.SetContent("")
		m.fullscreenTarget = 1 // back to fullscreen with no instance

		// Delete the minikube cluster in the background.
		sp := m.statePath
		go func() {
			prog.Send(cmdActiveMsg(+1))
			streamToPanel(context.Background(),
				func(s string) tea.Msg { return commandLineMsg(s) },
				"minikube", "delete",
			)
			prog.Send(cmdActiveMsg(-1))
			_ = MarkInactive(sp, name)
			_ = SetCurrentInstance(sp, "")
			prog.Send(commandLineMsg(fmt.Sprintf("instance '%s' stopped", name)))
		}()

	case "theme":
		m.commandsBuf = appendLine(m.commandsBuf, "$ "+line)
		if len(parts) < 2 {
			m.commandsBuf = appendLine(m.commandsBuf, "themes: "+themeNames())
		} else {
			name := parts[1]
			found := false
			for _, t := range presets {
				if t.Name == name {
					currentTheme = t
					// Refresh the help overlay with the new palette.
					m.helpOverlayVP.SetContent(helpContent(m.helpOverlayVP.Width))
					m.commandsBuf = appendLine(m.commandsBuf, "theme set to: "+name)
					found = true
					sp := m.statePath
					go func() { _ = SaveTheme(sp, name) }()
					break
				}
			}
			if !found {
				m.commandsBuf = appendLine(m.commandsBuf, "unknown theme: "+name+" (try: "+themeNames()+")")
			}
		}
		m.commandsVP.SetContent(wrapContent(m.commandsBuf, m.commandsVP.Width))
		m.commandsVP.GotoBottom()

	default:
		m.commandsBuf = appendLine(m.commandsBuf, "$ "+line)
		m.commandsBuf = appendLine(m.commandsBuf, "unknown command: "+parts[0]+" (try: help)")
		m.commandsVP.SetContent(wrapContent(m.commandsBuf, m.commandsVP.Width))
		m.commandsVP.GotoBottom()
	}
}

// ── Wizard ───────────────────────────────────────────────────────────────────

func (m *model) handleWizardKey(msg tea.KeyMsg) {
	wiz := m.wizard
	if wiz == nil {
		return
	}
	switch wiz.screen {
	case wizScreenSelect:
		m.handleWizardKeySelect(msg)
	case wizScreenFile:
		m.handleWizardKeyFile(msg)
	case wizScreenCustom:
		m.handleWizardKeyCustom(msg)
	}
}

func (m *model) handleWizardKeySelect(msg tea.KeyMsg) {
	wiz := m.wizard
	switch msg.String() {
	case "up":
		if wiz.screenIdx > 0 {
			wiz.screenIdx--
		}
	case "down":
		if wiz.screenIdx < 1 {
			wiz.screenIdx++
		}
	case "enter":
		if wiz.screenIdx == 0 {
			wiz.screen = wizScreenFile
		} else {
			wiz.screen = wizScreenCustom
		}
		wiz.syncFocus()
	}
}

func (m *model) handleWizardKeyFile(msg tea.KeyMsg) {
	wiz := m.wizard
	key := msg.String()

	switch key {
	case "tab":
		wiz.field = (wiz.field + 1) % wizNumFields
		wiz.syncFocus()
		return
	case "shift+tab":
		wiz.field = (wiz.field - 1 + wizNumFields) % wizNumFields
		wiz.syncFocus()
		return
	}

	switch wiz.field {
	case wizFieldName:
		switch key {
		case "down", "enter":
			wiz.field = wizFieldConfig
			wiz.syncFocus()
		case "up":
			wiz.field = wizFieldButtons
			wiz.syncFocus()
		default:
			wiz.nameInput, _ = wiz.nameInput.Update(msg)
		}

	case wizFieldConfig:
		switch key {
		case "up":
			if len(wiz.browseFiles) > 0 && wiz.browseIdx > 0 {
				wiz.browseIdx--
				wiz.configInput.SetValue(wiz.browseFiles[wiz.browseIdx])
			} else {
				wiz.field = wizFieldName
				wiz.syncFocus()
			}
		case "down":
			if len(wiz.browseFiles) > 0 && wiz.browseIdx < len(wiz.browseFiles)-1 {
				wiz.browseIdx++
				wiz.configInput.SetValue(wiz.browseFiles[wiz.browseIdx])
			} else {
				wiz.field = wizFieldMode
				wiz.syncFocus()
			}
		case "enter":
			if len(wiz.browseFiles) > 0 {
				wiz.configInput.SetValue(wiz.browseFiles[wiz.browseIdx])
			}
			wiz.field = wizFieldMode
			wiz.syncFocus()
		default:
			wiz.configInput, _ = wiz.configInput.Update(msg)
			v := wiz.configInput.Value()
			for i, f := range wiz.browseFiles {
				if f == v {
					wiz.browseIdx = i
					break
				}
			}
		}

	case wizFieldMode:
		switch key {
		case "left":
			if wiz.modeIdx > 0 {
				wiz.modeIdx--
			}
		case "right":
			if wiz.modeIdx < len(skaffoldModes)-1 {
				wiz.modeIdx++
			}
		case "up":
			wiz.field = wizFieldConfig
			wiz.syncFocus()
		case "down", "enter":
			wiz.field = wizFieldButtons
			wiz.syncFocus()
		}

	case wizFieldButtons:
		switch key {
		case "left":
			if wiz.confirmIdx > 0 {
				wiz.confirmIdx--
			}
		case "right":
			if wiz.confirmIdx < 1 {
				wiz.confirmIdx++
			}
		case "up":
			wiz.field = wizFieldMode
			wiz.syncFocus()
		case "down":
			wiz.field = wizFieldName
			wiz.syncFocus()
		case "enter":
			if wiz.confirmIdx == 0 {
				m.executeStartFromWizard()
			}
			m.flipTarget = 0.0
		}
	}
}

func (m *model) handleWizardKeyCustom(msg tea.KeyMsg) {
	wiz := m.wizard
	key := msg.String()

	// Tab/Shift+Tab cycle fields only when the picker is closed.
	switch key {
	case "tab":
		if !wiz.compPickerOpen {
			wiz.custField = (wiz.custField + 1) % custNumFields
			wiz.syncFocus()
		}
		return
	case "shift+tab":
		if !wiz.compPickerOpen {
			wiz.custField = (wiz.custField - 1 + custNumFields) % custNumFields
			wiz.syncFocus()
		}
		return
	}

	switch wiz.custField {
	case custFieldName:
		switch key {
		case "down", "enter":
			wiz.custField = custFieldCPU
			wiz.syncFocus()
		case "up":
			wiz.custField = custFieldButtons
			wiz.syncFocus()
		default:
			wiz.custName, _ = wiz.custName.Update(msg)
		}

	case custFieldCPU:
		switch key {
		case "left":
			if wiz.cpuIdx > 0 {
				wiz.cpuIdx--
			}
		case "right":
			if wiz.cpuIdx < len(cpuOptions)-1 {
				wiz.cpuIdx++
			}
		case "up":
			wiz.custField = custFieldName
			wiz.syncFocus()
		case "down", "enter":
			wiz.custField = custFieldRAM
			wiz.syncFocus()
		}

	case custFieldRAM:
		switch key {
		case "left":
			if wiz.ramIdx > 0 {
				wiz.ramIdx--
			}
		case "right":
			if wiz.ramIdx < len(ramOptions)-1 {
				wiz.ramIdx++
			}
		case "up":
			wiz.custField = custFieldCPU
			wiz.syncFocus()
		case "down", "enter":
			wiz.custField = custFieldComponents
			wiz.syncFocus()
		}

	case custFieldComponents:
		if wiz.compPickerOpen {
			switch key {
			case "up":
				if wiz.compPickerIdx > 0 {
					wiz.compPickerIdx--
				}
			case "down":
				if wiz.compPickerIdx < len(wiz.compPickerItems)-1 {
					wiz.compPickerIdx++
				}
			case "enter":
				wiz.togglePickerItem(wiz.compPickerIdx)
			default:
				wiz.compPickerSearch, _ = wiz.compPickerSearch.Update(msg)
				wiz.updateCompFilter()
			}
		} else {
			switch key {
			case "up":
				wiz.custField = custFieldRAM
				wiz.syncFocus()
			case "down":
				wiz.custField = custFieldMFE
				wiz.syncFocus()
			case "enter":
				wiz.compPickerOpen = true
				wiz.compPickerSearch.SetValue("")
				wiz.updateCompFilter()
				wiz.syncFocus()
			}
		}

	case custFieldMFE:
		switch key {
		case "up":
			wiz.custField = custFieldComponents
			wiz.syncFocus()
		case "down", "enter":
			wiz.custField = custFieldMode
			wiz.syncFocus()
		default:
			wiz.custMFEInput, _ = wiz.custMFEInput.Update(msg)
		}

	case custFieldMode:
		switch key {
		case "left":
			if wiz.custModeIdx > 0 {
				wiz.custModeIdx--
			}
		case "right":
			if wiz.custModeIdx < len(skaffoldModes)-1 {
				wiz.custModeIdx++
			}
		case "up":
			wiz.custField = custFieldMFE
			wiz.syncFocus()
		case "down", "enter":
			wiz.custField = custFieldButtons
			wiz.syncFocus()
		}

	case custFieldButtons:
		switch key {
		case "left":
			if wiz.custConfirmIdx > 0 {
				wiz.custConfirmIdx--
			}
		case "right":
			if wiz.custConfirmIdx < 1 {
				wiz.custConfirmIdx++
			}
		case "up":
			wiz.custField = custFieldMode
			wiz.syncFocus()
		case "down":
			wiz.custField = custFieldName
			wiz.syncFocus()
		case "enter":
			if wiz.custConfirmIdx == 0 {
				m.executeStartFromWizard()
			}
			m.flipTarget = 0.0
		}
	}
}

// startParams carries everything needed to boot an instance.
type startParams struct {
	cpu          string
	ram          string
	skaffoldPath string
	skaffoldMode string // "dev", "run", or "debug"
	mfePath      string
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
	mfePath := strings.TrimSpace(wiz.custMFEInput.Value())

	// Write selections to disk.
	cfg := CustomInstanceConfig{
		Instance:   name,
		CPU:        cpu,
		RAM:        ram,
		Components: wiz.selectedComps,
		MFE:        mfePath,
		Mode:       mode,
	}
	sp := m.statePath
	go func() {
		if err := WriteCustomConfig(sp, name, cfg); err != nil {
			prog.Send(commandLineMsg("warning: could not save selections: " + err.Error()))
		} else {
			prog.Send(commandLineMsg("selections saved"))
		}
	}()

	// Switch to the new instance.
	cancelInstance()
	instanceCtx, cancelInstance = context.WithCancel(context.Background())
	m.instance.Name = name
	m.minikubeBuf = nil
	m.skaffoldBuf = nil
	m.minikubeVP.SetContent("")
	m.skaffoldVP.SetContent("")
	ctx := instanceCtx
	go watchKubectl(ctx, name)
	go watchSkaffoldLog(ctx, skaffoldLogPath(name), name)
	go SetCurrentInstance(sp, name) //nolint:errcheck

	m.fullscreenTarget = 0
	m.executeStart(startParams{
		cpu:          cpu,
		ram:          ram,
		skaffoldPath: "sample/skaffold.yaml",
		skaffoldMode: mode,
		mfePath:      mfePath,
	})
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
				// Name not found — fall back to the first entry alphabetically.
				cfgNames := make([]string, 0, len(configs))
				for n := range configs {
					cfgNames = append(cfgNames, n)
				}
				sort.Strings(cfgNames)
				instanceCfg = configs[cfgNames[0]]
			}
		}
	}

	// Use config values if present; fall back to sensible defaults.
	cpu := "4"
	ram := "4g"
	if instanceCfg.Minikube.CPU > 0 {
		cpu = fmt.Sprintf("%d", instanceCfg.Minikube.CPU)
	}
	if instanceCfg.Minikube.RAM != "" {
		ram = instanceCfg.Minikube.RAM
	}

	// Switch to the new instance — cancel old watchers, start fresh ones.
	cancelInstance()
	instanceCtx, cancelInstance = context.WithCancel(context.Background())
	m.instance.Name = name
	m.minikubeBuf = nil
	m.skaffoldBuf = nil
	m.minikubeVP.SetContent("")
	m.skaffoldVP.SetContent("")
	ctx := instanceCtx
	go watchKubectl(ctx, name)
	go watchSkaffoldLog(ctx, skaffoldLogPath(name), name)
	go SetCurrentInstance(m.statePath, name) //nolint:errcheck

	m.fullscreenTarget = 0
	m.executeStart(startParams{
		cpu:          cpu,
		ram:          ram,
		skaffoldPath: instanceCfg.Skaffold.Path,
		skaffoldMode: mode,
		mfePath:      instanceCfg.MFE.Path,
	})
}

// executeStart streams minikube start, then chains skaffold and MFE.
func (m *model) executeStart(p startParams) {
	m.commandsBuf = appendLine(m.commandsBuf, fmt.Sprintf("$ minikube start --cpus %s --memory %s", p.cpu, p.ram))
	if p.skaffoldPath != "" {
		m.commandsBuf = appendLine(m.commandsBuf, fmt.Sprintf("  + skaffold %s --filename %s", p.skaffoldMode, p.skaffoldPath))
	}
	m.commandsVP.SetContent(wrapContent(m.commandsBuf, m.commandsVP.Width))
	m.commandsVP.GotoBottom()

	name := m.instance.Name
	sp := m.statePath

	go func() {
		prog.Send(cmdActiveMsg(+1))
		streamToPanel(context.Background(),
			func(s string) tea.Msg { return commandLineMsg(s) },
			"minikube", "start", "--cpus", p.cpu, "--memory", p.ram,
		)
		prog.Send(cmdActiveMsg(-1))
		if name != "" {
			if err := MarkActive(sp, name); err != nil {
				prog.Send(commandLineMsg("warning: " + err.Error()))
			} else {
				prog.Send(commandLineMsg("instance '" + name + "' marked active"))
			}
		}
		if p.skaffoldPath != "" {
			go startSkaffoldToLog(name, p.skaffoldPath, p.skaffoldMode)
		}
		if p.mfePath != "" {
			go startMFE(p.mfePath)
		}
	}()
}

// addToHistory appends line to cmdHistory (skipping consecutive duplicates)
// and resets the navigation state.
func (m *model) addToHistory(line string) {
	if len(m.cmdHistory) == 0 || m.cmdHistory[len(m.cmdHistory)-1] != line {
		m.cmdHistory = append(m.cmdHistory, line)
	}
	m.historyIdx = -1
	m.historyDraft = ""
}

// historyUp moves one step back through command history.
func (m *model) historyUp() {
	if len(m.cmdHistory) == 0 {
		return
	}
	if m.historyIdx == -1 {
		m.historyDraft = m.input.Value()
		m.historyIdx = len(m.cmdHistory) - 1
	} else if m.historyIdx > 0 {
		m.historyIdx--
	}
	m.input.SetValue(m.cmdHistory[m.historyIdx])
}

// historyDown moves one step forward through command history, restoring the
// draft when the user navigates past the most recent entry.
func (m *model) historyDown() {
	if m.historyIdx == -1 {
		return
	}
	if m.historyIdx == len(m.cmdHistory)-1 {
		m.historyIdx = -1
		m.input.SetValue(m.historyDraft)
	} else {
		m.historyIdx++
		m.input.SetValue(m.cmdHistory[m.historyIdx])
	}
}

func (m *model) cycleFocus(d int) {
	m.focused = (m.focused + d + numPanels) % numPanels
	if m.focused == panelCommands {
		m.input.Focus()
	} else {
		m.input.Blur()
	}
}

func (m *model) resizePanels() {
	const border = 2
	const title  = 2 // title text (1) + MarginBottom(1)
	const input  = 2 // separator (1) + textinput (1)

	grid := m.height - 1 // 1 row reserved for the top bar

	var (
		vpW_L, vpW_R                             int
		vpH_top, vpH_commands, vpH_mfe, vpH_overlay int
	)

	if m.fullscreenProgress >= 1 {
		// Focused panel fills the entire grid (minus top bar).
		vpW_L        = max(1, m.width-border)
		vpW_R        = vpW_L
		vpH_top      = max(1, grid-border-title)
		vpH_commands = max(1, grid-border-title-input)
		vpH_mfe      = vpH_top
		vpH_overlay  = vpH_top
	} else {
		colL := m.width / 2
		colR := m.width - colL
		rowT := grid / 2
		rowB := grid - rowT

		vpW_L        = max(1, colL-border)
		vpW_R        = max(1, colR-border)
		vpH_top      = max(1, rowT-border-title)
		vpH_commands = max(1, rowB-border-title-input)
		vpH_mfe      = max(1, rowB-border-title)
		vpH_overlay  = max(1, rowB-border-title)
	}

	if m.minikubeVP.Width == 0 {
		// First resize: create viewports so keymaps are initialised.
		m.minikubeVP    = viewport.New(vpW_L, vpH_top)
		m.skaffoldVP    = viewport.New(vpW_R, vpH_top)
		m.commandsVP    = viewport.New(vpW_L, vpH_commands)
		m.mfeVP         = viewport.New(vpW_R, vpH_mfe)
		m.helpOverlayVP = viewport.New(vpW_L, vpH_overlay)
		// Restore any content that arrived before the first layout.
		m.minikubeVP.SetContent(wrapContent(m.minikubeBuf, m.minikubeVP.Width))
		m.skaffoldVP.SetContent(wrapContent(m.skaffoldBuf, m.skaffoldVP.Width))
		m.commandsVP.SetContent(wrapContent(m.commandsBuf, m.commandsVP.Width))
		m.mfeVP.SetContent(wrapContent(m.mfeBuf, m.mfeVP.Width))
		m.minikubeVP.GotoBottom()
		m.skaffoldVP.GotoBottom()
		m.commandsVP.GotoBottom()
	} else {
		// Subsequent resizes: update dimensions then re-wrap content at new width.
		m.minikubeVP.Width     = vpW_L
		m.minikubeVP.Height    = vpH_top
		m.skaffoldVP.Width     = vpW_R
		m.skaffoldVP.Height    = vpH_top
		m.commandsVP.Width     = vpW_L
		m.commandsVP.Height    = vpH_commands
		m.mfeVP.Width          = vpW_R
		m.mfeVP.Height         = vpH_mfe
		m.helpOverlayVP.Width  = vpW_L
		m.helpOverlayVP.Height = vpH_overlay

		m.minikubeVP.SetContent(wrapContent(m.minikubeBuf, vpW_L))
		m.skaffoldVP.SetContent(wrapContent(m.skaffoldBuf, vpW_R))
		m.commandsVP.SetContent(wrapContent(m.commandsBuf, vpW_L))
		m.mfeVP.SetContent(wrapContent(m.mfeBuf, vpW_R))
		m.minikubeVP.GotoBottom()
		m.skaffoldVP.GotoBottom()
		m.commandsVP.GotoBottom()
		m.mfeVP.GotoBottom()
	}
	// Help content uses width for column layout — refresh on every resize.
	m.helpOverlayVP.SetContent(helpContent(vpW_L))

	// Textinput width tracks the panel content area.
	m.input.Width = vpW_L

	// Keep wizard inputs sized to the current panel width.
	if m.wizard != nil {
		inputW := max(20, vpW_L-16)
		m.wizard.nameInput.Width = inputW
		m.wizard.configInput.Width = inputW
		m.wizard.custName.Width = inputW
		m.wizard.compPickerSearch.Width = inputW
		m.wizard.custMFEInput.Width = inputW
	}
}
