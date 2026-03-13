package tui

import (
	"fmt"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/thompsonja/its_tui/config"
	"github.com/thompsonja/its_tui/step"
)

// flipStep controls animation speed: how much flipProgress advances per 60fps tick.
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
		m.spinnerTick++
		// Update spinner frames for any active (non-pending, non-done) steps.
		if len(m.steps) > 0 {
			changed := false
			for _, s := range m.steps {
				if !s.done && !s.pending && s.bufIdx < len(m.commandsBuf) {
					m.commandsBuf[s.bufIdx] = "  " + spinnerFrames[m.spinnerTick%len(spinnerFrames)] + " " + s.label
					changed = true
				}
			}
			if changed {
				m.commandsVP.SetContent(wrapContent(m.commandsBuf, m.commandsVP.Width))
			}
		}
		// Expire flash notification in the panel title.
		if m.flashMsg != "" && m.spinnerTick >= m.flashUntil {
			m.flashMsg = ""
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
				m.resizePanels()
			}
		}

	// ── Streaming log ingestion ──────────────────────────────────────────────

	case step.LineMsg:
		pid, idx := m.panelAndIdx(msg.ID)
		if idx >= 0 {
			pv := &m.panels[pid]
			pv.bufs[idx] = appendLine(pv.bufs[idx], msg.Line)
			if pv.activeIdx == idx {
				vp := &m.panelVPs[pid]
				vp.SetContent(wrapContent(pv.bufs[idx], vp.Width))
				vp.GotoBottom()
			}
		}

	case step.SetMsg:
		pid, idx := m.panelAndIdx(msg.ID)
		if idx >= 0 {
			pv := &m.panels[pid]
			pv.bufs[idx] = msg.Content
			if pv.activeIdx == idx {
				vp := &m.panelVPs[pid]
				vp.SetContent(wrapContent(pv.bufs[idx], vp.Width))
			}
		}

	case commandLineMsg:
		appendToVP(&m.commandsBuf, &m.commandsVP, string(msg))

	case step.CommandMsg:
		appendToVP(&m.commandsBuf, &m.commandsVP, msg.Text)

	case step.PIDMsg:
		sp := m.statePath
		pgid := msg.PID
		go func() { _ = SaveMFEPGID(sp, pgid) }()

	case step.DebugPortMsg:
		if isDebugPortName(msg.PortName) {
			m.debugPorts = append(m.debugPorts, msg)
		} else {
			m.fwdPorts = append(m.fwdPorts, msg)
		}
		m.portsVP.SetContent(m.renderPortsContent())
		m.debugVP.SetContent(m.renderDebugContent())
		sp := m.statePath
		fwdPorts := make([]config.DebugPort, len(m.fwdPorts))
		for i, p := range m.fwdPorts {
			fwdPorts[i] = config.DebugPort{
				LocalPort:    p.LocalPort,
				RemotePort:   p.RemotePort,
				ResourceName: p.ResourceName,
				PortName:     p.PortName,
				Address:      p.Address,
			}
		}
		dbgPorts := make([]config.DebugPort, len(m.debugPorts))
		for i, p := range m.debugPorts {
			dbgPorts[i] = config.DebugPort{
				LocalPort:    p.LocalPort,
				RemotePort:   p.RemotePort,
				ResourceName: p.ResourceName,
				PortName:     p.PortName,
				Address:      p.Address,
			}
		}
		go func() { _ = config.SavePorts(sp, fwdPorts, dbgPorts) }()

	case copyResultMsg:
		m.flashMsg = msg.msg
		m.flashOk = msg.ok
		m.flashUntil = m.spinnerTick + 180 // ~3 s at 60 fps

	case testLineMsg:
		m.testBuf = appendLine(m.testBuf, string(msg))
		if m.isTestsTabActive(PanelBottomRight) {
			m.testVP.SetContent(wrapContent(m.testBuf, m.testVP.Width))
			m.testVP.GotoBottom()
		}

	case testDoneMsg:
		m.testRunning = false
		status := "  [tests passed]"
		if !msg.ok {
			status = "  [tests failed]"
		}
		m.testBuf = appendLine(m.testBuf, status)
		if m.isTestsTabActive(PanelBottomRight) {
			m.testVP.SetContent(wrapContent(m.testBuf, m.testVP.Width))
			m.testVP.GotoBottom()
		}

	case stepDoneMsg:
		m.finishStep(msg.id, msg.ok, msg.label)

	case stepDepReadyMsg:
		m.depReady(msg.id, msg.dep)

	case stepDepFailedMsg:
		sp := m.statePath
		m.printLine(fmt.Sprintf("  ⚠ warning: %s dependency %s failed, skipping", msg.id, msg.failedDep))
		_ = UpdateStepState(sp, msg.id, config.StepStatusSkipped, fmt.Errorf("dependency %s failed", msg.failedDep))
		m.finishStep(msg.id, false, msg.id+" skipped (dependency failed)")

	case stepActivateMsg:
		if s, ok := m.steps[msg.id]; ok {
			s.pending = false
			// Update the line immediately to a spinner frame so it starts animating.
			if s.bufIdx < len(m.commandsBuf) {
				m.commandsBuf[s.bufIdx] = "  " + spinnerFrames[m.spinnerTick%len(spinnerFrames)] + " " + s.label
			}
			m.commandsVP.SetContent(wrapContent(m.commandsBuf, m.commandsVP.Width))
		}
		// AutoActivate: switch the panel view to show this step.
		if def, ok := m.findDef(msg.id); ok && def.meta.autoActivate {
			pid, idx := m.panelAndIdx(msg.id)
			if idx >= 0 {
				pv := &m.panels[pid]
				pv.activeIdx = idx
				m.panelVPs[pid].SetContent(wrapContent(pv.bufs[idx], m.panelVPs[pid].Width))
			}
		}

	case instanceStoppedMsg:
		m.fullscreenTarget = 1

	case clearActiveDefsMsg:
		m.activeDefs = nil

	// ── Key handling ─────────────────────────────────────────────────────────

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit

		case "ctrl+f":
			if m.instanceName != "" {
				if m.fullscreenTarget == 1 {
					m.fullscreenTarget = 0
				} else {
					m.fullscreenTarget = 1
				}
			}
			return m, tea.Batch(cmds...)

		case "esc":
			// Close open picker before anything else.
			if m.wizard != nil && m.wizard.anyPickerOpen() {
				if s := m.wizard.activeState(); s != nil {
					s.pickerOpen = false
				}
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
			if m.wizard != nil && m.wizard.anyPickerOpen() {
				if s := m.wizard.activeState(); s != nil {
					s.pickerOpen = false
				}
				m.wizard.syncFocus()
				return m, tea.Batch(cmds...)
			}
			if m.instanceName != "" {
				if m.flipTarget == 1.0 {
					m.flipTarget = 0.0
				}
				m.cycleFocus(+1)
			}
			return m, tea.Batch(cmds...)

		case "shift+tab":
			// Picker open: Shift+Tab closes only the picker, does not cycle panels.
			if m.wizard != nil && m.wizard.anyPickerOpen() {
				if s := m.wizard.activeState(); s != nil {
					s.pickerOpen = false
				}
				m.wizard.syncFocus()
				return m, tea.Batch(cmds...)
			}
			if m.instanceName != "" {
				if m.flipTarget == 1.0 {
					m.flipTarget = 0.0
				}
				m.cycleFocus(-1)
			}
			return m, tea.Batch(cmds...)

		default:
			var cmd tea.Cmd
			switch m.focused {
			case panelCommands:
				if m.flipTarget == 1.0 {
					switch m.overlay {
					case overlayHelp:
						m.helpOverlayVP, cmd = m.helpOverlayVP.Update(msg)
					case overlayWizard:
						m.handleWizardKey(msg)
						if m.wizard != nil {
							m.wizard.reEvalDynamicFields()
						}
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
			default: // content panels: panelMinikube, panelSkaffold, panelMFE
				pid, ok := m.focusedPanelID()
				if !ok {
					break
				}
				pv := &m.panels[pid]
				totalTabs := len(pv.defs)
				if pid == PanelTopRight && len(m.fwdPorts) > 0 {
					totalTabs++ // virtual Ports tab
				}
				if pid == PanelTopRight && len(m.debugPorts) > 0 {
					totalTabs++ // virtual Debug tab
				}
				if pid == PanelBottomRight && len(m.cfg.Tests) > 0 {
					totalTabs++ // virtual Tests tab
				}
				if m.searchMode {
					switch msg.String() {
					case "esc":
						m.searchMode = false
						m.searchQuery = ""
						m.searchInput.Reset()
						m.searchInput.Blur()
					default:
						var newInput textinput.Model
						newInput, cmd = m.searchInput.Update(msg)
						m.searchInput = newInput
						m.searchQuery = m.searchInput.Value()
						m.refreshFocusedPanel()
					}
				} else if msg.String() == "/" {
					m.searchMode = true
					m.searchQuery = ""
					m.searchInput.Reset()
					m.searchInput.Focus()
				} else if msg.String() == "t" && totalTabs > 1 {
					pv.activeIdx = (pv.activeIdx + 1) % totalTabs
					if pv.activeIdx < len(pv.defs) {
						buf := pv.bufs[pv.activeIdx]
						m.panelVPs[pid].SetContent(wrapContent(buf, m.panelVPs[pid].Width))
						m.panelVPs[pid].GotoBottom()
					} else if m.isPortsTabActive(pid) {
						m.portsVP.SetContent(m.renderPortsContent())
						m.portsVP.GotoBottom()
					} else if m.isDebugTabActive(pid) {
						m.debugVP.SetContent(m.renderDebugContent())
						m.debugVP.GotoBottom()
					} else if m.isTestsTabActive(pid) {
						m.testVP.SetContent(wrapContent(m.testBuf, m.testVP.Width))
						m.testVP.GotoBottom()
					}
				} else if m.isDebugTabActive(pid) {
					if msg.String() == "c" {
						json := m.launchJSONString()
						go func() {
							if err := copyToClipboard(json); err != nil {
								prog.Send(copyResultMsg{ok: false, msg: err.Error()})
							} else {
								prog.Send(copyResultMsg{ok: true, msg: "copied to clipboard"})
							}
						}()
					} else {
						m.debugVP, cmd = m.debugVP.Update(msg)
					}
				} else if m.isPortsTabActive(pid) {
					m.portsVP, cmd = m.portsVP.Update(msg)
				} else if m.isTestsTabActive(pid) {
					m.testVP, cmd = m.testVP.Update(msg)
				} else {
					m.panelVPs[pid], cmd = m.panelVPs[pid].Update(msg)
				}
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
	const title = 2 // title text (1) + MarginBottom(1)
	const input = 2 // separator (1) + textinput (1)

	grid := m.height - 1 // 1 row reserved for the top bar

	var (
		vpW_L, vpW_R                                int
		vpH_top, vpH_commands, vpH_mfe, vpH_overlay int
	)

	if m.fullscreenProgress >= 1 {
		vpW_L = max(1, m.width-border)
		vpW_R = vpW_L
		vpH_top = max(1, grid-border-title)
		vpH_commands = max(1, grid-border-title-input)
		vpH_mfe = vpH_top
		vpH_overlay = vpH_top
	} else {
		colL := m.width / 2
		colR := m.width - colL
		rowT := grid / 2
		rowB := grid - rowT

		vpW_L = max(1, colL-border)
		vpW_R = max(1, colR-border)
		vpH_top = max(1, rowT-border-title)
		vpH_commands = max(1, rowB-border-title-input)
		vpH_mfe = max(1, rowB-border-title)
		vpH_overlay = max(1, rowB-border-title)
	}

	type contentSpec struct {
		pid  PanelID
		w, h int
	}
	contentSpecs := []contentSpec{
		{PanelTopLeft, vpW_L, vpH_top},
		{PanelTopRight, vpW_R, vpH_top},
		{PanelBottomRight, vpW_R, vpH_mfe},
	}

	firstTime := m.panelVPs[0].Width == 0
	if firstTime {
		for _, s := range contentSpecs {
			pv := &m.panels[s.pid]
			m.panelVPs[s.pid] = viewport.New(s.w, s.h)
			m.panelVPs[s.pid].SetContent(wrapContent(pv.activeBuf(), s.w))
			m.panelVPs[s.pid].GotoBottom()
		}
		m.commandsVP = viewport.New(vpW_L, vpH_commands)
		m.commandsVP.SetContent(wrapContent(m.commandsBuf, vpW_L))
		m.commandsVP.GotoBottom()
		m.helpOverlayVP = viewport.New(vpW_L, vpH_overlay)
		m.portsVP = viewport.New(vpW_R, vpH_top)
		m.debugVP = viewport.New(vpW_R, vpH_top)
		m.testVP = viewport.New(vpW_R, vpH_mfe)
	} else {
		for _, s := range contentSpecs {
			pv := &m.panels[s.pid]
			vp := &m.panelVPs[s.pid]
			vp.Width = s.w
			vp.Height = s.h
			vp.SetContent(wrapContent(pv.activeBuf(), s.w))
			vp.GotoBottom()
		}
		m.commandsVP.Width = vpW_L
		m.commandsVP.Height = vpH_commands
		m.commandsVP.SetContent(wrapContent(m.commandsBuf, vpW_L))
		m.commandsVP.GotoBottom()
		m.helpOverlayVP.Width = vpW_L
		m.helpOverlayVP.Height = vpH_overlay
		m.portsVP.Width = vpW_R
		m.portsVP.Height = vpH_top
		m.debugVP.Width = vpW_R
		m.debugVP.Height = vpH_top
		m.testVP.Width = vpW_R
		m.testVP.Height = vpH_mfe
	}
	m.portsVP.SetContent(m.renderPortsContent())
	m.debugVP.SetContent(m.renderDebugContent())
	m.testVP.SetContent(wrapContent(m.testBuf, m.testVP.Width))
	m.helpOverlayVP.SetContent(m.helpContent(vpW_L))
	m.input.Width = vpW_L

	if m.wizard != nil {
		inputW := max(20, vpW_L-16)
		for i := range m.wizard.states {
			if m.wizard.states[i].spec.Kind != FieldKindSelect {
				m.wizard.states[i].pickerSearch.Width = inputW
			}
		}
	}
}
