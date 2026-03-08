package tui

import tea "github.com/charmbracelet/bubbletea"

func (m *model) handleWizardKey(msg tea.KeyMsg) {
	wiz := m.wizard
	if wiz == nil {
		return
	}
	key := msg.String()

	// Tab/Shift+Tab: cycle fields when pickers are closed; close picker when open.
	switch key {
	case "tab":
		if wiz.compPickerOpen {
			wiz.compPickerOpen = false
			wiz.syncFocus()
		} else if wiz.mfePickerOpen {
			wiz.mfePickerOpen = false
			wiz.syncFocus()
		} else {
			wiz.field = (wiz.field + 1) % wizNumFields
			if wiz.field == wizFieldComponents {
				wiz.selectedIdx = 0
			}
			wiz.syncFocus()
		}
		return
	case "shift+tab":
		if wiz.compPickerOpen {
			wiz.compPickerOpen = false
			wiz.syncFocus()
		} else if wiz.mfePickerOpen {
			wiz.mfePickerOpen = false
			wiz.syncFocus()
		} else {
			wiz.field = (wiz.field - 1 + wizNumFields) % wizNumFields
			if wiz.field == wizFieldComponents {
				wiz.selectedIdx = len(wiz.selectedComps)
			}
			wiz.syncFocus()
		}
		return
	}

	switch wiz.field {
	case wizFieldCPU:
		switch key {
		case "left":
			if wiz.cpuIdx > 0 {
				wiz.cpuIdx--
			}
		case "right":
			if wiz.cpuIdx < len(cpuOptions)-1 {
				wiz.cpuIdx++
			}
		case "up":
			wiz.field = wizFieldButtons
			wiz.syncFocus()
		case "down", "enter":
			wiz.field = wizFieldRAM
			wiz.syncFocus()
		}

	case wizFieldRAM:
		switch key {
		case "left":
			if wiz.ramIdx > 0 {
				wiz.ramIdx--
			}
		case "right":
			if wiz.ramIdx < len(ramOptions)-1 {
				wiz.ramIdx++
			}
		case "up":
			wiz.field = wizFieldCPU
			wiz.syncFocus()
		case "down", "enter":
			wiz.field = wizFieldComponents
			wiz.selectedIdx = 0
			wiz.syncFocus()
		}

	case wizFieldComponents:
		if wiz.compPickerOpen {
			switch key {
			case "up":
				if wiz.compPickerIdx > 0 {
					wiz.compPickerIdx--
				}
			case "down":
				if wiz.compPickerIdx < len(wiz.compPickerItems)-1 {
					wiz.compPickerIdx++
				}
			case "enter":
				wiz.togglePickerItem(wiz.compPickerIdx)
			default:
				wiz.compPickerSearch, _ = wiz.compPickerSearch.Update(msg)
				wiz.updateCompFilter()
			}
		} else {
			switch key {
			case "up":
				if wiz.selectedIdx > 0 {
					wiz.selectedIdx--
				} else {
					wiz.field = wizFieldRAM
					wiz.syncFocus()
				}
			case "down":
				if wiz.selectedIdx < len(wiz.selectedComps) {
					wiz.selectedIdx++
				} else {
					wiz.field = wizFieldMFE
					wiz.syncFocus()
				}
			case "x":
				if wiz.selectedIdx < len(wiz.selectedComps) {
					wiz.selectedComps = append(wiz.selectedComps[:wiz.selectedIdx], wiz.selectedComps[wiz.selectedIdx+1:]...)
					if wiz.selectedIdx > len(wiz.selectedComps) {
						wiz.selectedIdx = len(wiz.selectedComps)
					}
				}
			case "enter":
				wiz.compPickerOpen = true
				wiz.compPickerSearch.SetValue("")
				wiz.updateCompFilter()
				wiz.syncFocus()
			}
		}

	case wizFieldMFE:
		if wiz.mfePickerOpen {
			switch key {
			case "up":
				if wiz.mfePickerIdx > 0 {
					wiz.mfePickerIdx--
				}
			case "down":
				if wiz.mfePickerIdx < len(wiz.mfePickerItems)-1 {
					wiz.mfePickerIdx++
				}
			case "enter":
				if wiz.mfePickerIdx >= 0 && wiz.mfePickerIdx < len(wiz.mfePickerItems) {
					wiz.selectedMFE = wiz.mfePickerItems[wiz.mfePickerIdx]
				}
				wiz.mfePickerOpen = false
				wiz.syncFocus()
			default:
				wiz.mfePickerSearch, _ = wiz.mfePickerSearch.Update(msg)
				wiz.updateMFEFilter()
			}
		} else {
			switch key {
			case "up":
				wiz.field = wizFieldComponents
				wiz.selectedIdx = len(wiz.selectedComps)
				wiz.syncFocus()
			case "down":
				wiz.field = wizFieldMode
				wiz.syncFocus()
			case "enter":
				if len(wiz.mfeAll) > 0 {
					wiz.mfePickerOpen = true
					wiz.mfePickerSearch.SetValue("")
					wiz.updateMFEFilter()
					wiz.syncFocus()
				}
			case "x":
				wiz.selectedMFE = ""
			}
		}

	case wizFieldMode:
		switch key {
		case "left":
			if wiz.modeIdx > 0 {
				wiz.modeIdx--
			}
		case "right":
			if wiz.modeIdx < len(skaffoldModes)-1 {
				wiz.modeIdx++
			}
		case "up":
			wiz.field = wizFieldMFE
			wiz.syncFocus()
		case "down", "enter":
			wiz.field = wizFieldButtons
			wiz.syncFocus()
		}

	case wizFieldButtons:
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
			wiz.field = wizFieldMode
			wiz.syncFocus()
		case "down":
			wiz.field = wizFieldCPU
			wiz.syncFocus()
		case "enter":
			if wiz.confirmIdx == 0 {
				m.executeStartFromWizard()
			}
			m.flipTarget = 0.0
		}
	}
}
