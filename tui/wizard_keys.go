package tui

import tea "github.com/charmbracelet/bubbletea"

func (m *model) handleWizardKey(msg tea.KeyMsg) {
	wiz := m.wizard
	if wiz == nil {
		return
	}
	key := msg.String()
	numFields := len(wiz.states)

	// Tab / Shift+Tab: close picker when open, else cycle fields.
	switch key {
	case "tab":
		if wiz.anyPickerOpen() {
			if s := wiz.activeState(); s != nil {
				s.pickerOpen = false
			}
			wiz.syncFocus()
		} else {
			wiz.fieldIdx = (wiz.fieldIdx + 1) % (numFields + 1)
			if s := wiz.activeState(); s != nil && s.spec.Kind == FieldKindSystemSelect {
				s.collapsedIdx = 0
			}
			wiz.syncFocus()
		}
		return
	case "shift+tab":
		if wiz.anyPickerOpen() {
			if s := wiz.activeState(); s != nil {
				s.pickerOpen = false
			}
			wiz.syncFocus()
		} else {
			wiz.fieldIdx = (wiz.fieldIdx - 1 + numFields + 1) % (numFields + 1)
			if s := wiz.activeState(); s != nil && s.spec.Kind == FieldKindSystemSelect {
				s.collapsedIdx = len(s.multiValues)
			}
			wiz.syncFocus()
		}
		return
	}

	// Buttons row.
	if wiz.fieldIdx == numFields {
		switch key {
		case "left":
			if wiz.confirmIdx > 0 {
				wiz.confirmIdx--
			}
		case "right":
			if wiz.confirmIdx < 1 {
				wiz.confirmIdx++
			}
		case "up":
			if numFields > 0 {
				wiz.fieldIdx = numFields - 1
				wiz.syncFocus()
			}
		case "down":
			wiz.fieldIdx = 0
			wiz.syncFocus()
		case "enter":
			if wiz.confirmIdx == 0 {
				m.executeStartFromWizard()
			}
			m.flipTarget = 0.0
		}
		return
	}

	s := &wiz.states[wiz.fieldIdx]
	next := func() {
		wiz.fieldIdx = (wiz.fieldIdx + 1) % (numFields + 1)
		wiz.syncFocus()
	}
	prev := func() {
		wiz.fieldIdx = (wiz.fieldIdx - 1 + numFields + 1) % (numFields + 1)
		wiz.syncFocus()
	}

	switch s.spec.Kind {
	case FieldKindSelect:
		switch key {
		case "left":
			if s.selectIdx > 0 {
				s.selectIdx--
			}
		case "right":
			if s.selectIdx < len(s.resolvedOptions)-1 {
				s.selectIdx++
			}
		case "up":
			prev()
		case "down", "enter":
			next()
		}

	case FieldKindSystemSelect:
		if s.pickerOpen {
			switch key {
			case "up":
				if s.pickerIdx > 0 {
					s.pickerIdx--
				}
			case "down":
				if s.pickerIdx < len(s.sysPickerItems)-1 {
					s.pickerIdx++
				}
			case "enter":
				s.toggleSysPicker(s.pickerIdx)
			default:
				s.pickerSearch, _ = s.pickerSearch.Update(msg)
				s.updateSysFilter()
			}
		} else {
			switch key {
			case "up":
				if s.collapsedIdx > 0 {
					s.collapsedIdx--
				} else {
					prev()
				}
			case "down":
				if s.collapsedIdx < len(s.multiValues) {
					s.collapsedIdx++
				} else {
					next()
				}
			case "x":
				if s.collapsedIdx < len(s.multiValues) {
					s.multiValues = append(s.multiValues[:s.collapsedIdx], s.multiValues[s.collapsedIdx+1:]...)
					if s.collapsedIdx > len(s.multiValues) {
						s.collapsedIdx = len(s.multiValues)
					}
				}
			case "enter":
				s.pickerOpen = true
				s.pickerSearch.SetValue("")
				s.updateSysFilter()
				wiz.syncFocus()
			}
		}

	case FieldKindSingleSelect:
		if s.pickerOpen {
			switch key {
			case "up":
				if s.pickerIdx > 0 {
					s.pickerIdx--
				}
			case "down":
				if s.pickerIdx < len(s.strPickerItems)-1 {
					s.pickerIdx++
				}
			case "enter":
				if s.pickerIdx >= 0 && s.pickerIdx < len(s.strPickerItems) {
					s.singleValue = s.strPickerItems[s.pickerIdx]
				}
				s.pickerOpen = false
				wiz.syncFocus()
			default:
				s.pickerSearch, _ = s.pickerSearch.Update(msg)
				s.updateStrFilter()
			}
		} else {
			switch key {
			case "up":
				prev()
			case "down":
				next()
			case "enter":
				if len(s.resolvedOptions) > 0 {
					s.pickerOpen = true
					s.pickerSearch.SetValue("")
					s.updateStrFilter()
					wiz.syncFocus()
				}
			case "x":
				s.singleValue = ""
			}
		}

	case FieldKindText:
		switch key {
		case "up":
			prev()
		case "down", "enter":
			next()
		default:
			s.pickerSearch, _ = s.pickerSearch.Update(msg)
		}

	case FieldKindMultiSelect:
		if s.pickerOpen {
			switch key {
			case "up":
				if s.pickerIdx > 0 {
					s.pickerIdx--
				}
			case "down":
				if s.pickerIdx < len(s.strPickerItems)-1 {
					s.pickerIdx++
				}
			case "enter":
				if s.pickerIdx >= 0 && s.pickerIdx < len(s.strPickerItems) {
					s.toggleMulti(s.strPickerItems[s.pickerIdx])
				}
			default:
				s.pickerSearch, _ = s.pickerSearch.Update(msg)
				s.updateStrFilter()
			}
		} else {
			switch key {
			case "up":
				if s.collapsedIdx > 0 {
					s.collapsedIdx--
				} else {
					prev()
				}
			case "down":
				if s.collapsedIdx < len(s.multiValues) {
					s.collapsedIdx++
				} else {
					next()
				}
			case "x":
				if s.collapsedIdx < len(s.multiValues) {
					s.multiValues = append(s.multiValues[:s.collapsedIdx], s.multiValues[s.collapsedIdx+1:]...)
					if s.collapsedIdx > len(s.multiValues) {
						s.collapsedIdx = len(s.multiValues)
					}
				}
			case "enter":
				s.pickerOpen = true
				s.pickerSearch.SetValue("")
				s.updateStrFilter()
				wiz.syncFocus()
			}
		}
	}
}
