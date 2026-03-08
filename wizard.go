package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
)

// ── Start wizard ─────────────────────────────────────────────────────────────

var skaffoldModes = []string{"dev", "run", "debug"}
var cpuOptions = []string{"2", "4", "8", "16"}
var ramOptions = []string{"2g", "4g", "8g", "16g"}

// Wizard field indices.
const (
	wizFieldCPU        = 0
	wizFieldRAM        = 1
	wizFieldComponents = 2
	wizFieldMFE        = 3
	wizFieldMode       = 4
	wizFieldButtons    = 5
	wizNumFields       = 6
)

// pickerItem is one row in the hierarchical component picker: either a system
// header or an individual component nested under a system.
type pickerItem struct {
	isSystem bool
	system   string // system name
	comp     string // component name; empty for system rows
}

type startWizard struct {
	field      int
	cpuIdx     int
	ramIdx     int
	modeIdx    int
	confirmIdx int

	// ── Component picker (shown when wizFieldComponents is active) ────────────
	compAll          []System
	selectedComps    []string
	selectedIdx      int // highlighted row in collapsed view (0..len-1 = comp, len = Add button)
	compPickerOpen   bool
	compPickerSearch textinput.Model
	compPickerItems  []pickerItem // filtered view
	compPickerIdx    int

	// ── MFE picker (shown when wizFieldMFE is active) ─────────────────────────
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

	compSearch := textinput.New()
	compSearch.Placeholder = "search systems or components…"
	compSearch.Width = inputW

	mfeSearch := textinput.New()
	mfeSearch.Placeholder = "search MFEs…"
	mfeSearch.Width = inputW

	wiz := &startWizard{
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
	w.compPickerSearch.Blur()
	w.mfePickerSearch.Blur()
	switch w.field {
	case wizFieldComponents:
		if w.compPickerOpen {
			w.compPickerSearch.Focus()
		}
	case wizFieldMFE:
		if w.mfePickerOpen {
			w.mfePickerSearch.Focus()
		}
	}
}
