package tui

import (
	"os"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// Panel focus indices — order determines Tab cycle direction.
const (
	panelMinikube = iota
	panelSkaffold
	panelCommands
	panelMFE
	numPanels
)

// overlayKind identifies which overlay is currently shown in the Commands panel.
type overlayKind int

const (
	overlayNone   overlayKind = iota
	overlayHelp               // help reference card
	overlayWizard             // start-instance wizard
)

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

func init() {
	if len(spinnerFrames) == 0 {
		panic("tui: spinnerFrames must not be empty")
	}
}

// model is the Bubbletea tea.Model shim. It owns an appState (domain) and a
// viewState (display), keeping the two concerns clearly separated.
type model struct {
	app appState
	vs  viewState
}

func newModel(cfg Config) model {
	ti := textinput.New()
	ti.Placeholder = "type a command (try: help)"
	ti.CharLimit = 512
	ti.Focus()

	si := textinput.New()
	si.Placeholder = "search..."
	si.CharLimit = 128

	workspaceDir := cfg.WorkspaceDir
	if workspaceDir == "" {
		workspaceDir, _ = os.Getwd()
	}

	return model{
		app: appState{
			cfg:          cfg,
			workspaceDir: workspaceDir,
			historyIdx:   -1,
			customCmds:   make(map[string]CommandSpec),
		},
		vs: viewState{
			focused:            panelCommands,
			input:              ti,
			searchInput:        si,
			fullscreenProgress: 1.0,
			fullscreenTarget:   1.0,
		},
	}
}

func (m model) Init() tea.Cmd {
	return tickCmd()
}

func tickCmd() tea.Cmd {
	return tea.Tick(time.Second/60, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// printLine appends a line to the commands buffer and syncs the viewport.
func (m *model) printLine(s string) {
	m.app.commandsBuf = appendLine(m.app.commandsBuf, s)
	m.vs.commandsVP.SetContent(wrapContent(m.app.commandsBuf, m.vs.commandsVP.Width))
	m.vs.commandsVP.GotoBottom()
}

func (m *model) cycleFocus(d int) {
	m.vs.focused = (m.vs.focused + d + numPanels) % numPanels
	if m.vs.focused == panelCommands {
		m.vs.input.Focus()
	} else {
		m.vs.input.Blur()
	}
}

// resizePanels recalculates all viewport dimensions and syncs content.
func (m *model) resizePanels() {
	m.vs.resize(m.app)
}

// refreshFocusedPanel regenerates the focused panel viewport, applying search
// highlighting when search mode is active.
func (m *model) refreshFocusedPanel() {
	pid, ok := focusedPanelID(m.vs.focused)
	if !ok {
		return
	}
	pv := &m.app.panels[pid]
	if pv.activeIdx >= len(pv.bufs) {
		return
	}
	buf := pv.bufs[pv.activeIdx]
	vp := &m.vs.panelVPs[pid]
	if m.vs.searchMode && m.vs.searchQuery != "" {
		vp.SetContent(wrapContentSearch(buf, vp.Width, m.vs.searchQuery))
	} else {
		vp.SetContent(wrapContent(buf, vp.Width))
	}
}
