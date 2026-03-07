package tui

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


const maxBufLines = 1000

type model struct {
	width, height int
	ready         bool
	focused       int

	cfg       Config  // library configuration provided by the caller
	instance  Instance
	configs   Configs // parsed from YAML config file
	statePath string  // path to state.json

	minikubeVP    viewport.Model
	skaffoldVP    viewport.Model
	commandsVP    viewport.Model
	mfeVP         viewport.Model
	helpOverlayVP viewport.Model // shown inside commands panel when help is active

	input textinput.Model

	minikubeBuf          []string // kubectl get pods output
	minikubeLogBuf       []string // minikube start log output
	minikubeShowLog      bool     // true = show log, false = show kubectl
	minikubeAutoSwitched bool     // true after the one-time auto-switch to kubectl has fired
	skaffoldBuf          []string
	commandsBuf          []string
	mfeBuf               []string

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

	// steps tracks in-progress operations shown as spinner lines in the commands panel.
	steps map[string]*commandStep
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
		minikubeShowLog:    true,
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
