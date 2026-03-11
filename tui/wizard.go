package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
)

// pickerItem is one row in the hierarchical component picker: either a system
// header or an individual component nested under a system.
type pickerItem struct {
	isSystem bool
	system   string
	comp     string // empty for system header rows
}

// fieldState holds the runtime state for one wizard field.
type fieldState struct {
	spec FieldSpec

	// FieldKindSelect: index of the currently selected option.
	selectIdx int

	// FieldKindSingleSelect: the currently selected value.
	singleValue string

	// FieldKindMultiSelect / FieldKindSystemSelect: selected items.
	multiValues  []string
	collapsedIdx int // cursor in collapsed list (0..len(multiValues) = Add button)

	// Picker state for Single / Multi / System select kinds.
	pickerOpen     bool
	pickerSearch   textinput.Model
	pickerIdx      int
	sysPickerItems []pickerItem // FieldKindSystemSelect filtered view
	strPickerItems []string     // FieldKindSingleSelect / FieldKindMultiSelect filtered view

	// resolvedSystems / resolvedOptions hold the full (unfiltered) item list
	// as resolved at wizard-open time (from SystemsFunc/OptionsFunc or the
	// static slice). Filter functions use these instead of re-reading the spec.
	resolvedSystems []System
	resolvedOptions []string
}

// startWizard drives the instance-start configuration screen.
type startWizard struct {
	fields     []FieldSpec  // all specs collected from templates, in order
	states     []fieldState // one per field
	fieldIdx   int          // index into states; len(states) = Buttons row
	confirmIdx int          // 0 = Start, 1 = Cancel
}

// buildValues collects the wizard's current selections into a WizardValues.
func (w *startWizard) buildValues() WizardValues {
	v := WizardValues{
		str:  make(map[string]string),
		strs: make(map[string][]string),
	}
	for _, s := range w.states {
		switch s.spec.Kind {
		case FieldKindSelect:
			if s.selectIdx >= 0 && s.selectIdx < len(s.spec.Options) {
				v.str[s.spec.ID] = s.spec.Options[s.selectIdx]
			}
		case FieldKindSingleSelect:
			if s.singleValue != "" {
				v.str[s.spec.ID] = s.singleValue
			}
		case FieldKindMultiSelect, FieldKindSystemSelect:
			v.strs[s.spec.ID] = s.multiValues
		case FieldKindText:
			v.str[s.spec.ID] = s.pickerSearch.Value()
		}
	}
	return v
}

// anyPickerOpen returns true if any field currently has its picker open.
func (w *startWizard) anyPickerOpen() bool {
	for _, s := range w.states {
		if s.pickerOpen {
			return true
		}
	}
	return false
}

// activeState returns a pointer to the focused fieldState, or nil when the
// Buttons row (fieldIdx == len(states)) is active.
func (w *startWizard) activeState() *fieldState {
	if w.fieldIdx >= 0 && w.fieldIdx < len(w.states) {
		return &w.states[w.fieldIdx]
	}
	return nil
}

// updateSysFilter refreshes sysPickerItems based on the current search query.
func (s *fieldState) updateSysFilter() {
	q := strings.ToLower(s.pickerSearch.Value())
	s.sysPickerItems = s.sysPickerItems[:0]
	for _, sys := range s.resolvedSystems {
		sysMatches := q == "" || strings.Contains(strings.ToLower(sys.Name), q)
		var matched []Component
		for _, c := range sys.Components {
			if sysMatches || strings.Contains(strings.ToLower(c.Name), q) {
				matched = append(matched, c)
			}
		}
		if len(matched) > 0 {
			s.sysPickerItems = append(s.sysPickerItems, pickerItem{isSystem: true, system: sys.Name})
			for _, c := range matched {
				s.sysPickerItems = append(s.sysPickerItems, pickerItem{isSystem: false, system: sys.Name, comp: c.Name})
			}
		}
	}
	if s.pickerIdx >= len(s.sysPickerItems) && len(s.sysPickerItems) > 0 {
		s.pickerIdx = len(s.sysPickerItems) - 1
	}
	if len(s.sysPickerItems) == 0 {
		s.pickerIdx = 0
	}
}

// updateStrFilter refreshes strPickerItems based on the current search query.
func (s *fieldState) updateStrFilter() {
	q := strings.ToLower(s.pickerSearch.Value())
	s.strPickerItems = s.strPickerItems[:0]
	for _, opt := range s.resolvedOptions {
		if q == "" || strings.Contains(strings.ToLower(opt), q) {
			s.strPickerItems = append(s.strPickerItems, opt)
		}
	}
	if s.pickerIdx >= len(s.strPickerItems) && len(s.strPickerItems) > 0 {
		s.pickerIdx = len(s.strPickerItems) - 1
	}
	if len(s.strPickerItems) == 0 {
		s.pickerIdx = 0
	}
}

// isMultiSelected returns true if name appears in the multiValues selection.
func (s *fieldState) isMultiSelected(name string) bool {
	for _, v := range s.multiValues {
		if v == name {
			return true
		}
	}
	return false
}

// toggleMulti adds or removes name from multiValues.
func (s *fieldState) toggleMulti(name string) {
	for i, v := range s.multiValues {
		if v == name {
			s.multiValues = append(s.multiValues[:i], s.multiValues[i+1:]...)
			return
		}
	}
	s.multiValues = append(s.multiValues, name)
}

// toggleSysPicker toggles the item at idx in the hierarchical picker.
// For system header rows, it toggles all visible components under that system.
func (s *fieldState) toggleSysPicker(idx int) {
	if idx < 0 || idx >= len(s.sysPickerItems) {
		return
	}
	item := s.sysPickerItems[idx]
	if !item.isSystem {
		s.toggleMulti(item.comp)
		return
	}
	var comps []string
	for _, pi := range s.sysPickerItems {
		if !pi.isSystem && pi.system == item.system {
			comps = append(comps, pi.comp)
		}
	}
	allSelected := len(comps) > 0
	for _, c := range comps {
		if !s.isMultiSelected(c) {
			allSelected = false
			break
		}
	}
	for _, c := range comps {
		if allSelected {
			for i, v := range s.multiValues {
				if v == c {
					s.multiValues = append(s.multiValues[:i], s.multiValues[i+1:]...)
					break
				}
			}
		} else if !s.isMultiSelected(c) {
			s.multiValues = append(s.multiValues, c)
		}
	}
}

// newStartWizard creates a wizard driven by the templates in m.cfg.Steps.
// initial, if non-empty, pre-populates fields from a previously saved session.
func newStartWizard(m *model, initial WizardValues) *startWizard {
	inputW := max(20, m.commandsVP.Width-16)

	// Collect all fields from all templates, in template order.
	var fields []FieldSpec
	for _, tmpl := range m.cfg.Steps {
		fields = append(fields, tmpl.Fields...)
	}

	states := make([]fieldState, len(fields))
	for i, spec := range fields {
		s := fieldState{spec: spec, selectIdx: spec.Default}

		// Pre-populate from initial values (last session's wizard selections).
		switch spec.Kind {
		case FieldKindSelect:
			if v := initial.String(spec.ID); v != "" {
				for idx, opt := range spec.Options {
					if opt == v {
						s.selectIdx = idx
						break
					}
				}
			}
		case FieldKindSingleSelect:
			s.singleValue = initial.String(spec.ID)
		case FieldKindMultiSelect, FieldKindSystemSelect:
			if vals := initial.Strings(spec.ID); len(vals) > 0 {
				s.multiValues = append([]string(nil), vals...)
			}
		case FieldKindText:
			// pre-populated below in the setup switch
		}

		// Set up picker search inputs and initial item lists.
		switch spec.Kind {
		case FieldKindSystemSelect:
			search := textinput.New()
			search.Placeholder = "search systems or components…"
			search.Width = inputW
			s.pickerSearch = search
			systems := spec.Systems
			if spec.SystemsFunc != nil {
				systems = spec.SystemsFunc()
			}
			s.resolvedSystems = systems
			for _, sys := range systems {
				s.sysPickerItems = append(s.sysPickerItems, pickerItem{isSystem: true, system: sys.Name})
				for _, c := range sys.Components {
					s.sysPickerItems = append(s.sysPickerItems, pickerItem{isSystem: false, system: sys.Name, comp: c.Name})
				}
			}
		case FieldKindSingleSelect, FieldKindMultiSelect:
			search := textinput.New()
			search.Placeholder = "search…"
			search.Width = inputW
			s.pickerSearch = search
			opts := spec.Options
			if spec.OptionsFunc != nil {
				opts = spec.OptionsFunc()
			}
			s.resolvedOptions = opts
			s.strPickerItems = append([]string(nil), opts...)
		case FieldKindText:
			ti := textinput.New()
			ti.Placeholder = "…"
			ti.Width = inputW
			if v := initial.String(spec.ID); v != "" {
				ti.SetValue(v)
			}
			s.pickerSearch = ti
		}
		states[i] = s
	}

	return &startWizard{
		fields: fields,
		states: states,
	}
}

// syncFocus focuses the active picker search input and blurs all others.
func (w *startWizard) syncFocus() {
	for i := range w.states {
		s := &w.states[i]
		if s.spec.Kind == FieldKindSelect {
			continue
		}
		if s.spec.Kind == FieldKindText {
			if i == w.fieldIdx {
				s.pickerSearch.Focus()
			} else {
				s.pickerSearch.Blur()
			}
			continue
		}
		if i == w.fieldIdx && s.pickerOpen {
			s.pickerSearch.Focus()
		} else {
			s.pickerSearch.Blur()
		}
	}
}
