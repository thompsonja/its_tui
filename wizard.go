package tui

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
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
	custModeIdx    int
	custConfirmIdx int

	// ── Component picker (shown when custFieldComponents is active) ───────────
	compAll          []System
	selectedComps    []string
	custSelectedIdx  int // highlighted row in collapsed view (0..len-1 = comp, len = Add button)
	compPickerOpen   bool
	compPickerSearch textinput.Model
	compPickerItems  []pickerItem // filtered view
	compPickerIdx    int

	// ── MFE picker (shown when custFieldMFE is active) ───────────────────────
	mfeAll          []string
	selectedMFE     string
	mfePickerOpen   bool
	mfePickerSearch textinput.Model
	mfePickerItems  []string // filtered view
	mfePickerIdx    int
}

func (w *startWizard) updateCompFilter() {
	q := strings.ToLower(w.compPickerSearch.Value())
	w.compPickerItems = w.compPickerItems[:0]
	for _, sys := range w.compAll {
		sysMatches := q == "" || strings.Contains(strings.ToLower(sys.Name), q)
		var matched []Component
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

func (w *startWizard) updateMFEFilter() {
	q := strings.ToLower(w.mfePickerSearch.Value())
	w.mfePickerItems = w.mfePickerItems[:0]
	for _, mfe := range w.mfeAll {
		if q == "" || strings.Contains(strings.ToLower(mfe), q) {
			w.mfePickerItems = append(w.mfePickerItems, mfe)
		}
	}
	if w.mfePickerIdx >= len(w.mfePickerItems) && len(w.mfePickerItems) > 0 {
		w.mfePickerIdx = len(w.mfePickerItems) - 1
	}
	if len(w.mfePickerItems) == 0 {
		w.mfePickerIdx = 0
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

	compSearch := textinput.New()
	compSearch.Placeholder = "search systems or components…"
	compSearch.Width = inputW

	mfeSearch := textinput.New()
	mfeSearch.Placeholder = "search MFEs…"
	mfeSearch.Width = inputW

	wiz := &startWizard{
		screen:           wizScreenSelect,
		field:            wizFieldName,
		nameInput:        nameIn,
		configInput:      cfgIn,
		browseFiles:      browseFiles,
		browseIdx:        browseIdx,
		custName:         custNameIn,
		compAll:          m.cfg.Systems,
		compPickerSearch: compSearch,
		mfeAll:           m.cfg.MFEs,
		mfePickerSearch:  mfeSearch,
	}
	wiz.updateCompFilter()
	wiz.updateMFEFilter()
	return wiz
}

// syncFocus focuses the active text input and blurs all others.
func (w *startWizard) syncFocus() {
	w.nameInput.Blur()
	w.configInput.Blur()
	w.custName.Blur()
	w.compPickerSearch.Blur()
	w.mfePickerSearch.Blur()
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
			if w.mfePickerOpen {
				w.mfePickerSearch.Focus()
			}
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
