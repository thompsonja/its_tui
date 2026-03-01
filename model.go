package main

import (
	"os"
	"path/filepath"
	"sort"
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

var skaffoldModes = []string{"dev", "run", "debug"}
var cpuOptions = []string{"2", "4", "8", "16"}
var ramOptions = []string{"2g", "4g", "8g", "16g"}

// Wizard screens.
const (
	wizScreenSelect = 0 // mode-select: file vs custom
	wizScreenFile   = 1 // file-based wizard
	wizScreenCustom = 2 // custom-instance wizard
)

// File wizard field indices.
const (
	wizFieldName    = 0
	wizFieldConfig  = 1
	wizFieldMode    = 2
	wizFieldButtons = 3
	wizNumFields    = 4
)

// Custom wizard field indices.
const (
	custFieldName       = 0
	custFieldCPU        = 1
	custFieldRAM        = 2
	custFieldComponents = 3
	custFieldMFE        = 4
	custFieldMode       = 5
	custFieldButtons    = 6
	custNumFields       = 7
)

// pickerItem is one row in the hierarchical component picker: either a system
// header or an individual component nested under a system.
type pickerItem struct {
	isSystem bool
	system   string // system name
	comp     string // component name; empty for system rows
}

type startWizard struct {
	screen    int // wizScreenSelect / wizScreenFile / wizScreenCustom
	screenIdx int // cursor on the mode-select screen (0=file, 1=custom)

	// ── File wizard ──────────────────────────────────────────────────────────
	field       int
	nameInput   textinput.Model
	configInput textinput.Model
	modeIdx     int
	confirmIdx  int
	browseFiles []string
	browseIdx   int

	// ── Custom wizard ─────────────────────────────────────────────────────────
	custField      int
	custName       textinput.Model
	cpuIdx         int
	ramIdx         int
	custMFEInput   textinput.Model
	custModeIdx    int
	custConfirmIdx int

	// ── Component picker (shown when custFieldComponents is active) ───────────
	compAll          ComponentsFile
	selectedComps    []string
	custSelectedIdx  int // highlighted row in collapsed view (0..len-1 = comp, len = Add button)
	compPickerOpen   bool
	compPickerSearch textinput.Model
	compPickerItems  []pickerItem // filtered view
	compPickerIdx    int
}

func (w *startWizard) updateCompFilter() {
	q := strings.ToLower(w.compPickerSearch.Value())
	w.compPickerItems = w.compPickerItems[:0]
	for _, sys := range w.compAll.Systems {
		sysMatches := q == "" || strings.Contains(strings.ToLower(sys.Name), q)
		var matched []ComponentEntry
		for _, c := range sys.Components {
			if sysMatches || strings.Contains(strings.ToLower(c.Name), q) {
				matched = append(matched, c)
			}
		}
		if len(matched) > 0 {
			w.compPickerItems = append(w.compPickerItems, pickerItem{isSystem: true, system: sys.Name})
			for _, c := range matched {
				w.compPickerItems = append(w.compPickerItems, pickerItem{isSystem: false, system: sys.Name, comp: c.Name})
			}
		}
	}
	if w.compPickerIdx >= len(w.compPickerItems) && len(w.compPickerItems) > 0 {
		w.compPickerIdx = len(w.compPickerItems) - 1
	}
	if len(w.compPickerItems) == 0 {
		w.compPickerIdx = 0
	}
}

func (w *startWizard) isCompSelected(name string) bool {
	for _, s := range w.selectedComps {
		if s == name {
			return true
		}
	}
	return false
}

func (w *startWizard) toggleComp(name string) {
	for i, s := range w.selectedComps {
		if s == name {
			w.selectedComps = append(w.selectedComps[:i], w.selectedComps[i+1:]...)
			return
		}
	}
	w.selectedComps = append(w.selectedComps, name)
}

// togglePickerItem toggles the item at idx. For system rows, toggles all
// visible components; if all are already selected, deselects them.
func (w *startWizard) togglePickerItem(idx int) {
	if idx < 0 || idx >= len(w.compPickerItems) {
		return
	}
	item := w.compPickerItems[idx]
	if !item.isSystem {
		w.toggleComp(item.comp)
		return
	}
	// Collect visible components for this system.
	var comps []string
	for _, pi := range w.compPickerItems {
		if !pi.isSystem && pi.system == item.system {
			comps = append(comps, pi.comp)
		}
	}
	allSelected := len(comps) > 0
	for _, c := range comps {
		if !w.isCompSelected(c) {
			allSelected = false
			break
		}
	}
	for _, c := range comps {
		if allSelected {
			// Remove each one.
			for i, s := range w.selectedComps {
				if s == c {
					w.selectedComps = append(w.selectedComps[:i], w.selectedComps[i+1:]...)
					break
				}
			}
		} else if !w.isCompSelected(c) {
			w.selectedComps = append(w.selectedComps, c)
		}
	}
}

// newStartWizard creates a wizard pre-populated from the current model state.
func newStartWizard(m *model) *startWizard {
	inputW := max(20, m.commandsVP.Width-16)

	// ── File wizard inputs ────────────────────────────────────────────────────
	nameIn := textinput.New()
	nameIn.Placeholder = "instance-name"
	nameIn.CharLimit = 64
	nameIn.Width = inputW
	if m.instance.Name != "" {
		nameIn.SetValue(m.instance.Name)
	} else if len(m.configs) > 0 {
		names := make([]string, 0, len(m.configs))
		for n := range m.configs {
			names = append(names, n)
		}
		sort.Strings(names)
		nameIn.SetValue(names[0])
	}

	cfgIn := textinput.New()
	cfgIn.Placeholder = DefaultConfigPath()
	cfgIn.CharLimit = 256
	cfgIn.Width = inputW

	browseFiles := findYAMLConfigs()
	browseIdx := 0
	if _, err := os.Stat(DefaultConfigPath()); err == nil {
		cfgIn.SetValue(DefaultConfigPath())
	} else if _, err := os.Stat("config.yaml"); err == nil {
		cfgIn.SetValue("config.yaml")
	} else if len(browseFiles) > 0 {
		cfgIn.SetValue(browseFiles[0])
	}
	for i, f := range browseFiles {
		if f == cfgIn.Value() {
			browseIdx = i
			break
		}
	}

	// ── Custom wizard inputs ──────────────────────────────────────────────────
	custNameIn := textinput.New()
	custNameIn.Placeholder = "instance-name"
	custNameIn.CharLimit = 64
	custNameIn.Width = inputW
	if m.instance.Name != "" {
		custNameIn.SetValue(m.instance.Name)
	}

	custMFEIn := textinput.New()
	custMFEIn.Placeholder = "path/to/package.json"
	custMFEIn.CharLimit = 256
	custMFEIn.Width = inputW

	compSearch := textinput.New()
	compSearch.Placeholder = "search systems or components…"
	compSearch.Width = inputW

	compsFile, _ := LoadComponents("sample/components.json")

	wiz := &startWizard{
		screen:           wizScreenSelect,
		field:            wizFieldName,
		nameInput:        nameIn,
		configInput:      cfgIn,
		browseFiles:      browseFiles,
		browseIdx:        browseIdx,
		custName:         custNameIn,
		custMFEInput:     custMFEIn,
		compAll:          compsFile,
		compPickerSearch: compSearch,
	}
	wiz.updateCompFilter()
	return wiz
}

// syncFocus focuses the active text input and blurs all others.
func (w *startWizard) syncFocus() {
	w.nameInput.Blur()
	w.configInput.Blur()
	w.custName.Blur()
	w.compPickerSearch.Blur()
	w.custMFEInput.Blur()
	switch w.screen {
	case wizScreenFile:
		switch w.field {
		case wizFieldName:
			w.nameInput.Focus()
		case wizFieldConfig:
			w.configInput.Focus()
		}
	case wizScreenCustom:
		switch w.custField {
		case custFieldName:
			w.custName.Focus()
		case custFieldComponents:
			if w.compPickerOpen {
				w.compPickerSearch.Focus()
			}
		case custFieldMFE:
			w.custMFEInput.Focus()
		}
	}
}

// findYAMLConfigs returns a sorted list of .yaml/.yml files found in common
// locations relative to the working directory, plus the default config path.
func findYAMLConfigs() []string {
	seen := map[string]bool{}
	var files []string
	add := func(p string) {
		if !seen[p] {
			seen[p] = true
			files = append(files, p)
		}
	}
	for _, pattern := range []string{
		"*.yaml", "*.yml",
		"sample/*.yaml", "sample/*.yml",
		"config/*.yaml", "config/*.yml",
	} {
		matches, _ := filepath.Glob(pattern)
		for _, m := range matches {
			add(m)
		}
	}
	if p := DefaultConfigPath(); !seen[p] {
		if _, err := os.Stat(p); err == nil {
			add(p)
		}
	}
	sort.Strings(files)
	return files
}

const maxBufLines = 1000

// tickMsg drives the 60fps render loop.
type tickMsg time.Time

// One message type per panel so Update can route cleanly.
type (
	minikubeLineMsg  string   // appends one line to the minikube log buffer
	minikubeSetMsg   []string // replaces the kubectl (pods) buffer
	minikubeReadyMsg struct{} // one-time: kubectl is up → auto-switch to kubectl tab
	skaffoldLineMsg  string
	commandLineMsg   string
	mfeLineMsg       string

	// cmdActiveMsg adjusts the count of running background commands.
	// Send +1 when a command starts, -1 when it finishes.
	cmdActiveMsg int
)

type model struct {
	width, height int
	ready         bool
	focused       int

	instance  Instance
	configs   Configs // parsed from YAML config file
	statePath string  // path to state.json

	minikubeVP    viewport.Model
	skaffoldVP    viewport.Model
	commandsVP    viewport.Model
	mfeVP         viewport.Model
	helpOverlayVP viewport.Model // shown inside commands panel when help is active

	input textinput.Model

	minikubeBuf         []string // kubectl get pods output
	minikubeLogBuf      []string // minikube start log output
	minikubeShowLog     bool     // true = show log, false = show kubectl
	minikubeAutoSwitched bool   // true after the one-time auto-switch to kubectl has fired
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

	// spinner — shown in Commands title while background commands are running.
	runningCmds int
	spinnerTick int
}

func newModel() model {
	ti := textinput.New()
	ti.Placeholder = "type a command (try: help)"
	ti.CharLimit = 512
	ti.Focus()

	return model{
		focused:            panelCommands,
		input:              ti,
		historyIdx:         -1,
		minikubeShowLog:    true, // start with the Minikube tab selected
		// Start fullscreen until the user selects or starts an instance.
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
