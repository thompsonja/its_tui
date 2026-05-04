package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/lipgloss"
)

// animation step constants.
const (
	flipStep       = 0.10
	fullscreenStep = 0.12
)

// viewState holds all ephemeral display state: viewports, animations, overlays,
// and UI elements that are derived from appState on demand.
type viewState struct {
	width, height int
	ready         bool
	focused       int

	panelVPs      [3]viewport.Model
	commandsVP    viewport.Model
	helpOverlayVP viewport.Model
	portsVP       viewport.Model
	debugVP       viewport.Model
	testVP        viewport.Model

	input       textinput.Model
	searchInput textinput.Model
	searchMode  bool
	searchQuery string

	// card-flip animation: 0.0 = commands panel, 1.0 = overlay.
	flipProgress float64
	flipTarget   float64

	// fullscreen animation: 0.0 = 2×2 grid, 1.0 = focused panel fills screen.
	fullscreenProgress float64
	fullscreenTarget   float64

	runningCmds int
	spinnerTick int
	steps       map[string]*commandStep

	flashMsg   string
	flashOk    bool
	flashUntil int
}

// advanceAnim advances progress toward target by step. Returns new progress
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

// resize recalculates all viewport dimensions from terminal size and animation
// state, then syncs viewport content from app.
func (vs *viewState) resize(app appState) {
	const border = 2
	const title = 2
	const input = 2

	grid := vs.height - 1

	var (
		vpW_L, vpW_R                                int
		vpH_top, vpH_commands, vpH_mfe, vpH_overlay int
	)

	if vs.fullscreenProgress >= 1 {
		vpW_L = max(1, vs.width-border)
		vpW_R = vpW_L
		vpH_top = max(1, grid-border-title)
		vpH_commands = max(1, grid-border-title-input)
		vpH_mfe = vpH_top
		vpH_overlay = vpH_top
	} else {
		colL := vs.width / 2
		colR := vs.width - colL
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

	firstTime := vs.panelVPs[0].Width == 0
	if firstTime {
		for _, s := range contentSpecs {
			pv := &app.panels[s.pid]
			vs.panelVPs[s.pid] = viewport.New(s.w, s.h)
			vs.panelVPs[s.pid].SetContent(wrapContent(pv.activeBuf(), s.w))
			vs.panelVPs[s.pid].GotoBottom()
		}
		vs.commandsVP = viewport.New(vpW_L, vpH_commands)
		vs.commandsVP.SetContent(wrapContent(app.commandsBuf, vpW_L))
		vs.commandsVP.GotoBottom()
		vs.helpOverlayVP = viewport.New(vpW_L, vpH_overlay)
		vs.portsVP = viewport.New(vpW_R, vpH_top)
		vs.debugVP = viewport.New(vpW_R, vpH_top)
		vs.testVP = viewport.New(vpW_R, vpH_mfe)
	} else {
		for _, s := range contentSpecs {
			pv := &app.panels[s.pid]
			vp := &vs.panelVPs[s.pid]
			vp.Width = s.w
			vp.Height = s.h
			vp.SetContent(wrapContent(pv.activeBuf(), s.w))
			vp.GotoBottom()
		}
		vs.commandsVP.Width = vpW_L
		vs.commandsVP.Height = vpH_commands
		vs.commandsVP.SetContent(wrapContent(app.commandsBuf, vpW_L))
		vs.commandsVP.GotoBottom()
		vs.helpOverlayVP.Width = vpW_L
		vs.helpOverlayVP.Height = vpH_overlay
		vs.portsVP.Width = vpW_R
		vs.portsVP.Height = vpH_top
		vs.debugVP.Width = vpW_R
		vs.debugVP.Height = vpH_top
		vs.testVP.Width = vpW_R
		vs.testVP.Height = vpH_mfe
	}
	vs.portsVP.SetContent(vs.renderPortsContent(app))
	vs.debugVP.SetContent(vs.renderDebugContent(app))
	vs.testVP.SetContent(wrapContent(app.testBuf, vs.testVP.Width))
	vs.helpOverlayVP.SetContent(vs.helpContent(vpW_L, app.customCmds))
	vs.input.Width = vpW_L

	if app.wizard != nil {
		inputW := max(20, vpW_L-16)
		for i := range app.wizard.states {
			if app.wizard.states[i].spec.Kind != FieldKindSelect {
				app.wizard.states[i].pickerSearch.Width = inputW
			}
		}
	}
}

// ── Rendering ─────────────────────────────────────────────────────────────────

// render produces the full terminal frame from the current view and app state.
func (vs viewState) render(app appState) string {
	if !vs.ready {
		return "Initializing...\n"
	}

	bar := vs.renderTopBar(app)

	switch {
	case vs.fullscreenProgress >= 1:
		return lipgloss.JoinVertical(lipgloss.Left, bar, vs.renderFullscreen(app))
	case vs.fullscreenProgress > 0:
		return lipgloss.JoinVertical(lipgloss.Left, bar, vs.renderFullscreenTransition(app))
	}

	colL := vs.width / 2
	colR := vs.width - colL
	grid := vs.height - 1
	rowT := grid / 2
	rowB := grid - rowT

	topRow := lipgloss.JoinHorizontal(lipgloss.Top,
		vs.renderScrollPanel(app, panelMinikube, vs.panelTitle(app, PanelTopLeft, vs.focused == panelMinikube), vs.panelVPs[PanelTopLeft].View(), colL, rowT),
		vs.renderScrollPanel(app, panelSkaffold, vs.panelTitle(app, PanelTopRight, vs.focused == panelSkaffold), vs.skaffoldPanelView(app), colR, rowT),
	)
	bottomRow := lipgloss.JoinHorizontal(lipgloss.Top,
		vs.renderCommandsPanel(app, colL, rowB),
		vs.renderScrollPanel(app, panelMFE, vs.panelTitle(app, PanelBottomRight, vs.focused == panelMFE), vs.mfePanelView(app), colR, rowB),
	)

	return lipgloss.JoinVertical(lipgloss.Left, bar, topRow, bottomRow)
}

func (vs viewState) fullscreenHint(app appState) string {
	if app.inst.name == "" {
		return ""
	}
	var text string
	if vs.fullscreenTarget == 1 {
		text = "  ctrl+f to exit fullscreen"
	} else {
		text = "  ctrl+f to fullscreen"
	}
	return lipgloss.NewStyle().Foreground(currentTheme.Muted).Render(text)
}

func (vs viewState) renderFullscreen(app appState) string {
	w := vs.width
	grid := vs.height - 1
	switch vs.focused {
	case panelMinikube:
		return vs.renderScrollPanel(app, panelMinikube, vs.panelTitle(app, PanelTopLeft, true), vs.panelVPs[PanelTopLeft].View(), w, grid)
	case panelSkaffold:
		return vs.renderScrollPanel(app, panelSkaffold, vs.panelTitle(app, PanelTopRight, true), vs.skaffoldPanelView(app), w, grid)
	case panelMFE:
		return vs.renderScrollPanel(app, panelMFE, vs.panelTitle(app, PanelBottomRight, true), vs.mfePanelView(app), w, grid)
	default:
		return vs.renderCommandsPanel(app, w, grid)
	}
}

// renderFullscreenTransition renders the animated panel during fullscreen
// enter/exit. Viewport dimension mutations here are on a copy — intentional.
func (vs viewState) renderFullscreenTransition(app appState) string {
	p := vs.fullscreenProgress
	grid := vs.height - 1
	const border, titleH, inputH = 2, 2, 2

	colL := vs.width / 2
	colR := vs.width - colL
	rowT := grid / 2
	rowB := grid - rowT

	lerp := func(a, b int) int {
		return a + int(float64(b-a)*p)
	}

	var normalX, normalY, normalW, normalH int
	switch vs.focused {
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
	w := lerp(normalW, vs.width)
	h := lerp(normalH, grid)

	var panel string
	switch vs.focused {
	case panelMinikube:
		vs.panelVPs[PanelTopLeft].Width = max(1, w-border)
		vs.panelVPs[PanelTopLeft].Height = max(1, h-border-titleH)
		panel = vs.renderScrollPanel(app, panelMinikube, vs.panelTitle(app, PanelTopLeft, true), vs.panelVPs[PanelTopLeft].View(), w, h)
	case panelSkaffold:
		if app.isPortsTabActive(PanelTopRight) {
			vs.portsVP.Width = max(1, w-border)
			vs.portsVP.Height = max(1, h-border-titleH)
			vs.portsVP.SetContent(vs.renderPortsContent(app))
		} else if app.isDebugTabActive(PanelTopRight) {
			vs.debugVP.Width = max(1, w-border)
			vs.debugVP.Height = max(1, h-border-titleH)
			vs.debugVP.SetContent(vs.renderDebugContent(app))
		} else {
			vs.panelVPs[PanelTopRight].Width = max(1, w-border)
			vs.panelVPs[PanelTopRight].Height = max(1, h-border-titleH)
		}
		panel = vs.renderScrollPanel(app, panelSkaffold, vs.panelTitle(app, PanelTopRight, true), vs.skaffoldPanelView(app), w, h)
	case panelMFE:
		if app.isTestsTabActive(PanelBottomRight) {
			vs.testVP.Width = max(1, w-border)
			vs.testVP.Height = max(1, h-border-titleH)
			vs.testVP.SetContent(wrapContent(app.testBuf, vs.testVP.Width))
		} else {
			vs.panelVPs[PanelBottomRight].Width = max(1, w-border)
			vs.panelVPs[PanelBottomRight].Height = max(1, h-border-titleH)
		}
		panel = vs.renderScrollPanel(app, panelMFE, vs.panelTitle(app, PanelBottomRight, true), vs.mfePanelView(app), w, h)
	default: // panelCommands
		vs.commandsVP.Width = max(1, w-border)
		vs.commandsVP.Height = max(1, h-border-titleH-inputH)
		vs.input.Width = w - border
		panel = vs.renderCommandsPanel(app, w, h)
	}

	blank := strings.Repeat(" ", vs.width)
	leftPad := strings.Repeat(" ", x)
	lines := strings.Split(panel, "\n")

	out := make([]string, 0, grid)
	for range y {
		out = append(out, blank)
	}
	for _, line := range lines {
		vw := x + lipgloss.Width(line)
		rightPad := ""
		if vw < vs.width {
			rightPad = strings.Repeat(" ", vs.width-vw)
		}
		out = append(out, leftPad+line+rightPad)
	}
	for len(out) < grid {
		out = append(out, blank)
	}
	return strings.Join(out, "\n")
}

func (vs viewState) panelTitle(app appState, pid PanelID, focused bool) string {
	pv := app.panels[pid]
	dim := lipgloss.NewStyle().Foreground(currentTheme.Muted)

	hasPortsTab := pid == PanelTopRight && len(app.fwdPorts) > 0
	hasDebugTab := pid == PanelTopRight && len(app.debugPorts) > 0
	hasTestsTab := pid == PanelBottomRight && len(app.cfg.Tests) > 0
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
		if app.testRunning {
			testsLabel = "Tests " + spinnerFrames[vs.spinnerTick%len(spinnerFrames)]
		}
		if pv.activeIdx == tabIdx {
			parts[tabIdx] = active.Render(testsLabel)
		} else {
			parts[tabIdx] = dim.Render(testsLabel)
		}
	}
	title := " " + strings.Join(parts, sep)

	if focused && vs.searchMode {
		title += dim.Render("  ·  ") + vs.searchInput.View()
	} else {
		if focused && totalTabs > 1 {
			title += dim.Render("  ·  t to cycle")
		}
		if focused && app.isDebugTabActive(pid) {
			title += dim.Render("  ·  c to copy")
		}
	}
	if pid == PanelTopRight && vs.flashMsg != "" {
		var s lipgloss.Style
		var icon string
		if vs.flashOk {
			s = lipgloss.NewStyle().Foreground(currentTheme.Focused).Bold(true)
			icon = "✓ "
		} else {
			s = lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Bold(true)
			icon = "✗ "
		}
		title += "  " + s.Render(icon+vs.flashMsg)
	}
	return title
}

func (vs viewState) renderTopBar(app appState) string {
	var text string
	if app.cfg.StatusLine != nil {
		text = app.cfg.StatusLine(app.inst.name)
	} else if app.inst.name != "" {
		text = app.inst.name
	} else {
		text = "no instance running"
	}
	return topBarStyle().Width(vs.width).Render(text)
}

func (vs viewState) renderScrollPanel(app appState, panel int, title, content string, w, _ int) string {
	focused := vs.focused == panel
	if focused {
		title += vs.fullscreenHint(app)
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

func (vs viewState) mfePanelView(app appState) string {
	if app.isTestsTabActive(PanelBottomRight) {
		return vs.testVP.View()
	}
	return vs.panelVPs[PanelBottomRight].View()
}

func (vs viewState) skaffoldPanelView(app appState) string {
	if app.isPortsTabActive(PanelTopRight) {
		return vs.portsVP.View()
	}
	if app.isDebugTabActive(PanelTopRight) {
		return vs.debugVP.View()
	}
	return vs.panelVPs[PanelTopRight].View()
}

func (vs viewState) renderCommandsPanel(app appState, w, h int) string {
	focused := vs.focused == panelCommands
	hint := ""
	if focused {
		hint = vs.fullscreenHint(app)
	}

	const border = 2
	const titleH = 2
	innerH := h - border - titleH

	p := vs.flipProgress

	var titleText string
	var content string

	spinner := ""
	if vs.runningCmds > 0 {
		spinner = " " + lipgloss.NewStyle().Foreground(currentTheme.Focused).Render(spinnerFrames[vs.spinnerTick%len(spinnerFrames)])
	}

	switch {
	case p <= 0:
		titleText = " Commands" + spinner + hint
		content = vs.commandsContent(w)

	case p >= 1:
		titleText, content = vs.renderOverlay(app, w, innerH)
		titleText += spinner + hint

	case p < 0.5:
		multiplier := 1.0 - 2.0*p
		shrunkH := max(0, int(float64(innerH)*multiplier))
		titleText = " Commands" + spinner + hint
		if shrunkH < 2 {
			content = strings.Repeat("\n", innerH-1)
		} else {
			tmpVP := vs.commandsVP
			tmpVP.Height = max(1, shrunkH-2)
			sep := separatorStyle().Render(strings.Repeat("─", w-2))
			partial := lipgloss.JoinVertical(lipgloss.Left, tmpVP.View(), sep, vs.input.View())
			content = padToHeight(partial, tmpVP.Height+2, innerH)
		}

	default:
		multiplier := 2.0*p - 1.0
		expandH := max(1, int(float64(innerH)*multiplier))
		titleText, content = vs.renderOverlayExpanding(app, w, innerH, expandH)
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

func (vs viewState) commandsContent(w int) string {
	sep := separatorStyle().Render(strings.Repeat("─", w-2))
	return lipgloss.JoinVertical(lipgloss.Left,
		vs.commandsVP.View(),
		sep,
		vs.input.View(),
	)
}

func padToHeight(rendered string, current, target int) string {
	if current >= target {
		return rendered
	}
	return rendered + strings.Repeat("\n", target-current)
}

func (vs viewState) renderOverlay(app appState, w, innerH int) (string, string) {
	switch app.overlay {
	case overlayHelp:
		return " Help", vs.helpOverlayVP.View()
	case overlayWizard:
		raw := vs.renderWizard(app)
		return vs.wizardTitle(), padToHeight(raw, strings.Count(raw, "\n")+1, innerH)
	}
	return " Commands", ""
}

func (vs viewState) renderOverlayExpanding(app appState, w, innerH, expandH int) (string, string) {
	switch app.overlay {
	case overlayHelp:
		tmpVP := vs.helpOverlayVP
		tmpVP.Height = expandH
		return " Help", padToHeight(tmpVP.View(), expandH, innerH)
	case overlayWizard:
		raw := vs.renderWizard(app)
		rawLines := strings.Split(raw, "\n")
		visible := min(expandH, len(rawLines))
		return vs.wizardTitle(), padToHeight(strings.Join(rawLines[:visible], "\n"), visible, innerH)
	}
	return " Commands", ""
}

// ── Port / debug content ──────────────────────────────────────────────────────

func (vs viewState) launchJSONString(app appState) string {
	var lines []string
	var configNames []string

	lines = append(lines, `{`)
	lines = append(lines, `  "version": "0.2.0",`)
	lines = append(lines, `  "configurations": [`)
	for i, p := range app.debugPorts {
		addr := p.Address
		if addr == "" {
			addr = "127.0.0.1"
		}

		name := p.ResourceName
		if name == "" {
			name = fmt.Sprintf("port-%d", p.LocalPort)
		}
		configNames = append(configNames, "Attach "+name)

		comma := ","
		if i == len(app.debugPorts)-1 {
			comma = ""
		}
		for _, line := range vscodeLaunchConfig(p, addr) {
			lines = append(lines, "    "+line)
		}
		lines = append(lines, "    }"+comma)
	}
	lines = append(lines, `  ]`)

	if len(configNames) > 1 {
		lines = append(lines, `,`)
		lines = append(lines, `  "compounds": [`)
		lines = append(lines, `    {`)
		lines = append(lines, `      "name": "Attach All",`)
		lines = append(lines, `      "configurations": [`)
		for i, name := range configNames {
			comma := ","
			if i == len(configNames)-1 {
				comma = ""
			}
			lines = append(lines, fmt.Sprintf(`        "%s"%s`, name, comma))
		}
		lines = append(lines, `      ]`)
		lines = append(lines, `    }`)
		lines = append(lines, `  ]`)
	}

	lines = append(lines, `}`)
	return strings.Join(lines, "\n")
}

func (vs viewState) renderPortsContent(app appState) string {
	if len(app.fwdPorts) == 0 {
		return ""
	}
	var lines []string
	lines = append(lines, "  Forwarded ports:")
	for _, p := range app.fwdPorts {
		addr := p.Address
		if addr == "" {
			addr = "127.0.0.1"
		}
		lines = append(lines, fmt.Sprintf("  %-24s %s:%d", p.ResourceName, addr, p.LocalPort))
	}
	return strings.Join(lines, "\n")
}

func (vs viewState) renderDebugContent(app appState) string {
	if len(app.debugPorts) == 0 {
		return ""
	}
	var lines []string
	lines = append(lines, "  Debug ports:")
	for _, p := range app.debugPorts {
		addr := p.Address
		if addr == "" {
			addr = "127.0.0.1"
		}
		lines = append(lines, fmt.Sprintf("  %-24s %s:%d  (%s)", p.ResourceName, addr, p.LocalPort, debugRuntime(p.PortName)))
	}
	lines = append(lines, "")
	lines = append(lines, "  VSCode launch.json:")
	for _, line := range strings.Split(vs.launchJSONString(app), "\n") {
		lines = append(lines, "    "+line)
	}
	return strings.Join(lines, "\n")
}

// ── Wizard rendering ──────────────────────────────────────────────────────────

func (vs viewState) wizardTitle() string {
	return " Start"
}

func (vs viewState) renderWizard(app appState) string {
	if app.wizard == nil {
		return ""
	}
	return vs.renderWizardCustom(app)
}

func (vs viewState) renderWizardCustom(app appState) string {
	wiz := app.wizard
	ws := currentWizStyles()
	numFields := len(wiz.states)

	labelW := 8
	for _, s := range wiz.states {
		if n := len([]rune(s.spec.Label)); n > labelW {
			labelW = n
		}
	}

	var lines []string
	lines = append(lines, "")

	prevTemplateIdx := -1
	values := wiz.buildValues()

	for i := range wiz.states {
		s := &wiz.states[i]

		currentTemplateIdx := wiz.templateIdxs[i]
		if currentTemplateIdx != prevTemplateIdx {
			if len(lines) > 1 {
				lines = append(lines, "")
			}
			header := renderSectionHeader(wiz.templates[currentTemplateIdx], values, vs.commandsVP.Width-4)
			if header != "" {
				lines = append(lines, "  "+header)
			}
			prevTemplateIdx = currentTemplateIdx
		} else {
			if i > 0 {
				prev := &wiz.states[i-1]
				if !(s.spec.Kind == FieldKindSelect && prev.spec.Kind == FieldKindSelect) {
					lines = append(lines, "")
				}
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
			if len(s.resolvedOptions) > 0 {
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

// ── Search ────────────────────────────────────────────────────────────────────

// wrapContentSearch wraps content like wrapContent, but highlights lines
// containing query (case-insensitive) and dims non-matching lines.
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

// ── Help overlay ──────────────────────────────────────────────────────────────

func (vs viewState) helpContent(width int, customCmds map[string]CommandSpec) string {
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
		{"status", "show step status"},
		{"logs", "show log file paths"},
		{"test [label]", "run a test suite"},
		{"theme [name]", "set color theme"},
		{"", ""},
		{"Enter", "run command"},
		{"Esc", "close overlay"},
	})

	var customSection string
	if len(customCmds) > 0 {
		entries := make([]helpEntry, 0, len(customCmds))
		names := make([]string, 0, len(customCmds))
		for name := range customCmds {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			cmd := customCmds[name]
			helpText := cmd.Help
			if helpText == "" {
				helpText = "(no description)"
			}
			entries = append(entries, helpEntry{key: name, desc: helpText})
		}
		customSection = helpSection("Step Commands", entries)
	}

	global := helpSection("Global", []helpEntry{
		{"Ctrl+C", "quit"},
	})

	hs := helpTextStyle()

	if customSection != "" {
		if width >= 96 {
			cw := width / 3
			right := lipgloss.JoinVertical(lipgloss.Left, customSection, "", global)
			return lipgloss.JoinHorizontal(lipgloss.Top,
				hs.Width(cw).Render(nav),
				hs.Width(cw).Render(cmds),
				hs.Width(width-2*cw).Render(right),
			)
		}
		if width >= 64 {
			cw := width / 2
			right := lipgloss.JoinVertical(lipgloss.Left, cmds, "", customSection, "", global)
			return lipgloss.JoinHorizontal(lipgloss.Top,
				hs.Width(cw).Render(nav),
				hs.Width(width-cw).Render(right),
			)
		}
		return hs.Render(lipgloss.JoinVertical(lipgloss.Left, nav, "", cmds, "", customSection, "", global))
	}

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

// ── Wizard render helpers ─────────────────────────────────────────────────────

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

func renderSectionHeader(tmpl StepTemplate, values WizardValues, width int) string {
	label := tmpl.Label
	if tmpl.LabelFunc != nil {
		label = tmpl.LabelFunc(values)
	}
	if label == "" {
		return ""
	}
	const sepChar = "▬"
	prefix := sepChar + sepChar + sepChar + " " + label + " "
	remaining := width - lipgloss.Width(prefix)
	if remaining < 0 {
		remaining = 0
	}
	separator := prefix + strings.Repeat(sepChar, remaining)
	return lipgloss.NewStyle().Foreground(currentTheme.Muted).Render(separator)
}

func renderSelectField(i int, s *fieldState, ws wizStyles, activeField, labelW int) []string {
	focused := activeField == i
	return []string{
		"  " + wizLabel(s.spec.Label, activeField, i, labelW) + "  " +
			horizSelector(s.selectIdx, s.resolvedOptions, focused, ws.hl, ws.sel, ws.dim),
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
		if focused && len(s.resolvedOptions) > 0 {
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
