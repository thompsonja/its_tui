package main

import (
	"fmt"
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

	case tickMsg:
		cmds = append(cmds, tickCmd())
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
		m.minikubeVP.SetContent(joinLines(m.minikubeBuf))
		m.minikubeVP.GotoBottom()

	case minikubeSetMsg:
		m.minikubeBuf = []string(msg)
		m.minikubeVP.SetContent(joinLines(m.minikubeBuf))
		// No GotoBottom — preserve scroll position while the user reads.

	case skaffoldLineMsg:
		m.skaffoldBuf = appendLine(m.skaffoldBuf, string(msg))
		m.skaffoldVP.SetContent(joinLines(m.skaffoldBuf))
		m.skaffoldVP.GotoBottom()

	case commandLineMsg:
		m.commandsBuf = appendLine(m.commandsBuf, string(msg))
		m.commandsVP.SetContent(joinLines(m.commandsBuf))
		m.commandsVP.GotoBottom()

	case mfeLineMsg:
		m.mfeBuf = appendLine(m.mfeBuf, string(msg))
		m.mfeVP.SetContent(joinLines(m.mfeBuf))
		m.mfeVP.GotoBottom()

	// ── Key handling ─────────────────────────────────────────────────────────

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit

		case "ctrl+f":
			if m.fullscreenTarget == 1 {
				m.fullscreenTarget = 0
			} else {
				m.fullscreenTarget = 1
			}
			return m, tea.Batch(cmds...)

		case "esc":
			if m.fullscreenTarget == 1 {
				m.fullscreenTarget = 0
				return m, tea.Batch(cmds...)
			}
			if m.flipTarget == 1.0 {
				m.flipTarget = 0.0
			}
			return m, tea.Batch(cmds...)

		case "tab":
			// Tab exits the help overlay before cycling.
			if m.flipTarget == 1.0 {
				m.flipTarget = 0.0
			}
			m.cycleFocus(+1)
			return m, tea.Batch(cmds...)

		case "shift+tab":
			if m.flipTarget == 1.0 {
				m.flipTarget = 0.0
			}
			m.cycleFocus(-1)
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

	// ── Mouse (passed to focused viewport) ───────────────────────────────────

	case tea.MouseMsg:
		var cmd tea.Cmd
		switch m.focused {
		case panelMinikube:
			m.minikubeVP, cmd = m.minikubeVP.Update(msg)
		case panelSkaffold:
			m.skaffoldVP, cmd = m.skaffoldVP.Update(msg)
		case panelCommands:
			if m.flipTarget == 1.0 {
				if m.overlay == overlayHelp {
					m.helpOverlayVP, cmd = m.helpOverlayVP.Update(msg)
				}
				// Wizard has no scrollable viewport — mouse is a no-op there.
			} else {
				m.commandsVP, cmd = m.commandsVP.Update(msg)
			}
		case panelMFE:
			m.mfeVP, cmd = m.mfeVP.Update(msg)
		}
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

// dispatchCommand routes typed text to internal command handlers.
func (m *model) dispatchCommand(line string) {
	parts := strings.Fields(line)
	if len(parts) == 0 {
		return
	}
	switch parts[0] {
	case "help":
		m.overlay = overlayHelp
		m.flipTarget = 1.0

	case "start":
		m.overlay = overlayWizard
		m.wizard = &startWizard{cpuIdx: 1, ramIdx: 1} // default: 4 cores, 4 GB
		m.flipTarget = 1.0

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
					break
				}
			}
			if !found {
				m.commandsBuf = appendLine(m.commandsBuf, "unknown theme: "+name+" (try: "+themeNames()+")")
			}
		}
		m.commandsVP.SetContent(joinLines(m.commandsBuf))
		m.commandsVP.GotoBottom()

	default:
		m.commandsBuf = appendLine(m.commandsBuf, "$ "+line)
		m.commandsBuf = appendLine(m.commandsBuf, "unknown command: "+parts[0])
		m.commandsVP.SetContent(joinLines(m.commandsBuf))
		m.commandsVP.GotoBottom()
	}
}

// ── Wizard ───────────────────────────────────────────────────────────────────

const wizardNumFields = 3 // CPU, RAM, Buttons

func (m *model) handleWizardKey(msg tea.KeyMsg) {
	wiz := m.wizard
	if wiz == nil {
		return
	}
	switch msg.String() {
	case "up":
		wiz.field = (wiz.field - 1 + wizardNumFields) % wizardNumFields
	case "down":
		wiz.field = (wiz.field + 1) % wizardNumFields
	case "left":
		switch wiz.field {
		case 0:
			if wiz.cpuIdx > 0 {
				wiz.cpuIdx--
			}
		case 1:
			if wiz.ramIdx > 0 {
				wiz.ramIdx--
			}
		case 2:
			if wiz.confirmIdx > 0 {
				wiz.confirmIdx--
			}
		}
	case "right":
		switch wiz.field {
		case 0:
			if wiz.cpuIdx < len(cpuOptions)-1 {
				wiz.cpuIdx++
			}
		case 1:
			if wiz.ramIdx < len(ramOptions)-1 {
				wiz.ramIdx++
			}
		case 2:
			if wiz.confirmIdx < 1 {
				wiz.confirmIdx++
			}
		}
	case "enter":
		if wiz.field == 2 {
			if wiz.confirmIdx == 0 {
				m.executeStart()
			}
			m.flipTarget = 0.0
		}
	}
}

func (m *model) executeStart() {
	cpu := cpuArgs[m.wizard.cpuIdx]
	ram := ramArgs[m.wizard.ramIdx]
	m.commandsBuf = appendLine(m.commandsBuf, fmt.Sprintf("$ minikube start --cpus %s --memory %s", cpu, ram))
	m.commandsVP.SetContent(joinLines(m.commandsBuf))
	m.commandsVP.GotoBottom()
	go streamToPanel(
		func(s string) tea.Msg { return commandLineMsg(s) },
		"minikube", "start", "--cpus", cpu, "--memory", ram,
	)
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
		m.minikubeVP.SetContent(joinLines(m.minikubeBuf))
		m.skaffoldVP.SetContent(joinLines(m.skaffoldBuf))
		m.commandsVP.SetContent(joinLines(m.commandsBuf))
		m.mfeVP.SetContent(joinLines(m.mfeBuf))
		m.minikubeVP.GotoBottom()
		m.skaffoldVP.GotoBottom()
		m.commandsVP.GotoBottom()
	} else {
		// Subsequent resizes: update dimensions in-place to preserve scroll position.
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
	}
	// Help content uses width for column layout — refresh on every resize.
	m.helpOverlayVP.SetContent(helpContent(vpW_L))

	// Textinput width tracks the panel content area.
	m.input.Width = vpW_L
}
