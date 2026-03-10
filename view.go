package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// wrapContentSearch wraps content like wrapContent, but highlights lines
// containing query (case-insensitive) with the focused color, and dims
// non-matching lines. If query is empty it behaves like wrapContent.
func wrapContentSearch(buf []string, width int, query string) string {
	if query == "" {
		return wrapContent(buf, width)
	}
	q := strings.ToLower(query)
	hl := lipgloss.NewStyle().Foreground(currentTheme.Focused)
	dm := lipgloss.NewStyle().Foreground(currentTheme.Muted)
	result := make([]string, 0, len(buf))
	for _, line := range buf {
		wrapped := wrapLine(line, width)
		if strings.Contains(strings.ToLower(line), q) {
			result = append(result, hl.Render(wrapped))
		} else {
			result = append(result, dm.Render(wrapped))
		}
	}
	return strings.Join(result, "\n")
}

// refreshFocusedPanel regenerates the focused panel's viewport content,
// applying search highlighting when search mode is active.
func (m *model) refreshFocusedPanel() {
	pid, ok := m.focusedPanelID()
	if !ok {
		return
	}
	pv := &m.panels[pid]
	if pv.activeIdx >= len(pv.bufs) {
		return
	}
	buf := pv.bufs[pv.activeIdx]
	vp := &m.panelVPs[pid]
	if m.searchMode && m.searchQuery != "" {
		vp.SetContent(wrapContentSearch(buf, vp.Width, m.searchQuery))
	} else {
		vp.SetContent(wrapContent(buf, vp.Width))
	}
}

// isDebugPortName returns true when portName indicates a debug-protocol port
// (as opposed to a regular forwarded service port).
func isDebugPortName(portName string) bool {
	switch portName {
	case "dlv", "jvm", "ptvsd", "debugpy", "node", "nodejs":
		return true
	}
	return false
}

// isPortsTabActive reports whether the virtual Ports tab is the active tab on
// the given panel. It is only ever true for PanelTopRight when forwarded ports
// have been collected and activeIdx sits one past the real step defs.
func (m model) isPortsTabActive(pid PanelID) bool {
	if pid != PanelTopRight || len(m.fwdPorts) == 0 {
		return false
	}
	return m.panels[pid].activeIdx == len(m.panels[pid].defs)
}

// isDebugTabActive reports whether the virtual Debug tab is the active tab on
// the given panel. It is only ever true for PanelTopRight when debug ports
// have been collected.
func (m model) isDebugTabActive(pid PanelID) bool {
	if pid != PanelTopRight || len(m.debugPorts) == 0 {
		return false
	}
	portsOffset := 0
	if len(m.fwdPorts) > 0 {
		portsOffset = 1
	}
	return m.panels[pid].activeIdx == len(m.panels[pid].defs)+portsOffset
}

// isTestsTabActive reports whether the virtual Tests tab is active on the
// given panel. Only ever true for PanelBottomRight when tests are configured.
func (m model) isTestsTabActive(pid PanelID) bool {
	if pid != PanelBottomRight || len(m.cfg.Tests) == 0 {
		return false
	}
	return m.panels[pid].activeIdx == len(m.panels[pid].defs)
}

// mfePanelView returns the content for the BottomRight panel, switching to
// the testVP when the virtual Tests tab is active.
func (m model) mfePanelView() string {
	if m.isTestsTabActive(PanelBottomRight) {
		return m.testVP.View()
	}
	return m.panelVPs[PanelBottomRight].View()
}

// skaffoldPanelView returns the content string for the skaffold panel,
// switching to portsVP or debugVP when the corresponding virtual tab is active.
func (m model) skaffoldPanelView() string {
	if m.isPortsTabActive(PanelTopRight) {
		return m.portsVP.View()
	}
	if m.isDebugTabActive(PanelTopRight) {
		return m.debugVP.View()
	}
	return m.panelVPs[PanelTopRight].View()
}

// launchJSONString returns a clean, copy-ready JSON string for a VSCode
// launch.json file using 2-space indentation.
func (m model) launchJSONString() string {
	var lines []string
	lines = append(lines, `{`)
	lines = append(lines, `  "version": "0.2.0",`)
	lines = append(lines, `  "configurations": [`)
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
			lines = append(lines, "    "+line)
		}
		lines = append(lines, "    }"+comma)
	}
	lines = append(lines, `  ]`)
	lines = append(lines, `}`)
	return strings.Join(lines, "\n")
}

// renderPortsContent builds the text displayed in the virtual Ports tab (forwarded service ports only).
func (m model) renderPortsContent() string {
	if len(m.fwdPorts) == 0 {
		return ""
	}
	var lines []string
	lines = append(lines, "  Forwarded ports:")
	for _, p := range m.fwdPorts {
		addr := p.Address
		if addr == "" {
			addr = "127.0.0.1"
		}
		lines = append(lines, fmt.Sprintf("  %-24s %s:%d", p.ResourceName, addr, p.LocalPort))
	}
	return strings.Join(lines, "\n")
}

// renderDebugContent builds the text displayed in the virtual Debug tab (debug ports + launch.json).
func (m model) renderDebugContent() string {
	if len(m.debugPorts) == 0 {
		return ""
	}
	var lines []string
	lines = append(lines, "  Debug ports:")
	for _, p := range m.debugPorts {
		addr := p.Address
		if addr == "" {
			addr = "127.0.0.1"
		}
		lines = append(lines, fmt.Sprintf("  %-24s %s:%d  (%s)", p.ResourceName, addr, p.LocalPort, debugRuntime(p.PortName)))
	}
	lines = append(lines, "")
	lines = append(lines, "  VSCode launch.json:")
	// Indent the clean JSON by 4 spaces for display inside the panel.
	for _, line := range strings.Split(m.launchJSONString(), "\n") {
		lines = append(lines, "    "+line)
	}
	return strings.Join(lines, "\n")
}

func (m model) View() string {
	if !m.ready {
		return "Initializing...\n"
	}

	bar := m.renderTopBar()

	switch {
	case m.fullscreenProgress >= 1:
		return lipgloss.JoinVertical(lipgloss.Left, bar, m.renderFullscreen())
	case m.fullscreenProgress > 0:
		return lipgloss.JoinVertical(lipgloss.Left, bar, m.renderFullscreenTransition())
	}

	colL := m.width / 2
	colR := m.width - colL
	grid := m.height - 1
	rowT := grid / 2
	rowB := grid - rowT

	topRow := lipgloss.JoinHorizontal(lipgloss.Top,
		m.renderScrollPanel(panelMinikube, m.panelTitle(PanelTopLeft, m.focused == panelMinikube), m.panelVPs[PanelTopLeft].View(), colL, rowT),
		m.renderScrollPanel(panelSkaffold, m.panelTitle(PanelTopRight, m.focused == panelSkaffold), m.skaffoldPanelView(), colR, rowT),
	)
	bottomRow := lipgloss.JoinHorizontal(lipgloss.Top,
		m.renderCommandsPanel(colL, rowB),
		m.renderScrollPanel(panelMFE, m.panelTitle(PanelBottomRight, m.focused == panelMFE), m.mfePanelView(), colR, rowB),
	)

	return lipgloss.JoinVertical(lipgloss.Left, bar, topRow, bottomRow)
}

func (m model) fullscreenHint() string {
	if m.instanceName == "" {
		return ""
	}
	var text string
	if m.fullscreenTarget == 1 {
		text = "  ctrl+f to exit fullscreen"
	} else {
		text = "  ctrl+f to fullscreen"
	}
	return lipgloss.NewStyle().Foreground(currentTheme.Muted).Render(text)
}

func (m model) renderFullscreenTransition() string {
	p := m.fullscreenProgress
	grid := m.height - 1
	const border, titleH, inputH = 2, 2, 2

	colL := m.width / 2
	colR := m.width - colL
	rowT := grid / 2
	rowB := grid - rowT

	lerp := func(a, b int) int {
		return a + int(float64(b-a)*p)
	}

	var normalX, normalY, normalW, normalH int
	switch m.focused {
	case panelMinikube:
		normalX, normalY, normalW, normalH = 0, 0, colL, rowT
	case panelSkaffold:
		normalX, normalY, normalW, normalH = colL, 0, colR, rowT
	case panelCommands:
		normalX, normalY, normalW, normalH = 0, rowT, colL, rowB
	default: // panelMFE
		normalX, normalY, normalW, normalH = colL, rowT, colR, rowB
	}

	x := lerp(normalX, 0)
	y := lerp(normalY, 0)
	w := lerp(normalW, m.width)
	h := lerp(normalH, grid)

	var panel string
	switch m.focused {
	case panelMinikube:
		m.panelVPs[PanelTopLeft].Width = max(1, w-border)
		m.panelVPs[PanelTopLeft].Height = max(1, h-border-titleH)
		panel = m.renderScrollPanel(panelMinikube, m.panelTitle(PanelTopLeft, true), m.panelVPs[PanelTopLeft].View(), w, h)
	case panelSkaffold:
		if m.isPortsTabActive(PanelTopRight) {
			m.portsVP.Width = max(1, w-border)
			m.portsVP.Height = max(1, h-border-titleH)
			m.portsVP.SetContent(m.renderPortsContent())
		} else if m.isDebugTabActive(PanelTopRight) {
			m.debugVP.Width = max(1, w-border)
			m.debugVP.Height = max(1, h-border-titleH)
			m.debugVP.SetContent(m.renderDebugContent())
		} else {
			m.panelVPs[PanelTopRight].Width = max(1, w-border)
			m.panelVPs[PanelTopRight].Height = max(1, h-border-titleH)
		}
		panel = m.renderScrollPanel(panelSkaffold, m.panelTitle(PanelTopRight, true), m.skaffoldPanelView(), w, h)
	case panelMFE:
		if m.isTestsTabActive(PanelBottomRight) {
			m.testVP.Width = max(1, w-border)
			m.testVP.Height = max(1, h-border-titleH)
			m.testVP.SetContent(wrapContent(m.testBuf, m.testVP.Width))
		} else {
			m.panelVPs[PanelBottomRight].Width = max(1, w-border)
			m.panelVPs[PanelBottomRight].Height = max(1, h-border-titleH)
		}
		panel = m.renderScrollPanel(panelMFE, m.panelTitle(PanelBottomRight, true), m.mfePanelView(), w, h)
	default: // panelCommands
		m.commandsVP.Width = max(1, w-border)
		m.commandsVP.Height = max(1, h-border-titleH-inputH)
		m.input.Width = w - border
		panel = m.renderCommandsPanel(w, h)
	}

	blank := strings.Repeat(" ", m.width)
	leftPad := strings.Repeat(" ", x)
	lines := strings.Split(panel, "\n")

	out := make([]string, 0, grid)
	for range y {
		out = append(out, blank)
	}
	for _, line := range lines {
		vw := x + lipgloss.Width(line)
		rightPad := ""
		if vw < m.width {
			rightPad = strings.Repeat(" ", m.width-vw)
		}
		out = append(out, leftPad+line+rightPad)
	}
	for len(out) < grid {
		out = append(out, blank)
	}
	return strings.Join(out, "\n")
}

func (m model) renderFullscreen() string {
	w := m.width
	grid := m.height - 1
	switch m.focused {
	case panelMinikube:
		return m.renderScrollPanel(panelMinikube, m.panelTitle(PanelTopLeft, true), m.panelVPs[PanelTopLeft].View(), w, grid)
	case panelSkaffold:
		return m.renderScrollPanel(panelSkaffold, m.panelTitle(PanelTopRight, true), m.skaffoldPanelView(), w, grid)
	case panelMFE:
		return m.renderScrollPanel(panelMFE, m.panelTitle(PanelBottomRight, true), m.mfePanelView(), w, grid)
	default: // panelCommands
		return m.renderCommandsPanel(w, grid)
	}
}

// panelTitle builds the title string for a content panel.
// With multiple steps it shows "Step1 / Step2" with the active one bolded,
// plus a cycling hint when the panel is focused.
func (m model) panelTitle(pid PanelID, focused bool) string {
	pv := m.panels[pid]
	dim := lipgloss.NewStyle().Foreground(currentTheme.Muted)

	hasPortsTab := pid == PanelTopRight && len(m.fwdPorts) > 0
	hasDebugTab := pid == PanelTopRight && len(m.debugPorts) > 0
	hasTestsTab := pid == PanelBottomRight && len(m.cfg.Tests) > 0
	totalTabs := len(pv.defs)
	if hasPortsTab {
		totalTabs++
	}
	if hasDebugTab {
		totalTabs++
	}
	if hasTestsTab {
		totalTabs++
	}

	if totalTabs == 0 {
		switch pid {
		case PanelTopLeft:
			return dim.Render(" Panel 1")
		case PanelTopRight:
			return dim.Render(" Panel 2")
		default:
			return dim.Render(" Panel 3")
		}
	}

	active := lipgloss.NewStyle().Foreground(currentTheme.Focused).Bold(true)
	sep := dim.Render(" / ")

	parts := make([]string, totalTabs)
	for i, def := range pv.defs {
		label := def.effectiveLabel()
		if i == pv.activeIdx {
			parts[i] = active.Render(label)
		} else {
			parts[i] = dim.Render(label)
		}
	}
	tabIdx := len(pv.defs)
	if hasPortsTab {
		if pv.activeIdx == tabIdx {
			parts[tabIdx] = active.Render("Ports")
		} else {
			parts[tabIdx] = dim.Render("Ports")
		}
		tabIdx++
	}
	if hasDebugTab {
		if pv.activeIdx == tabIdx {
			parts[tabIdx] = active.Render("Debug")
		} else {
			parts[tabIdx] = dim.Render("Debug")
		}
		tabIdx++
	}
	if hasTestsTab {
		testsLabel := "Tests"
		if m.testRunning {
			testsLabel = "Tests " + spinnerFrames[m.spinnerTick%len(spinnerFrames)]
		}
		if pv.activeIdx == tabIdx {
			parts[tabIdx] = active.Render(testsLabel)
		} else {
			parts[tabIdx] = dim.Render(testsLabel)
		}
	}
	title := " " + strings.Join(parts, sep)

	if focused && m.searchMode {
		title += dim.Render("  ·  ") + m.searchInput.View()
	} else {
		if focused && totalTabs > 1 {
			title += dim.Render("  ·  t to cycle")
		}
		if focused && m.isDebugTabActive(pid) {
			title += dim.Render("  ·  c to copy")
		}
	}
	if pid == PanelTopRight && m.flashMsg != "" {
		var s lipgloss.Style
		var icon string
		if m.flashOk {
			s = lipgloss.NewStyle().Foreground(currentTheme.Focused).Bold(true)
			icon = "✓ "
		} else {
			s = lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Bold(true)
			icon = "✗ "
		}
		title += "  " + s.Render(icon+m.flashMsg)
	}
	return title
}

func (m model) renderTopBar() string {
	var text string
	if m.cfg.StatusLine != nil {
		text = m.cfg.StatusLine(m.instanceName)
	} else if m.instanceName != "" {
		text = m.instanceName
	} else {
		text = "no instance running"
	}
	return topBarStyle().Width(m.width).Render(text)
}

func (m model) renderScrollPanel(panel int, title, content string, w, _ int) string {
	focused := m.focused == panel
	if focused {
		title += m.fullscreenHint()
	}
	div := separatorStyle().Render(strings.Repeat("─", w-2))
	inner := lipgloss.JoinVertical(lipgloss.Left,
		titleStyle(focused).Render(title),
		div,
		content,
	)
	return panelStyle(focused).
		Width(w - 2).
		Render(inner)
}

func (m model) renderCommandsPanel(w, h int) string {
	focused := m.focused == panelCommands
	hint := ""
	if focused {
		hint = m.fullscreenHint()
	}

	const border = 2
	const titleH = 2
	innerH := h - border - titleH

	p := m.flipProgress

	var titleText string
	var content string

	spinner := ""
	if m.runningCmds > 0 {
		spinner = " " + lipgloss.NewStyle().Foreground(currentTheme.Focused).Render(spinnerFrames[m.spinnerTick%len(spinnerFrames)])
	}

	switch {
	case p <= 0:
		titleText = " Commands" + spinner + hint
		content = m.commandsContent(w)

	case p >= 1:
		titleText, content = m.renderOverlay(w, innerH)
		titleText += spinner + hint

	case p < 0.5:
		multiplier := 1.0 - 2.0*p
		shrunkH := max(0, int(float64(innerH)*multiplier))
		titleText = " Commands" + spinner + hint
		if shrunkH < 2 {
			content = strings.Repeat("\n", innerH-1)
		} else {
			tmpVP := m.commandsVP
			tmpVP.Height = max(1, shrunkH-2)
			sep := separatorStyle().Render(strings.Repeat("─", w-2))
			partial := lipgloss.JoinVertical(lipgloss.Left, tmpVP.View(), sep, m.input.View())
			content = padToHeight(partial, tmpVP.Height+2, innerH)
		}

	default:
		multiplier := 2.0*p - 1.0
		expandH := max(1, int(float64(innerH)*multiplier))
		titleText, content = m.renderOverlayExpanding(w, innerH, expandH)
		titleText += spinner + hint
	}

	inner := lipgloss.JoinVertical(lipgloss.Left,
		titleStyle(focused).Render(titleText),
		separatorStyle().Render(strings.Repeat("─", w-2)),
		content,
	)
	return panelStyle(focused).
		Width(w - 2).
		Render(inner)
}

func (m model) commandsContent(w int) string {
	sep := separatorStyle().Render(strings.Repeat("─", w-2))
	return lipgloss.JoinVertical(lipgloss.Left,
		m.commandsVP.View(),
		sep,
		m.input.View(),
	)
}

func padToHeight(rendered string, current, target int) string {
	if current >= target {
		return rendered
	}
	return rendered + strings.Repeat("\n", target-current)
}

// ── Overlay dispatch ─────────────────────────────────────────────────────────

func (m model) renderOverlay(w, innerH int) (string, string) {
	switch m.overlay {
	case overlayHelp:
		return " Help", m.helpOverlayVP.View()
	case overlayWizard:
		raw := m.renderWizard()
		return m.wizardTitle(), padToHeight(raw, strings.Count(raw, "\n")+1, innerH)
	}
	return " Commands", ""
}

func (m model) renderOverlayExpanding(w, innerH, expandH int) (string, string) {
	switch m.overlay {
	case overlayHelp:
		tmpVP := m.helpOverlayVP
		tmpVP.Height = expandH
		return " Help", padToHeight(tmpVP.View(), expandH, innerH)
	case overlayWizard:
		raw := m.renderWizard()
		rawLines := strings.Split(raw, "\n")
		visible := min(expandH, len(rawLines))
		return m.wizardTitle(), padToHeight(strings.Join(rawLines[:visible], "\n"), visible, innerH)
	}
	return " Commands", ""
}

// ── Start wizard renderer ─────────────────────────────────────────────────────

type wizStyles struct {
	hl   lipgloss.Style
	sel  lipgloss.Style
	dim  lipgloss.Style
	hint lipgloss.Style
}

func currentWizStyles() wizStyles {
	return wizStyles{
		hl:   lipgloss.NewStyle().Background(currentTheme.Focused).Foreground(currentTheme.HighlightText).Bold(true),
		sel:  lipgloss.NewStyle().Foreground(currentTheme.Focused).Bold(true),
		dim:  lipgloss.NewStyle().Foreground(currentTheme.Muted),
		hint: lipgloss.NewStyle().Foreground(currentTheme.Help),
	}
}

func wizLabel(text string, activeField, thisField, labelW int) string {
	s := lipgloss.NewStyle().Width(labelW)
	if activeField == thisField {
		return s.Background(currentTheme.Focused).Foreground(currentTheme.HighlightText).Bold(true).Render(text)
	}
	return s.Foreground(currentTheme.Title).Render(text)
}

func wizardButtons(focused bool, idx int, hl lipgloss.Style) string {
	btn := lipgloss.NewStyle().Padding(0, 2)
	var startS, cancelS lipgloss.Style
	if focused {
		if idx == 0 {
			startS, cancelS = hl.Padding(0, 2), btn.Foreground(currentTheme.Muted)
		} else {
			startS, cancelS = btn.Foreground(currentTheme.Muted), hl.Padding(0, 2)
		}
	} else {
		startS = btn.Foreground(currentTheme.Title)
		cancelS = btn.Foreground(currentTheme.Muted)
	}
	return "  " + startS.Render("Start") + "  " + cancelS.Render("Cancel")
}

func (m model) wizardTitle() string {
	return " Start"
}

func (m model) renderWizard() string {
	if m.wizard == nil {
		return ""
	}
	return m.renderWizardCustom()
}

func renderSelectField(i int, s *fieldState, ws wizStyles, activeField, labelW int) []string {
	focused := activeField == i
	return []string{
		"  " + wizLabel(s.spec.Label, activeField, i, labelW) + "  " +
			horizSelector(s.selectIdx, s.spec.Options, focused, ws.hl, ws.sel, ws.dim),
	}
}

func renderTextField(i int, s *fieldState, ws wizStyles, activeField, labelW int) []string {
	return []string{
		"  " + wizLabel(s.spec.Label, activeField, i, labelW) + "  " + s.pickerSearch.View(),
	}
}

func renderSystemSelectField(i int, s *fieldState, ws wizStyles, activeField, labelW int) []string {
	var lines []string
	focused := activeField == i
	if s.pickerOpen {
		lines = append(lines, "  "+ws.hl.Render(" "+s.spec.Label+" "))
		lines = append(lines, "  "+s.pickerSearch.View())
		const maxVisible = 8
		start := 0
		if s.pickerIdx >= maxVisible {
			start = s.pickerIdx - maxVisible + 1
		}
		end := min(len(s.sysPickerItems), start+maxVisible)
		if len(s.sysPickerItems) == 0 {
			lines = append(lines, "  "+ws.dim.Render("  (no matches)"))
		} else {
			for j := start; j < end; j++ {
				item := s.sysPickerItems[j]
				isFocused := j == s.pickerIdx
				if item.isSystem {
					total, selected := 0, 0
					for _, pi := range s.sysPickerItems {
						if !pi.isSystem && pi.system == item.system {
							total++
							if s.isMultiSelected(pi.comp) {
								selected++
							}
						}
					}
					icon := "○"
					if total > 0 && selected == total {
						icon = "✓"
					} else if selected > 0 {
						icon = "◐"
					}
					text := fmt.Sprintf("%s %s  [%d/%d]", icon, item.system, selected, total)
					if isFocused {
						lines = append(lines, "  "+ws.hl.Render(text))
					} else if selected > 0 {
						lines = append(lines, "  "+ws.sel.Render(text))
					} else {
						lines = append(lines, "  "+ws.dim.Render(text))
					}
				} else {
					isSelected := s.isMultiSelected(item.comp)
					check := "○"
					if isSelected {
						check = "✓"
					}
					text := "  " + check + " " + item.comp
					switch {
					case isFocused:
						lines = append(lines, "  "+ws.hl.Render(text))
					case isSelected:
						lines = append(lines, "  "+ws.sel.Render(text))
					default:
						lines = append(lines, "  "+ws.dim.Render(text))
					}
				}
			}
		}
	} else if focused {
		for j, comp := range s.multiValues {
			var rowPrefix string
			if j == 0 {
				rowPrefix = "  " + wizLabel(s.spec.Label, activeField, i, labelW) + "  "
			} else {
				rowPrefix = "  " + strings.Repeat(" ", labelW) + "  "
			}
			if j == s.collapsedIdx {
				lines = append(lines, rowPrefix+ws.hl.Render(" ✓ "+comp+" "))
			} else {
				lines = append(lines, rowPrefix+ws.sel.Render("✓ "+comp))
			}
		}
		isAddFocused := s.collapsedIdx == len(s.multiValues)
		var addBtn string
		if isAddFocused {
			addBtn = ws.hl.Render(" + Add ")
		} else {
			addBtn = ws.dim.Render("[ + Add ]")
		}
		if len(s.multiValues) == 0 {
			lines = append(lines, "  "+wizLabel(s.spec.Label, activeField, i, labelW)+"  "+addBtn)
		} else {
			lines = append(lines, "  "+strings.Repeat(" ", labelW+4)+addBtn)
		}
	} else {
		var summary string
		if len(s.multiValues) == 0 {
			summary = ws.dim.Render("(none)")
		} else {
			summary = ws.sel.Render(strings.Join(s.multiValues, ", "))
		}
		lines = append(lines, "  "+wizLabel(s.spec.Label, activeField, i, labelW)+"  "+summary)
		lines = append(lines, "  "+strings.Repeat(" ", labelW+4)+ws.dim.Render("[ + Add ]"))
	}
	return lines
}

func renderSingleSelectField(i int, s *fieldState, ws wizStyles, activeField, labelW int) []string {
	var lines []string
	focused := activeField == i
	if s.pickerOpen {
		lines = append(lines, "  "+ws.hl.Render(" "+s.spec.Label+" "))
		lines = append(lines, "  "+s.pickerSearch.View())
		const maxVisible = 6
		start := 0
		if s.pickerIdx >= maxVisible {
			start = s.pickerIdx - maxVisible + 1
		}
		end := min(len(s.strPickerItems), start+maxVisible)
		if len(s.strPickerItems) == 0 {
			lines = append(lines, "  "+ws.dim.Render("  (no matches)"))
		} else {
			for j := start; j < end; j++ {
				opt := s.strPickerItems[j]
				isFocused := j == s.pickerIdx
				isSelected := opt == s.singleValue
				check := "○"
				if isSelected {
					check = "●"
				}
				text := check + " " + opt
				switch {
				case isFocused:
					lines = append(lines, "  "+ws.hl.Render(text))
				case isSelected:
					lines = append(lines, "  "+ws.sel.Render(text))
				default:
					lines = append(lines, "  "+ws.dim.Render(text))
				}
			}
		}
	} else {
		var display string
		if s.singleValue == "" {
			display = ws.dim.Render("(none)")
		} else if focused {
			display = ws.sel.Render(s.singleValue)
		} else {
			display = ws.dim.Render(s.singleValue)
		}
		lines = append(lines, "  "+wizLabel(s.spec.Label, activeField, i, labelW)+"  "+display)
		if focused && len(s.spec.Options) > 0 {
			lines = append(lines, "  "+strings.Repeat(" ", labelW+4)+ws.dim.Render("[ Enter to select ]"))
		}
	}
	return lines
}

func renderMultiSelectField(i int, s *fieldState, ws wizStyles, activeField, labelW int) []string {
	var lines []string
	focused := activeField == i
	if s.pickerOpen {
		lines = append(lines, "  "+ws.hl.Render(" "+s.spec.Label+" "))
		lines = append(lines, "  "+s.pickerSearch.View())
		const maxVisible = 6
		start := 0
		if s.pickerIdx >= maxVisible {
			start = s.pickerIdx - maxVisible + 1
		}
		end := min(len(s.strPickerItems), start+maxVisible)
		if len(s.strPickerItems) == 0 {
			lines = append(lines, "  "+ws.dim.Render("  (no matches)"))
		} else {
			for j := start; j < end; j++ {
				opt := s.strPickerItems[j]
				isFocused := j == s.pickerIdx
				isSelected := s.isMultiSelected(opt)
				check := "○"
				if isSelected {
					check = "✓"
				}
				text := check + " " + opt
				switch {
				case isFocused:
					lines = append(lines, "  "+ws.hl.Render(text))
				case isSelected:
					lines = append(lines, "  "+ws.sel.Render(text))
				default:
					lines = append(lines, "  "+ws.dim.Render(text))
				}
			}
		}
	} else if focused {
		for j, v := range s.multiValues {
			var rowPrefix string
			if j == 0 {
				rowPrefix = "  " + wizLabel(s.spec.Label, activeField, i, labelW) + "  "
			} else {
				rowPrefix = "  " + strings.Repeat(" ", labelW) + "  "
			}
			if j == s.collapsedIdx {
				lines = append(lines, rowPrefix+ws.hl.Render(" ✓ "+v+" "))
			} else {
				lines = append(lines, rowPrefix+ws.sel.Render("✓ "+v))
			}
		}
		isAddFocused := s.collapsedIdx == len(s.multiValues)
		var addBtn string
		if isAddFocused {
			addBtn = ws.hl.Render(" + Add ")
		} else {
			addBtn = ws.dim.Render("[ + Add ]")
		}
		if len(s.multiValues) == 0 {
			lines = append(lines, "  "+wizLabel(s.spec.Label, activeField, i, labelW)+"  "+addBtn)
		} else {
			lines = append(lines, "  "+strings.Repeat(" ", labelW+4)+addBtn)
		}
	} else {
		var summary string
		if len(s.multiValues) == 0 {
			summary = ws.dim.Render("(none)")
		} else {
			summary = ws.sel.Render(strings.Join(s.multiValues, ", "))
		}
		lines = append(lines, "  "+wizLabel(s.spec.Label, activeField, i, labelW)+"  "+summary)
		lines = append(lines, "  "+strings.Repeat(" ", labelW+4)+ws.dim.Render("[ + Add ]"))
	}
	return lines
}

func (m model) renderWizardCustom() string {
	wiz := m.wizard
	ws := currentWizStyles()
	numFields := len(wiz.states)

	// Compute label width from the longest label (minimum 8).
	labelW := 8
	for _, s := range wiz.states {
		if n := len([]rune(s.spec.Label)); n > labelW {
			labelW = n
		}
	}

	var lines []string
	lines = append(lines, "") // leading blank

	for i := range wiz.states {
		s := &wiz.states[i]

		// Blank line before field, except between consecutive Select fields.
		if i > 0 {
			prev := &wiz.states[i-1]
			if !(s.spec.Kind == FieldKindSelect && prev.spec.Kind == FieldKindSelect) {
				lines = append(lines, "")
			}
		}

		switch s.spec.Kind {
		case FieldKindSelect:
			lines = append(lines, renderSelectField(i, s, ws, wiz.fieldIdx, labelW)...)
		case FieldKindSystemSelect:
			lines = append(lines, renderSystemSelectField(i, s, ws, wiz.fieldIdx, labelW)...)
		case FieldKindSingleSelect:
			lines = append(lines, renderSingleSelectField(i, s, ws, wiz.fieldIdx, labelW)...)
		case FieldKindText:
			lines = append(lines, renderTextField(i, s, ws, wiz.fieldIdx, labelW)...)
		case FieldKindMultiSelect:
			lines = append(lines, renderMultiSelectField(i, s, ws, wiz.fieldIdx, labelW)...)
		}
	}

	lines = append(lines, "")
	lines = append(lines, "")
	lines = append(lines, wizardButtons(wiz.fieldIdx == numFields, wiz.confirmIdx, ws.hl))
	lines = append(lines, "")

	// Hint text based on the current field kind.
	var hintText string
	if wiz.fieldIdx < numFields {
		s := &wiz.states[wiz.fieldIdx]
		switch {
		case s.pickerOpen && s.spec.Kind == FieldKindSystemSelect:
			hintText = "  ↑↓ navigate  ·  Enter toggle  ·  type to search  ·  Tab done"
		case s.pickerOpen && (s.spec.Kind == FieldKindSingleSelect || s.spec.Kind == FieldKindMultiSelect):
			hintText = "  ↑↓ navigate  ·  Enter select  ·  type to search  ·  Tab done"
		case s.spec.Kind == FieldKindSelect:
			hintText = "  ←→ select  ·  ↑↓ or Tab to move  ·  Esc cancel"
		case s.spec.Kind == FieldKindSystemSelect:
			hintText = "  ↑↓ navigate  ·  x remove  ·  Enter add  ·  Tab next field"
		case s.spec.Kind == FieldKindSingleSelect:
			if len(s.spec.Options) > 0 {
				hintText = "  Enter to pick  ·  x clear  ·  ↑↓ or Tab to move  ·  Esc cancel"
			} else {
				hintText = "  ↑↓ or Tab to move  ·  Esc cancel"
			}
		case s.spec.Kind == FieldKindMultiSelect:
			hintText = "  ↑↓ navigate  ·  x remove  ·  Enter add  ·  Tab next field"
		case s.spec.Kind == FieldKindText:
			hintText = "  type to edit  ·  ↑↓ or Tab to move  ·  Esc cancel"
		}
	} else {
		hintText = "  ←→ select  ·  Enter confirm  ·  Esc cancel"
	}
	lines = append(lines, ws.hint.Render(hintText))
	return strings.Join(lines, "\n")
}

func horizSelector(idx int, opts []string, focused bool, hlStyle, selStyle, dimStyle lipgloss.Style) string {
	parts := make([]string, len(opts))
	for i, opt := range opts {
		if i == idx {
			if focused {
				parts[i] = hlStyle.Render("● " + opt)
			} else {
				parts[i] = selStyle.Render("● " + opt)
			}
		} else {
			parts[i] = dimStyle.Render("○ " + opt)
		}
	}
	return strings.Join(parts, "  ")
}

// helpContent builds the help text, arranging sections into columns when the
// available width allows it.
func helpContent(width int) string {
	nav := helpSection("Navigation", []helpEntry{
		{"Tab / Shift+Tab", "cycle panels"},
		{"↑ / k", "scroll up"},
		{"↓ / j", "scroll down"},
		{"PgUp / b", "page up"},
		{"PgDn / f", "page down"},
		{"g / G", "top / bottom"},
		{"t", "cycle step views in panel"},
		{"/ ", "search panel logs"},
		{"Ctrl+F", "fullscreen toggle"},
	})
	cmds := helpSection("Commands", []helpEntry{
		{"help", "show this help"},
		{"start", "start the instance"},
		{"stop", "stop instance + delete cluster"},
		{"logs", "show log file paths"},
		{"test [label]", "run a test suite"},
		{"theme [name]", "set color theme"},
		{"", ""},
		{"Enter", "run command"},
		{"Esc", "close overlay"},
	})
	global := helpSection("Global", []helpEntry{
		{"Ctrl+C", "quit"},
	})

	hs := helpTextStyle()

	if width >= 96 {
		cw := width / 3
		return lipgloss.JoinHorizontal(lipgloss.Top,
			hs.Width(cw).Render(nav),
			hs.Width(cw).Render(cmds),
			hs.Width(width-2*cw).Render(global),
		)
	}
	if width >= 64 {
		cw := width / 2
		right := lipgloss.JoinVertical(lipgloss.Left, cmds, "", global)
		return lipgloss.JoinHorizontal(lipgloss.Top,
			hs.Width(cw).Render(nav),
			hs.Width(width-cw).Render(right),
		)
	}
	return hs.Render(lipgloss.JoinVertical(lipgloss.Left, nav, "", cmds, "", global))
}

type helpEntry struct{ key, desc string }

func helpSection(title string, entries []helpEntry) string {
	titleLine := "  " + title

	var body []string
	for _, e := range entries {
		if e.key == "" {
			body = append(body, "")
		} else {
			body = append(body, fmt.Sprintf("  %-16s%s", e.key, e.desc))
		}
	}

	maxW := len([]rune(titleLine))
	for _, l := range body {
		if w := len([]rune(l)); w > maxW {
			maxW = w
		}
	}
	divLine := "  " + strings.Repeat("─", maxW-2)

	lines := append([]string{titleLine, divLine}, body...)
	return strings.Join(lines, "\n")
}
