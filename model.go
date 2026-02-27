package main

import (
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
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

// ── Start wizard ─────────────────────────────────────────────────────────────

var (
	cpuOptions = []string{"2 cores", "4 cores", "8 cores", "max"}
	cpuArgs    = []string{"2", "4", "8", "max"} // passed to --cpus

	ramOptions = []string{"2 GB", "4 GB", "8 GB", "16 GB", "max"}
	ramArgs    = []string{"2048", "4096", "8192", "16384", "max"} // MB passed to --memory
)

type startWizard struct {
	field      int // 0 = CPU, 1 = RAM, 2 = Buttons
	cpuIdx     int
	ramIdx     int
	confirmIdx int // 0 = Start, 1 = Cancel
}

const maxBufLines = 1000

// tickMsg drives the 60fps render loop.
type tickMsg time.Time

// One message type per panel so Update can route cleanly.
type (
	minikubeLineMsg string   // appends one line to the minikube panel
	minikubeSetMsg  []string // replaces the entire minikube panel buffer
	skaffoldLineMsg string
	commandLineMsg  string
	mfeLineMsg      string
)

type model struct {
	width, height int
	ready         bool
	focused       int

	instance Instance

	minikubeVP    viewport.Model
	skaffoldVP    viewport.Model
	commandsVP    viewport.Model
	mfeVP         viewport.Model
	helpOverlayVP viewport.Model // shown inside commands panel when help is active

	input textinput.Model

	minikubeBuf []string
	skaffoldBuf []string
	commandsBuf []string
	mfeBuf      []string

	// card-flip animation: 0.0 = commands fully visible, 1.0 = overlay fully visible.
	// flipTarget drives direction; flipProgress chases it on every tick.
	flipProgress float64
	flipTarget   float64
	overlay      overlayKind // which overlay is showing (or will show) when flipTarget == 1
	wizard       *startWizard

	// fullscreen animation: 0.0 = normal grid, 1.0 = focused panel fills screen.
	// fullscreenTarget drives direction; fullscreenProgress chases it on every tick.
	fullscreenProgress float64
	fullscreenTarget   float64

	// command history — navigated with ↑ / ↓ in the Commands panel.
	cmdHistory   []string
	historyIdx   int    // -1 = not navigating; ≥0 = index into cmdHistory
	historyDraft string // input saved when navigation starts, restored on ↓ past end
}

func newModel() model {
	ti := textinput.New()
	ti.Placeholder = "type a command (try: help)"
	ti.CharLimit = 512
	ti.Focus()

	return model{
		focused:    panelCommands,
		input:      ti,
		historyIdx: -1,
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
