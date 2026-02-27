package main

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
	grid := m.height - 1 // 1 row reserved for the top bar
	rowT := grid / 2
	rowB := grid - rowT

	topRow := lipgloss.JoinHorizontal(lipgloss.Top,
		m.renderScrollPanel(panelMinikube, " Minikube / kubectl", m.minikubeVP.View(), colL, rowT),
		m.renderScrollPanel(panelSkaffold, " Skaffold", m.skaffoldVP.View(), colR, rowT),
	)
	bottomRow := lipgloss.JoinHorizontal(lipgloss.Top,
		m.renderCommandsPanel(colL, rowB),
		m.renderScrollPanel(panelMFE, " MFE", m.mfeVP.View(), colR, rowB),
	)

	return lipgloss.JoinVertical(lipgloss.Left, bar, topRow, bottomRow)
}

// fullscreenHint returns a dim contextual hint for the Ctrl+F binding.
// Uses fullscreenTarget so the text flips at the moment the key is pressed,
// not only once the animation completes.
func (m model) fullscreenHint() string {
	var text string
	if m.fullscreenTarget == 1 {
		text = "  ctrl+f to exit fullscreen"
	} else {
		text = "  ctrl+f to fullscreen"
	}
	return lipgloss.NewStyle().Foreground(currentTheme.Muted).Render(text)
}

// renderFullscreenTransition renders the focused panel at an interpolated size
// and position between its grid slot and the full terminal.
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

	// Each panel's top-left corner in the grid, plus its normal dimensions.
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

	// Temporarily resize the focused viewport (safe: m is a value copy in View).
	var panel string
	switch m.focused {
	case panelMinikube:
		m.minikubeVP.Width = max(1, w-border)
		m.minikubeVP.Height = max(1, h-border-titleH)
		panel = m.renderScrollPanel(panelMinikube, " Minikube / kubectl", m.minikubeVP.View(), w, h)
	case panelSkaffold:
		m.skaffoldVP.Width = max(1, w-border)
		m.skaffoldVP.Height = max(1, h-border-titleH)
		panel = m.renderScrollPanel(panelSkaffold, " Skaffold", m.skaffoldVP.View(), w, h)
	case panelMFE:
		m.mfeVP.Width = max(1, w-border)
		m.mfeVP.Height = max(1, h-border-titleH)
		panel = m.renderScrollPanel(panelMFE, " MFE", m.mfeVP.View(), w, h)
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
	// Top padding rows above the panel.
	for range y {
		out = append(out, blank)
	}
	// Panel rows: left-pad then right-pad to terminal width.
	for _, line := range lines {
		vw := x + lipgloss.Width(line)
		rightPad := ""
		if vw < m.width {
			rightPad = strings.Repeat(" ", m.width-vw)
		}
		out = append(out, leftPad+line+rightPad)
	}
	// Bottom padding rows.
	for len(out) < grid {
		out = append(out, blank)
	}
	return strings.Join(out, "\n")
}

// renderFullscreen renders only the focused panel at full terminal width/height.
// The hint is appended automatically by the panel renderers since the panel is focused.
func (m model) renderFullscreen() string {
	w := m.width
	grid := m.height - 1
	switch m.focused {
	case panelMinikube:
		return m.renderScrollPanel(panelMinikube, " Minikube / kubectl", m.minikubeVP.View(), w, grid)
	case panelSkaffold:
		return m.renderScrollPanel(panelSkaffold, " Skaffold", m.skaffoldVP.View(), w, grid)
	case panelMFE:
		return m.renderScrollPanel(panelMFE, " MFE", m.mfeVP.View(), w, grid)
	default: // panelCommands
		return m.renderCommandsPanel(w, grid)
	}
}

func (m model) renderTopBar() string {
	return topBarStyle().Width(m.width).Render(m.instance.StatusLine())
}

// renderScrollPanel renders a titled panel containing pre-rendered content.
// w is the outer panel width. Height is not set — the viewport inside always
// produces exactly the right number of lines, so panel height = content + borders.
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

// renderCommandsPanel renders the bottom-left panel, which can either show the
// command input/output or the help overlay — animated with a card-flip effect.
func (m model) renderCommandsPanel(w, h int) string {
	focused := m.focused == panelCommands
	hint := ""
	if focused {
		hint = m.fullscreenHint()
	}

	// Inner height below the title: panel outer minus border(2) minus title(2).
	const border = 2
	const titleH = 2
	innerH := h - border - titleH

	p := m.flipProgress

	var titleText string
	var content string

	switch {
	case p <= 0:
		// Commands fully visible.
		titleText = " Commands" + hint
		content = m.commandsContent(w)

	case p >= 1:
		// Overlay fully visible.
		titleText, content = m.renderOverlay(w, innerH)
		titleText += hint

	case p < 0.5:
		// Phase 1: shrink the commands side toward the midpoint.
		multiplier := 1.0 - 2.0*p
		shrunkH := max(0, int(float64(innerH)*multiplier))
		titleText = " Commands" + hint
		if shrunkH < 2 {
			// Too few lines to show sep+input; just blank.
			content = strings.Repeat("\n", innerH-1)
		} else {
			// Temporarily narrow the viewport height so it renders fewer lines.
			tmpVP := m.commandsVP
			tmpVP.Height = max(1, shrunkH-2) // -2 for sep + input
			sep := separatorStyle().Render(strings.Repeat("─", w-2))
			partial := lipgloss.JoinVertical(lipgloss.Left, tmpVP.View(), sep, m.input.View())
			content = padToHeight(partial, tmpVP.Height+2, innerH)
		}

	default:
		// Phase 2 (p >= 0.5): expand the overlay from the midpoint.
		multiplier := 2.0*p - 1.0
		expandH := max(1, int(float64(innerH)*multiplier))
		titleText, content = m.renderOverlayExpanding(w, innerH, expandH)
		titleText += hint
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

// commandsContent returns the full commands panel body: viewport + separator + input.
func (m model) commandsContent(w int) string {
	sep := separatorStyle().Render(strings.Repeat("─", w-2))
	return lipgloss.JoinVertical(lipgloss.Left,
		m.commandsVP.View(),
		sep,
		m.input.View(),
	)
}

// padToHeight pads a rendered block (with `current` lines) up to `target` lines
// by appending blank lines. This keeps the panel frame the same height during
// the card-flip animation.
func padToHeight(rendered string, current, target int) string {
	if current >= target {
		return rendered
	}
	return rendered + strings.Repeat("\n", target-current)
}

// ── Overlay dispatch ─────────────────────────────────────────────────────────

// renderOverlay returns the (title, content) for the fully-visible overlay.
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

// renderOverlayExpanding returns (title, content) for the expanding animation phase.
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

func (m model) wizardTitle() string {
	return " Start"
}

// renderWizard returns the wizard form as a plain string (not padded to innerH).
// Layout: two horizontal selectors (CPU, RAM) + a buttons row, navigated with ↑↓/←→.
func (m model) renderWizard() string {
	wiz := m.wizard
	if wiz == nil {
		return ""
	}

	// hl = background-highlighted style for the focused label and selected option.
	hl   := lipgloss.NewStyle().Background(currentTheme.Focused).Foreground(currentTheme.HighlightText).Bold(true)
	sel  := lipgloss.NewStyle().Foreground(currentTheme.Focused).Bold(true)
	dim  := lipgloss.NewStyle().Foreground(currentTheme.Muted)
	info := lipgloss.NewStyle().Foreground(currentTheme.Title)
	hint := lipgloss.NewStyle().Foreground(currentTheme.Help)

	labelStyle := func(field int) lipgloss.Style {
		if wiz.field == field {
			return hl.Padding(0, 1)
		}
		return info
	}

	var lines []string
	lines = append(lines, "")
	lines = append(lines, "  "+labelStyle(0).Render("CPU")+"  "+horizSelector(wiz.cpuIdx, cpuOptions, wiz.field == 0, hl, sel, dim))
	lines = append(lines, "")
	lines = append(lines, "  "+labelStyle(1).Render("RAM")+"  "+horizSelector(wiz.ramIdx, ramOptions, wiz.field == 1, hl, sel, dim))
	lines = append(lines, "")
	lines = append(lines, "")

	// Buttons row.
	var startS, cancelS lipgloss.Style
	if wiz.field == 2 {
		if wiz.confirmIdx == 0 {
			startS, cancelS = hl.Padding(0, 1), dim
		} else {
			startS, cancelS = dim, hl.Padding(0, 1)
		}
	} else {
		startS, cancelS = info, dim
	}
	lines = append(lines, "  "+startS.Render("Start")+"   "+cancelS.Render("Cancel"))
	lines = append(lines, "")

	hintText := "  ↑↓ fields  ·  ←→ select"
	if wiz.field == 2 {
		hintText += "  ·  Enter confirm"
	}
	hintText += "  ·  Esc cancel"
	lines = append(lines, hint.Render(hintText))

	return strings.Join(lines, "\n")
}

// horizSelector renders a row of options.
// When focused, the selected option gets a background highlight (hlStyle);
// when not focused it gets the plain accent foreground (selStyle).
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
//
//	width < 64  → 1 column (stacked)
//	width >= 64 → 2 columns: Navigation | Commands + Global
//	width >= 96 → 3 columns: Navigation | Commands | Global
func helpContent(width int) string {
	nav := helpSection("Navigation", []helpEntry{
		{"Tab / Shift+Tab", "cycle panels"},
		{"↑ / k", "scroll up"},
		{"↓ / j", "scroll down"},
		{"PgUp / b", "page up"},
		{"PgDn / f", "page down"},
		{"g / G", "top / bottom"},
		{"Ctrl+F", "fullscreen toggle"},
	})
	cmds := helpSection("Commands", []helpEntry{
		{"help", "show this help"},
		{"start", "configure and start instance"},
		{"theme [name]", "set color theme"},
		{"", ""},
		{"Enter", "run command"},
		{"Esc", "close help"},
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

// helpSection renders one titled group of keybindings as a plain string.
// The divider under the title is sized to match the longest line in the section.
// fmt.Sprintf's %-Ns pads by rune count, so single-width Unicode keys (↑ ↓) align correctly.
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

	// Find the longest line by rune count (display width).
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
