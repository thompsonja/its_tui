package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

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
		m.renderScrollPanel(panelSkaffold, m.panelTitle(PanelTopRight, m.focused == panelSkaffold), m.panelVPs[PanelTopRight].View(), colR, rowT),
	)
	bottomRow := lipgloss.JoinHorizontal(lipgloss.Top,
		m.renderCommandsPanel(colL, rowB),
		m.renderScrollPanel(panelMFE, m.panelTitle(PanelBottomRight, m.focused == panelMFE), m.panelVPs[PanelBottomRight].View(), colR, rowB),
	)

	return lipgloss.JoinVertical(lipgloss.Left, bar, topRow, bottomRow)
}

func (m model) fullscreenHint() string {
	if m.instance.Name == "" {
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
		m.panelVPs[PanelTopRight].Width = max(1, w-border)
		m.panelVPs[PanelTopRight].Height = max(1, h-border-titleH)
		panel = m.renderScrollPanel(panelSkaffold, m.panelTitle(PanelTopRight, true), m.panelVPs[PanelTopRight].View(), w, h)
	case panelMFE:
		m.panelVPs[PanelBottomRight].Width = max(1, w-border)
		m.panelVPs[PanelBottomRight].Height = max(1, h-border-titleH)
		panel = m.renderScrollPanel(panelMFE, m.panelTitle(PanelBottomRight, true), m.panelVPs[PanelBottomRight].View(), w, h)
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
		return m.renderScrollPanel(panelSkaffold, m.panelTitle(PanelTopRight, true), m.panelVPs[PanelTopRight].View(), w, grid)
	case panelMFE:
		return m.renderScrollPanel(panelMFE, m.panelTitle(PanelBottomRight, true), m.panelVPs[PanelBottomRight].View(), w, grid)
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

	if len(pv.defs) == 0 {
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

	parts := make([]string, len(pv.defs))
	for i, def := range pv.defs {
		label := def.effectiveLabel()
		if i == pv.activeIdx {
			parts[i] = active.Render(label)
		} else {
			parts[i] = dim.Render(label)
		}
	}
	title := " " + strings.Join(parts, sep)

	if focused && len(pv.defs) > 1 {
		title += dim.Render("  ·  t to cycle")
	}
	return title
}

func (m model) renderTopBar() string {
	return topBarStyle().Width(m.width).Render(m.instance.StatusLine())
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
		frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
		spinner = " " + lipgloss.NewStyle().Foreground(currentTheme.Focused).Render(frames[m.spinnerTick%len(frames)])
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

func (m model) renderWizardCustom() string {
	wiz := m.wizard
	ws := currentWizStyles()

	var lines []string
	lines = append(lines, "")
	lines = append(lines, "  "+wizLabel("CPU", wiz.field, wizFieldCPU, 12)+"  "+horizSelector(wiz.cpuIdx, cpuOptions, wiz.field == wizFieldCPU, ws.hl, ws.sel, ws.dim))
	lines = append(lines, "  "+wizLabel("RAM", wiz.field, wizFieldRAM, 12)+"  "+horizSelector(wiz.ramIdx, ramOptions, wiz.field == wizFieldRAM, ws.hl, ws.sel, ws.dim))
	lines = append(lines, "")

	// ── Components field ──────────────────────────────────────────────────────
	compFocused := wiz.field == wizFieldComponents
	if wiz.compPickerOpen {
		lines = append(lines, "  "+ws.hl.Render(" Components "))
		lines = append(lines, "  "+wiz.compPickerSearch.View())
		const maxVisible = 8
		start := 0
		if wiz.compPickerIdx >= maxVisible {
			start = wiz.compPickerIdx - maxVisible + 1
		}
		end := min(len(wiz.compPickerItems), start+maxVisible)
		if len(wiz.compPickerItems) == 0 {
			lines = append(lines, "  "+ws.dim.Render("  (no matches)"))
		} else {
			for i := start; i < end; i++ {
				item := wiz.compPickerItems[i]
				isFocused := i == wiz.compPickerIdx
				if item.isSystem {
					total, selected := 0, 0
					for _, pi := range wiz.compPickerItems {
						if !pi.isSystem && pi.system == item.system {
							total++
							if wiz.isCompSelected(pi.comp) {
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
					isSelected := wiz.isCompSelected(item.comp)
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
	} else if compFocused {
		for i, comp := range wiz.selectedComps {
			var rowPrefix string
			if i == 0 {
				rowPrefix = "  " + wizLabel("Components", wiz.field, wizFieldComponents, 12) + "  "
			} else {
				rowPrefix = "  " + strings.Repeat(" ", 12) + "  "
			}
			if i == wiz.selectedIdx {
				lines = append(lines, rowPrefix+ws.hl.Render(" ✓ "+comp+" "))
			} else {
				lines = append(lines, rowPrefix+ws.sel.Render("✓ "+comp))
			}
		}
		isAddFocused := wiz.selectedIdx == len(wiz.selectedComps)
		var addBtn string
		if isAddFocused {
			addBtn = ws.hl.Render(" + Add ")
		} else {
			addBtn = ws.dim.Render("[ + Add ]")
		}
		if len(wiz.selectedComps) == 0 {
			lines = append(lines, "  "+wizLabel("Components", wiz.field, wizFieldComponents, 12)+"  "+addBtn)
		} else {
			lines = append(lines, "  "+strings.Repeat(" ", 16)+addBtn)
		}
	} else {
		var summary string
		if len(wiz.selectedComps) == 0 {
			summary = ws.dim.Render("(none)")
		} else {
			summary = ws.sel.Render(strings.Join(wiz.selectedComps, ", "))
		}
		lines = append(lines, "  "+wizLabel("Components", wiz.field, wizFieldComponents, 12)+"  "+summary)
		lines = append(lines, "  "+strings.Repeat(" ", 16)+ws.dim.Render("[ + Add ]"))
	}
	lines = append(lines, "")

	// ── MFE field ─────────────────────────────────────────────────────────────
	mfeFocused := wiz.field == wizFieldMFE
	if wiz.mfePickerOpen {
		lines = append(lines, "  "+ws.hl.Render(" MFE "))
		lines = append(lines, "  "+wiz.mfePickerSearch.View())
		const maxVisible = 6
		start := 0
		if wiz.mfePickerIdx >= maxVisible {
			start = wiz.mfePickerIdx - maxVisible + 1
		}
		end := min(len(wiz.mfePickerItems), start+maxVisible)
		if len(wiz.mfePickerItems) == 0 {
			lines = append(lines, "  "+ws.dim.Render("  (no matches)"))
		} else {
			for i := start; i < end; i++ {
				mfe := wiz.mfePickerItems[i]
				isFocused := i == wiz.mfePickerIdx
				isSelected := mfe == wiz.selectedMFE
				check := "○"
				if isSelected {
					check = "●"
				}
				text := check + " " + mfe
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
		var mfeDisplay string
		if wiz.selectedMFE == "" {
			mfeDisplay = ws.dim.Render("(none)")
		} else if mfeFocused {
			mfeDisplay = ws.sel.Render(wiz.selectedMFE)
		} else {
			mfeDisplay = ws.dim.Render(wiz.selectedMFE)
		}
		lines = append(lines, "  "+wizLabel("MFE", wiz.field, wizFieldMFE, 12)+"  "+mfeDisplay)
		if mfeFocused && len(wiz.mfeAll) > 0 {
			lines = append(lines, "  "+strings.Repeat(" ", 16)+ws.dim.Render("[ Enter to select ]"))
		}
	}
	lines = append(lines, "")

	lines = append(lines, "  "+wizLabel("Mode", wiz.field, wizFieldMode, 12)+"  "+horizSelector(wiz.modeIdx, skaffoldModes, wiz.field == wizFieldMode, ws.hl, ws.sel, ws.dim))
	lines = append(lines, "")
	lines = append(lines, "")
	lines = append(lines, wizardButtons(wiz.field == wizFieldButtons, wiz.confirmIdx, ws.hl))
	lines = append(lines, "")

	var hintText string
	switch {
	case wiz.compPickerOpen:
		hintText = "  ↑↓ navigate  ·  Enter toggle  ·  type to search  ·  Tab done"
	case wiz.mfePickerOpen:
		hintText = "  ↑↓ navigate  ·  Enter select  ·  type to search  ·  Tab done"
	case wiz.field == wizFieldCPU || wiz.field == wizFieldRAM:
		hintText = "  ←→ select  ·  ↑↓ or Tab to move  ·  Esc cancel"
	case wiz.field == wizFieldComponents:
		hintText = "  ↑↓ navigate  ·  x remove  ·  Enter add  ·  Tab next field"
	case wiz.field == wizFieldMFE:
		if len(wiz.mfeAll) > 0 {
			hintText = "  Enter to pick  ·  x clear  ·  ↑↓ or Tab to move  ·  Esc cancel"
		} else {
			hintText = "  ↑↓ or Tab to move  ·  Esc cancel"
		}
	case wiz.field == wizFieldMode:
		hintText = "  ←→ select mode  ·  ↑↓ or Tab to move  ·  Esc cancel"
	case wiz.field == wizFieldButtons:
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
		{"Ctrl+F", "fullscreen toggle"},
	})
	cmds := helpSection("Commands", []helpEntry{
		{"help", "show this help"},
		{"start", "start the instance"},
		{"stop", "stop instance + delete cluster"},
		{"logs", "show log file paths"},
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
