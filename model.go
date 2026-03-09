package tui

import (
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	"tui/step"
	tea "github.com/charmbracelet/bubbletea"
)

// Panel indices — order determines Tab cycle direction.
const (
	panelMinikube = iota
	panelSkaffold
	panelCommands
	panelMFE
	numPanels
)

// overlayKind identifies which overlay is currently shown inside the Commands panel.
type overlayKind int

const (
	overlayNone   overlayKind = iota
	overlayHelp               // help reference card
	overlayWizard             // start-instance wizard
)

const maxBufLines = 1000

// panelView holds the steps and log buffers for one content panel.
type panelView struct {
	defs      []StepDef  // steps assigned to this panel, in order
	bufs      [][]string // one buffer per def
	activeIdx int        // which step's buffer is currently shown
}

// activeBuf returns the buffer for the currently active step, or nil if empty.
func (pv *panelView) activeBuf() []string {
	if len(pv.bufs) == 0 || pv.activeIdx < 0 || pv.activeIdx >= len(pv.bufs) {
		return nil
	}
	return pv.bufs[pv.activeIdx]
}

type model struct {
	width, height int
	ready         bool
	focused       int

	cfg       Config   // library configuration provided by the caller
	instance  Instance
	statePath string   // path to state.json

	// Content panels: index by PanelID (0=TopLeft, 1=TopRight, 2=BottomRight).
	panels   [3]panelView
	panelVPs [3]viewport.Model

	commandsVP    viewport.Model
	helpOverlayVP viewport.Model // shown inside commands panel when help is active

	input textinput.Model

	commandsBuf []string

	// card-flip animation: 0.0 = commands fully visible, 1.0 = overlay fully visible.
	flipProgress float64
	flipTarget   float64
	overlay      overlayKind
	wizard       *startWizard

	// fullscreen animation: 0.0 = normal grid, 1.0 = focused panel fills screen.
	fullscreenProgress float64
	fullscreenTarget   float64

	// command history — navigated with ↑ / ↓ in the Commands panel.
	cmdHistory   []string
	historyIdx   int    // -1 = not navigating; ≥0 = index into cmdHistory
	historyDraft string // input saved when navigation starts, restored on ↓ past end

	// spinner — shown in Commands title while background commands are running.
	runningCmds int
	spinnerTick int

	// debugPorts collects port-forward events from skaffold debug.
	debugPorts []step.DebugPortMsg

	// steps tracks in-progress operations shown as spinner lines in the commands panel.
	steps map[string]*commandStep
}

// instanceName returns the configured instance name, falling back to the default.
func (m *model) instanceName() string {
	if m.cfg.InstanceName != "" {
		return m.cfg.InstanceName
	}
	return defaultInstanceName
}

func newModel(cfg Config) model {
	ti := textinput.New()
	ti.Placeholder = "type a command (try: help)"
	ti.CharLimit = 512
	ti.Focus()

	return model{
		cfg:                cfg,
		focused:            panelCommands,
		input:              ti,
		historyIdx:         -1,
		fullscreenProgress: 1.0,
		fullscreenTarget:   1.0,
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

// registerPipeline assigns steps to panels and initializes panel buffers.
func (m *model) registerPipeline(defs []StepDef) {
	var pv [3]panelView
	for _, def := range defs {
		pid := int(def.Panel)
		pv[pid].defs = append(pv[pid].defs, def)
		pv[pid].bufs = append(pv[pid].bufs, nil)
	}
	m.panels = pv
}

// panelAndIdx returns the PanelID and buffer index for the step with the given ID.
// Returns (0, -1) if not found.
func (m *model) panelAndIdx(id string) (PanelID, int) {
	for pid, pv := range m.panels {
		for i, def := range pv.defs {
			if def.Step.ID() == id {
				return PanelID(pid), i
			}
		}
	}
	return 0, -1
}

// findDef returns the StepDef for the given step ID.
func (m *model) findDef(id string) (StepDef, bool) {
	for _, pv := range m.panels {
		for _, def := range pv.defs {
			if def.Step.ID() == id {
				return def, true
			}
		}
	}
	return StepDef{}, false
}

// focusedPanelID returns the PanelID for the currently focused content panel.
// Returns (0, false) when the Commands panel is focused.
func (m *model) focusedPanelID() (PanelID, bool) {
	switch m.focused {
	case panelMinikube:
		return PanelTopLeft, true
	case panelSkaffold:
		return PanelTopRight, true
	case panelMFE:
		return PanelBottomRight, true
	}
	return 0, false
}

func appendLine(buf []string, line string) []string {
	buf = append(buf, line)
	if len(buf) > maxBufLines {
		buf = buf[len(buf)-maxBufLines:]
	}
	return buf
}

func joinLines(buf []string) string {
	return strings.Join(buf, "\n")
}

// wrapLine hard-wraps a single line at width runes, inserting newlines.
func wrapLine(line string, width int) string {
	runes := []rune(line)
	if width <= 0 || len(runes) <= width {
		return line
	}
	var sb strings.Builder
	for len(runes) > width {
		sb.WriteString(string(runes[:width]))
		sb.WriteByte('\n')
		runes = runes[width:]
	}
	if len(runes) > 0 {
		sb.WriteString(string(runes))
	}
	return sb.String()
}

// wrapContent wraps each line in buf to width and joins with newlines.
func wrapContent(buf []string, width int) string {
	if width <= 0 {
		return joinLines(buf)
	}
	result := make([]string, 0, len(buf))
	for _, line := range buf {
		result = append(result, wrapLine(line, width))
	}
	return strings.Join(result, "\n")
}

// appendToVP appends line to buf, syncs content to vp, and scrolls to bottom.
func appendToVP(buf *[]string, vp *viewport.Model, line string) {
	*buf = appendLine(*buf, line)
	vp.SetContent(wrapContent(*buf, vp.Width))
	vp.GotoBottom()
}
