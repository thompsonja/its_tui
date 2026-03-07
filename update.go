package tui

import (
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
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
			frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
			changed := false
			for _, s := range m.steps {
				if !s.done && !s.pending && s.bufIdx < len(m.commandsBuf) {
					m.commandsBuf[s.bufIdx] = "  " + frames[m.spinnerTick%len(frames)] + " " + s.label
					changed = true
				}
			}
			if changed {
				m.commandsVP.SetContent(wrapContent(m.commandsBuf, m.commandsVP.Width))
			}
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
		}

	case minikubeReadyMsg:
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

	case stepDoneMsg:
		m.finishStep(msg.id, msg.ok, msg.label)

	case stepActivateMsg:
		if s, ok := m.steps[msg.id]; ok {
			s.pending = false
			// Update the line immediately to a spinner frame so it starts animating.
			frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
			if s.bufIdx < len(m.commandsBuf) {
				m.commandsBuf[s.bufIdx] = "  " + frames[m.spinnerTick%len(frames)] + " " + s.label
			}
			m.commandsVP.SetContent(wrapContent(m.commandsBuf, m.commandsVP.Width))
		}

	case instanceStoppedMsg:
		m.fullscreenTarget = 1

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
			// Close MFE picker.
			if m.wizard != nil && m.wizard.mfePickerOpen {
				m.wizard.mfePickerOpen = false
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
			if m.wizard != nil && (m.wizard.compPickerOpen || m.wizard.mfePickerOpen) {
				m.wizard.compPickerOpen = false
				m.wizard.mfePickerOpen = false
				m.wizard.syncFocus()
				return m, tea.Batch(cmds...)
			}
			if m.instance.Name != "" {
				if m.flipTarget == 1.0 {
					m.flipTarget = 0.0
				}
				m.cycleFocus(+1)
			}
			return m, tea.Batch(cmds...)

		case "shift+tab":
			// Picker open: Shift+Tab closes only the picker, does not cycle panels.
			if m.wizard != nil && (m.wizard.compPickerOpen || m.wizard.mfePickerOpen) {
				m.wizard.compPickerOpen = false
				m.wizard.mfePickerOpen = false
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
		for _, s := range specs {
			*s.vp = viewport.New(s.w, s.h)
			s.vp.SetContent(wrapContent(*s.buf, s.w))
			s.vp.GotoBottom()
		}
		m.helpOverlayVP = viewport.New(vpW_L, vpH_overlay)
	} else {
		for _, s := range specs {
			s.vp.Width = s.w
			s.vp.Height = s.h
			s.vp.SetContent(wrapContent(*s.buf, s.w))
			s.vp.GotoBottom()
		}
		m.helpOverlayVP.Width = vpW_L
		m.helpOverlayVP.Height = vpH_overlay
	}
	m.helpOverlayVP.SetContent(helpContent(vpW_L))
	m.input.Width = vpW_L

	if m.wizard != nil {
		inputW := max(20, vpW_L-16)
		m.wizard.nameInput.Width = inputW
		m.wizard.configInput.Width = inputW
		m.wizard.custName.Width = inputW
		m.wizard.compPickerSearch.Width = inputW
		m.wizard.mfePickerSearch.Width = inputW
	}
}
