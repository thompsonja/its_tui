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

// advanceAnim advances progress toward target by step. Returns the new progress
// and whether it just settled on target this call.
func advanceAnim(progress, target, step float64) (float64, bool) {
	if progress == target {
		return progress, false
	}
	if target > progress {
		progress += step
		if progress >= target {
			return target, true
		}
	} else {
		progress -= step
		if progress <= target {
			return target, true
		}
	}
	return progress, false
}

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
		if newP, settled := advanceAnim(m.flipProgress, m.flipTarget, flipStep); newP != m.flipProgress {
			m.flipProgress = newP
			if settled && m.flipTarget == 0 {
				// Animation completed returning to commands — clean up overlay.
				m.overlay = overlayNone
				m.wizard = nil
			}
		}
		// Advance fullscreen animation toward target.
		if newP, settled := advanceAnim(m.fullscreenProgress, m.fullscreenTarget, fullscreenStep); newP != m.fullscreenProgress {
			m.fullscreenProgress = newP
			if settled {
				m.resizePanels() // settle viewports at final size
			}
		}

	// ── Streaming log ingestion ──────────────────────────────────────────────

	case minikubeLineMsg:
		m.minikubeLogBuf = appendLine(m.minikubeLogBuf, string(msg))
		if m.minikubeShowLog {
			m.minikubeVP.SetContent(wrapContent(m.minikubeLogBuf, m.minikubeVP.Width))
			m.minikubeVP.GotoBottom()
		}

	case minikubeSetMsg:
		m.minikubeBuf = []string(msg)
		if !m.minikubeShowLog {
			m.minikubeVP.SetContent(wrapContent(m.minikubeBuf, m.minikubeVP.Width))
			// No GotoBottom — preserve scroll position while the user reads.
		}

	case minikubeReadyMsg:
		// One-time auto-switch: flip to the kubectl tab now that the cluster is up.
		if !m.minikubeAutoSwitched {
			m.minikubeAutoSwitched = true
			m.minikubeShowLog = false
			m.minikubeVP.SetContent(wrapContent(m.minikubeBuf, m.minikubeVP.Width))
		}

	case skaffoldLineMsg:
		appendToVP(&m.skaffoldBuf, &m.skaffoldVP, string(msg))

	case commandLineMsg:
		appendToVP(&m.commandsBuf, &m.commandsVP, string(msg))

	case mfeLineMsg:
		appendToVP(&m.mfeBuf, &m.mfeVP, string(msg))

	case mfePIDMsg:
		sp, name := m.statePath, m.instance.Name
		pgid := int(msg)
		go func() { _ = SaveMFEPGID(sp, name, pgid) }()

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
			// Picker open: Tab closes only the picker, does not cycle panels.
			if m.wizard != nil && m.wizard.compPickerOpen {
				m.wizard.compPickerOpen = false
				m.wizard.syncFocus()
				return m, tea.Batch(cmds...)
			}
			if m.instance.Name != "" {
				// Tab exits the help overlay before cycling.
				if m.flipTarget == 1.0 {
					m.flipTarget = 0.0
				}
				m.cycleFocus(+1)
			}
			return m, tea.Batch(cmds...)

		case "shift+tab":
			// Picker open: Shift+Tab closes only the picker, does not cycle panels.
			if m.wizard != nil && m.wizard.compPickerOpen {
				m.wizard.compPickerOpen = false
				m.wizard.syncFocus()
				return m, tea.Batch(cmds...)
			}
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
				if msg.String() == "t" {
					m.minikubeShowLog = !m.minikubeShowLog
					if m.minikubeShowLog {
						m.minikubeVP.SetContent(wrapContent(m.minikubeLogBuf, m.minikubeVP.Width))
						m.minikubeVP.GotoBottom()
					} else {
						m.minikubeVP.SetContent(wrapContent(m.minikubeBuf, m.minikubeVP.Width))
					}
				} else {
					m.minikubeVP, cmd = m.minikubeVP.Update(msg)
				}
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

// printLine appends a line to the commands panel and scrolls to the bottom.
func (m *model) printLine(s string) {
	appendToVP(&m.commandsBuf, &m.commandsVP, s)
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
	go watchMinikubeLog(ctx, minikubeLogPath(name), name)
	go watchSkaffoldLog(ctx, skaffoldLogPath(name), name)
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
		// Always open the wizard — it pre-populates from the current instance.
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
		m.printLine(fmt.Sprintf("  stopping %s …", name))
		// Cancel instanceCtx — kills skaffold, MFE, and log watchers.
		cancelInstance()
		instanceCtx, cancelInstance = context.WithCancel(context.Background())
		m.instance.Name = ""
		delete(m.configs, name)
		m.minikubeBuf = nil
		m.minikubeLogBuf = nil
		m.skaffoldBuf = nil
		m.minikubeVP.SetContent("")
		m.skaffoldVP.SetContent("")
		m.fullscreenTarget = 1 // back to fullscreen with no instance
		sp := m.statePath
		go func() {
			prog.Send(cmdActiveMsg(+1))
			// Kill any persisted MFE process group (covers restarted-session scenario
			// where cancelInstance() above has no live process to signal).
			if state, err := LoadState(sp); err == nil {
				if inst, ok := state.Instances[name]; ok {
					killProcessGroup(inst.MFEPGID)
				}
			}
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

	// Tab/Shift+Tab: cycle fields when picker is closed; close picker when open.
	switch key {
	case "tab":
		if wiz.compPickerOpen {
			wiz.compPickerOpen = false
			wiz.syncFocus()
		} else {
			wiz.custField = (wiz.custField + 1) % custNumFields
			if wiz.custField == custFieldComponents {
				wiz.custSelectedIdx = 0
			}
			wiz.syncFocus()
		}
		return
	case "shift+tab":
		if wiz.compPickerOpen {
			wiz.compPickerOpen = false
			wiz.syncFocus()
		} else {
			wiz.custField = (wiz.custField - 1 + custNumFields) % custNumFields
			if wiz.custField == custFieldComponents {
				wiz.custSelectedIdx = len(wiz.selectedComps) // land on Add button
			}
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
			wiz.custSelectedIdx = 0
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
				if wiz.custSelectedIdx > 0 {
					wiz.custSelectedIdx--
				} else {
					wiz.custField = custFieldRAM
					wiz.syncFocus()
				}
			case "down":
				if wiz.custSelectedIdx < len(wiz.selectedComps) {
					wiz.custSelectedIdx++
				} else {
					wiz.custField = custFieldMFE
					wiz.syncFocus()
				}
			case "x":
				if wiz.custSelectedIdx < len(wiz.selectedComps) {
					wiz.selectedComps = append(wiz.selectedComps[:wiz.custSelectedIdx], wiz.selectedComps[wiz.custSelectedIdx+1:]...)
					// If we removed the last item, clamp to Add button.
					if wiz.custSelectedIdx > len(wiz.selectedComps) {
						wiz.custSelectedIdx = len(wiz.selectedComps)
					}
				}
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
			wiz.custSelectedIdx = len(wiz.selectedComps) // land on Add button
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

	m.switchToInstance(name)
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

	m.switchToInstance(name)
	m.fullscreenTarget = 0
	m.executeStart(startParams{
		cpu:          cpu,
		ram:          ram,
		skaffoldPath: instanceCfg.Skaffold.Path,
		skaffoldMode: mode,
		mfePath:      instanceCfg.MFE.Path,
	})
}

// executeStart launches all instance processes:
//   - MFE starts immediately (no dependency on minikube).
//   - minikube start runs (output → /tmp log, visible in the Minikube panel).
//   - skaffold starts once minikube completes.
func (m *model) executeStart(p startParams) {
	m.printLine(fmt.Sprintf("$ minikube start --cpus %s --memory %s", p.cpu, p.ram))
	if p.skaffoldPath != "" {
		m.printLine(fmt.Sprintf("  + skaffold %s --filename %s", p.skaffoldMode, p.skaffoldPath))
	}
	if p.mfePath != "" {
		m.printLine(fmt.Sprintf("  + npm start (%s)", p.mfePath))
	}
	m.printLine("  (minikube output → Minikube panel)")

	// Always start on the Minikube tab; reset the auto-switch so it fires once
	// when kubectl becomes ready.
	m.minikubeShowLog = true
	m.minikubeAutoSwitched = false
	m.minikubeVP.SetContent(wrapContent(m.minikubeLogBuf, m.minikubeVP.Width))

	name := m.instance.Name
	sp := m.statePath

	// MFE starts immediately — no dependency on minikube.
	if p.mfePath != "" {
		go startMFE(p.mfePath)
	}

	// minikube → skaffold chain. Output goes to the log file tailed by the panel.
	go func() {
		prog.Send(cmdActiveMsg(+1))
		err := startMinikubeToLog(name, p.cpu, p.ram)
		prog.Send(cmdActiveMsg(-1))
		if err != nil {
			prog.Send(commandLineMsg(fmt.Sprintf("[minikube: %v]", err)))
			return
		}
		if name != "" {
			if err := MarkActive(sp, name); err != nil {
				prog.Send(commandLineMsg("warning: " + err.Error()))
			} else {
				prog.Send(commandLineMsg("instance '" + name + "' marked active"))
			}
		}
		// kubectl get pods: if it succeeds, populate the buffer and trigger
		// the one-time auto-switch from minikube log → kubectl tab.
		if lines, ok := kubectlGetPodsOnce(); ok {
			prog.Send(minikubeSetMsg(lines))
			prog.Send(minikubeReadyMsg{})
		}
		if p.skaffoldPath != "" {
			go startSkaffoldToLog(name, p.skaffoldPath, p.skaffoldMode)
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

	type vpSpec struct {
		vp  *viewport.Model
		buf *[]string
		w, h int
	}
	minikubeBufPtr := &m.minikubeBuf
	if m.minikubeShowLog {
		minikubeBufPtr = &m.minikubeLogBuf
	}
	specs := []vpSpec{
		{&m.minikubeVP, minikubeBufPtr, vpW_L, vpH_top},
		{&m.skaffoldVP, &m.skaffoldBuf, vpW_R, vpH_top},
		{&m.commandsVP, &m.commandsBuf, vpW_L, vpH_commands},
		{&m.mfeVP, &m.mfeBuf, vpW_R, vpH_mfe},
	}
	if m.minikubeVP.Width == 0 {
		// First resize: create viewports so keymaps are initialised.
		for _, s := range specs {
			*s.vp = viewport.New(s.w, s.h)
			s.vp.SetContent(wrapContent(*s.buf, s.w))
			s.vp.GotoBottom()
		}
		m.helpOverlayVP = viewport.New(vpW_L, vpH_overlay)
	} else {
		// Subsequent resizes: update dimensions then re-wrap content at new width.
		for _, s := range specs {
			s.vp.Width = s.w
			s.vp.Height = s.h
			s.vp.SetContent(wrapContent(*s.buf, s.w))
			s.vp.GotoBottom()
		}
		m.helpOverlayVP.Width = vpW_L
		m.helpOverlayVP.Height = vpH_overlay
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
