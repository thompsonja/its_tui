package tui

import (
	"fmt"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/thompsonja/its_tui/config"
	"github.com/thompsonja/its_tui/step"
)

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.vs.width, m.vs.height = msg.Width, msg.Height
		m.vs.ready = true
		m.resizePanels()
		return m, tea.Batch(cmds...)

	case cmdActiveMsg:
		m.vs.runningCmds += int(msg)
		if m.vs.runningCmds < 0 {
			m.vs.runningCmds = 0
		}

	case tickMsg:
		cmds = append(cmds, tickCmd())
		m.vs.spinnerTick++
		// Update spinner frames for active (non-pending, non-done) steps.
		if len(m.vs.steps) > 0 {
			changed := false
			for _, s := range m.vs.steps {
				if !s.done && !s.pending && s.bufIdx < len(m.app.commandsBuf) {
					m.app.commandsBuf[s.bufIdx] = "  " + spinnerFrames[m.vs.spinnerTick%len(spinnerFrames)] + " " + s.label
					changed = true
				}
			}
			if changed {
				m.vs.commandsVP.SetContent(wrapContent(m.app.commandsBuf, m.vs.commandsVP.Width))
			}
		}
		// Expire flash notification.
		if m.vs.flashMsg != "" && m.vs.spinnerTick >= m.vs.flashUntil {
			m.vs.flashMsg = ""
		}
		// Advance card-flip animation.
		if newP, settled := advanceAnim(m.vs.flipProgress, m.vs.flipTarget, flipStep); newP != m.vs.flipProgress {
			m.vs.flipProgress = newP
			if settled && m.vs.flipTarget == 0 {
				m.vs.overlay = overlayNone
				m.vs.wizard = nil
			}
		}
		// Advance fullscreen animation.
		if newP, settled := advanceAnim(m.vs.fullscreenProgress, m.vs.fullscreenTarget, fullscreenStep); newP != m.vs.fullscreenProgress {
			m.vs.fullscreenProgress = newP
			if settled {
				m.resizePanels()
			}
		}

	// ── Streaming log ingestion ───────────────────────────────────────────────

	case step.LineMsg:
		if pid, idx, ok := m.app.panelAndIdx(msg.ID); ok {
			pv := &m.app.panels[pid]
			pv.bufs[idx] = appendLine(pv.bufs[idx], msg.Line)
			if pv.activeIdx == idx {
				vp := &m.vs.panelVPs[pid]
				vp.SetContent(wrapContent(pv.bufs[idx], vp.Width))
				vp.GotoBottom()
			}
		}

	case step.SetMsg:
		if pid, idx, ok := m.app.panelAndIdx(msg.ID); ok {
			pv := &m.app.panels[pid]
			pv.bufs[idx] = msg.Content
			if pv.activeIdx == idx {
				vp := &m.vs.panelVPs[pid]
				vp.SetContent(wrapContent(pv.bufs[idx], vp.Width))
			}
		}

	case commandLineMsg:
		appendToVP(&m.app.commandsBuf, &m.vs.commandsVP, string(msg))

	case step.CommandMsg:
		appendToVP(&m.app.commandsBuf, &m.vs.commandsVP, msg.Text)

	case step.PIDMsg:
		sp := m.app.statePath
		pgid := msg.PID
		go func() {
			if err := SaveMFEPGID(sp, pgid); err != nil {
				debugLog("SaveMFEPGID: %v", err)
			}
		}()

	case step.DebugPortMsg:
		if isDebugPortName(msg.PortName) {
			m.app.debugPorts = append(m.app.debugPorts, msg)
		} else {
			m.app.fwdPorts = append(m.app.fwdPorts, msg)
		}
		m.vs.portsVP.SetContent(m.vs.renderPortsContent(m.app))
		m.vs.debugVP.SetContent(m.vs.renderDebugContent(m.app))
		sp := m.app.statePath
		fwdPorts := make([]config.DebugPort, len(m.app.fwdPorts))
		for i, p := range m.app.fwdPorts {
			fwdPorts[i] = config.DebugPort{
				LocalPort:    p.LocalPort,
				RemotePort:   p.RemotePort,
				ResourceName: p.ResourceName,
				PortName:     p.PortName,
				Address:      p.Address,
			}
		}
		dbgPorts := make([]config.DebugPort, len(m.app.debugPorts))
		for i, p := range m.app.debugPorts {
			dbgPorts[i] = config.DebugPort{
				LocalPort:    p.LocalPort,
				RemotePort:   p.RemotePort,
				ResourceName: p.ResourceName,
				PortName:     p.PortName,
				Address:      p.Address,
			}
		}
		go func() {
			if err := config.SavePorts(sp, fwdPorts, dbgPorts); err != nil {
				debugLog("SavePorts: %v", err)
			}
		}()
		if isDebugPortName(msg.PortName) {
			ports := append([]step.DebugPortMsg(nil), m.app.debugPorts...)
			instanceName := m.app.instanceName
			workspaceDir := m.app.workspaceDir
			go func() {
				if err := updateVSCodeLaunchJSON(workspaceDir, instanceName, ports); err != nil {
					prog.Send(commandLineMsg(fmt.Sprintf("  warning: failed to update .vscode/launch.json: %v", err)))
				}
			}()
		}

	case copyResultMsg:
		m.vs.flashMsg = msg.msg
		m.vs.flashOk = msg.ok
		m.vs.flashUntil = m.vs.spinnerTick + 180 // ~3 s at 60 fps

	case testLineMsg:
		m.app.testBuf = appendLine(m.app.testBuf, string(msg))
		if m.app.isTestsTabActive(PanelBottomRight) {
			m.vs.testVP.SetContent(wrapContent(m.app.testBuf, m.vs.testVP.Width))
			m.vs.testVP.GotoBottom()
		}

	case testDoneMsg:
		m.app.testRunning = false
		status := "  [tests passed]"
		if !msg.ok {
			status = "  [tests failed]"
		}
		m.app.testBuf = appendLine(m.app.testBuf, status)
		if m.app.isTestsTabActive(PanelBottomRight) {
			m.vs.testVP.SetContent(wrapContent(m.app.testBuf, m.vs.testVP.Width))
			m.vs.testVP.GotoBottom()
		}

	case autoStatusMsg:
		m.dispatchCommand("status")

	case allStepsReadyMsg:
		sp := m.app.statePath
		state, err := LoadState(sp)
		if err == nil && state.Instance != nil && state.Instance.StartedAt != "" && state.Instance.ReadyAt != "" {
			started, err1 := time.Parse(time.RFC3339, state.Instance.StartedAt)
			ready, err2 := time.Parse(time.RFC3339, state.Instance.ReadyAt)
			if err1 == nil && err2 == nil {
				duration := ready.Sub(started)
				m.printLine(fmt.Sprintf("  ✓ All steps ready (startup took %s)", duration.Round(time.Millisecond)))
			}
		}

	case stepDoneMsg:
		m.finishStep(msg.id, msg.ok, msg.label)

	case stepDepReadyMsg:
		m.depReady(msg.id, msg.dep)

	case stepDepFailedMsg:
		sp := m.app.statePath
		m.printLine(fmt.Sprintf("  ⚠ warning: %s dependency %s failed, skipping", msg.id, msg.failedDep))
		if err := UpdateStepState(sp, msg.id, config.StepStatusSkipped, fmt.Errorf("dependency %s failed", msg.failedDep)); err != nil {
			debugLog("UpdateStepState %q skipped: %v", msg.id, err)
		}
		m.finishStep(msg.id, false, msg.id+" skipped (dependency failed)")

	case stepActivateMsg:
		if s, ok := m.vs.steps[msg.id]; ok {
			s.pending = false
			if s.bufIdx < len(m.app.commandsBuf) {
				m.app.commandsBuf[s.bufIdx] = "  " + spinnerFrames[m.vs.spinnerTick%len(spinnerFrames)] + " " + s.label
			}
			m.vs.commandsVP.SetContent(wrapContent(m.app.commandsBuf, m.vs.commandsVP.Width))
		}
		if def, ok := m.app.findDef(msg.id); ok && def.meta.autoActivate {
			if pid, idx, ok := m.app.panelAndIdx(msg.id); ok {
				pv := &m.app.panels[pid]
				pv.activeIdx = idx
				m.vs.panelVPs[pid].SetContent(wrapContent(pv.bufs[idx], m.vs.panelVPs[pid].Width))
			}
		}

	case instanceStoppedMsg:
		m.vs.fullscreenTarget = 1

	case clearActiveDefsMsg:
		m.app.activeDefs = nil

	// ── Key handling ──────────────────────────────────────────────────────────

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit

		case "ctrl+f":
			if m.app.instanceName != "" {
				if m.vs.fullscreenTarget == 1 {
					m.vs.fullscreenTarget = 0
				} else {
					m.vs.fullscreenTarget = 1
				}
			}
			return m, tea.Batch(cmds...)

		case "esc":
			if m.vs.wizard != nil && m.vs.wizard.anyPickerOpen() {
				if s := m.vs.wizard.activeState(); s != nil {
					s.pickerOpen = false
				}
				m.vs.wizard.syncFocus()
				return m, tea.Batch(cmds...)
			}
			if m.vs.flipTarget == 1.0 {
				m.vs.flipTarget = 0.0
				return m, tea.Batch(cmds...)
			}
			if m.vs.fullscreenTarget == 1 {
				m.vs.fullscreenTarget = 0
			}
			return m, tea.Batch(cmds...)

		case "tab":
			if m.vs.wizard != nil && m.vs.wizard.anyPickerOpen() {
				if s := m.vs.wizard.activeState(); s != nil {
					s.pickerOpen = false
				}
				m.vs.wizard.syncFocus()
				return m, tea.Batch(cmds...)
			}
			if m.app.instanceName != "" {
				if m.vs.flipTarget == 1.0 {
					m.vs.flipTarget = 0.0
				}
				m.cycleFocus(+1)
			}
			return m, tea.Batch(cmds...)

		case "shift+tab":
			if m.vs.wizard != nil && m.vs.wizard.anyPickerOpen() {
				if s := m.vs.wizard.activeState(); s != nil {
					s.pickerOpen = false
				}
				m.vs.wizard.syncFocus()
				return m, tea.Batch(cmds...)
			}
			if m.app.instanceName != "" {
				if m.vs.flipTarget == 1.0 {
					m.vs.flipTarget = 0.0
				}
				m.cycleFocus(-1)
			}
			return m, tea.Batch(cmds...)

		default:
			var cmd tea.Cmd
			switch m.vs.focused {
			case panelCommands:
				if m.vs.flipTarget == 1.0 {
					switch m.vs.overlay {
					case overlayHelp:
						m.vs.helpOverlayVP, cmd = m.vs.helpOverlayVP.Update(msg)
					case overlayWizard:
						m.handleWizardKey(msg)
						if m.vs.wizard != nil {
							m.vs.wizard.reEvalDynamicFields()
						}
					}
				} else if msg.String() == "enter" {
					if line := m.vs.input.Value(); line != "" {
						m.vs.input.Reset()
						m.addToHistory(line)
						m.dispatchCommand(line)
					}
				} else if msg.String() == "up" {
					m.historyUp()
				} else if msg.String() == "down" {
					m.historyDown()
				} else {
					m.vs.input, cmd = m.vs.input.Update(msg)
				}
			default: // content panels
				pid, ok := focusedPanelID(m.vs.focused)
				if !ok {
					break
				}
				pv := &m.app.panels[pid]
				totalTabs := len(pv.defs)
				if pid == PanelTopRight && len(m.app.fwdPorts) > 0 {
					totalTabs++
				}
				if pid == PanelTopRight && len(m.app.debugPorts) > 0 {
					totalTabs++
				}
				if pid == PanelBottomRight && len(m.app.cfg.Tests) > 0 {
					totalTabs++
				}
				if m.vs.searchMode {
					switch msg.String() {
					case "esc":
						m.vs.searchMode = false
						m.vs.searchQuery = ""
						m.vs.searchInput.Reset()
						m.vs.searchInput.Blur()
					default:
						var newInput textinput.Model
						newInput, cmd = m.vs.searchInput.Update(msg)
						m.vs.searchInput = newInput
						m.vs.searchQuery = m.vs.searchInput.Value()
						m.refreshFocusedPanel()
					}
				} else if msg.String() == "/" {
					m.vs.searchMode = true
					m.vs.searchQuery = ""
					m.vs.searchInput.Reset()
					m.vs.searchInput.Focus()
				} else if msg.String() == "t" && totalTabs > 1 {
					pv.activeIdx = (pv.activeIdx + 1) % totalTabs
					if pv.activeIdx < len(pv.defs) {
						buf := pv.bufs[pv.activeIdx]
						m.vs.panelVPs[pid].SetContent(wrapContent(buf, m.vs.panelVPs[pid].Width))
						m.vs.panelVPs[pid].GotoBottom()
					} else if m.app.isPortsTabActive(pid) {
						m.vs.portsVP.SetContent(m.vs.renderPortsContent(m.app))
						m.vs.portsVP.GotoBottom()
					} else if m.app.isDebugTabActive(pid) {
						m.vs.debugVP.SetContent(m.vs.renderDebugContent(m.app))
						m.vs.debugVP.GotoBottom()
					} else if m.app.isTestsTabActive(pid) {
						m.vs.testVP.SetContent(wrapContent(m.app.testBuf, m.vs.testVP.Width))
						m.vs.testVP.GotoBottom()
					}
					if m.app.instanceName != "" {
						sp := m.app.statePath
						tabs := [3]int{m.app.panels[0].activeIdx, m.app.panels[1].activeIdx, m.app.panels[2].activeIdx}
						go func() {
							if err := SavePanelTabs(sp, tabs); err != nil {
								debugLog("SavePanelTabs: %v", err)
							}
						}()
					}
				} else if m.app.isDebugTabActive(pid) {
					if msg.String() == "c" {
						json := m.vs.launchJSONString(m.app)
						go func() {
							if err := copyToClipboard(json); err != nil {
								prog.Send(copyResultMsg{ok: false, msg: err.Error()})
							} else {
								prog.Send(copyResultMsg{ok: true, msg: "copied to clipboard"})
							}
						}()
					} else {
						m.vs.debugVP, cmd = m.vs.debugVP.Update(msg)
					}
				} else if m.app.isPortsTabActive(pid) {
					m.vs.portsVP, cmd = m.vs.portsVP.Update(msg)
				} else if m.app.isTestsTabActive(pid) {
					m.vs.testVP, cmd = m.vs.testVP.Update(msg)
				} else {
					m.vs.panelVPs[pid], cmd = m.vs.panelVPs[pid].Update(msg)
				}
			}
			cmds = append(cmds, cmd)
		}
	}

	return m, tea.Batch(cmds...)
}
