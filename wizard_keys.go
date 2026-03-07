package tui

import tea "github.com/charmbracelet/bubbletea"

func (m *model) handleWizardKey(msg tea.KeyMsg) {
	wiz := m.wizard
	if wiz == nil {
		return
	}
	switch wiz.screen {
	case wizScreenSelect:
		m.handleWizardKeySelect(msg)
	case wizScreenFile:
		m.handleWizardKeyFile(msg)
	case wizScreenCustom:
		m.handleWizardKeyCustom(msg)
	}
}

func (m *model) handleWizardKeySelect(msg tea.KeyMsg) {
	wiz := m.wizard
	switch msg.String() {
	case "up":
		if wiz.screenIdx > 0 {
			wiz.screenIdx--
		}
	case "down":
		if wiz.screenIdx < 1 {
			wiz.screenIdx++
		}
	case "enter":
		if wiz.screenIdx == 0 {
			wiz.screen = wizScreenFile
		} else {
			wiz.screen = wizScreenCustom
		}
		wiz.syncFocus()
	}
}

func (m *model) handleWizardKeyFile(msg tea.KeyMsg) {
	wiz := m.wizard
	key := msg.String()

	switch key {
	case "tab":
		wiz.field = (wiz.field + 1) % wizNumFields
		wiz.syncFocus()
		return
	case "shift+tab":
		wiz.field = (wiz.field - 1 + wizNumFields) % wizNumFields
		wiz.syncFocus()
		return
	}

	switch wiz.field {
	case wizFieldName:
		switch key {
		case "down", "enter":
			wiz.field = wizFieldConfig
			wiz.syncFocus()
		case "up":
			wiz.field = wizFieldButtons
			wiz.syncFocus()
		default:
			wiz.nameInput, _ = wiz.nameInput.Update(msg)
		}

	case wizFieldConfig:
		switch key {
		case "up":
			if len(wiz.browseFiles) > 0 && wiz.browseIdx > 0 {
				wiz.browseIdx--
				wiz.configInput.SetValue(wiz.browseFiles[wiz.browseIdx])
			} else {
				wiz.field = wizFieldName
				wiz.syncFocus()
			}
		case "down":
			if len(wiz.browseFiles) > 0 && wiz.browseIdx < len(wiz.browseFiles)-1 {
				wiz.browseIdx++
				wiz.configInput.SetValue(wiz.browseFiles[wiz.browseIdx])
			} else {
				wiz.field = wizFieldMode
				wiz.syncFocus()
			}
		case "enter":
			if len(wiz.browseFiles) > 0 {
				wiz.configInput.SetValue(wiz.browseFiles[wiz.browseIdx])
			}
			wiz.field = wizFieldMode
			wiz.syncFocus()
		default:
			wiz.configInput, _ = wiz.configInput.Update(msg)
			v := wiz.configInput.Value()
			for i, f := range wiz.browseFiles {
				if f == v {
					wiz.browseIdx = i
					break
				}
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
			wiz.field = wizFieldConfig
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
			wiz.field = wizFieldName
			wiz.syncFocus()
		case "enter":
			if wiz.confirmIdx == 0 {
				m.executeStartFromWizard()
			}
			m.flipTarget = 0.0
		}
	}
}

func (m *model) handleWizardKeyCustom(msg tea.KeyMsg) {
	wiz := m.wizard
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
			wiz.custField = (wiz.custField + 1) % custNumFields
			if wiz.custField == custFieldComponents {
				wiz.custSelectedIdx = 0
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
			wiz.custField = (wiz.custField - 1 + custNumFields) % custNumFields
			if wiz.custField == custFieldComponents {
				wiz.custSelectedIdx = len(wiz.selectedComps)
			}
			wiz.syncFocus()
		}
		return
	}

	switch wiz.custField {
	case custFieldName:
		switch key {
		case "down", "enter":
			wiz.custField = custFieldCPU
			wiz.syncFocus()
		case "up":
			wiz.custField = custFieldButtons
			wiz.syncFocus()
		default:
			wiz.custName, _ = wiz.custName.Update(msg)
		}

	case custFieldCPU:
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
			wiz.custField = custFieldName
			wiz.syncFocus()
		case "down", "enter":
			wiz.custField = custFieldRAM
			wiz.syncFocus()
		}

	case custFieldRAM:
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
			wiz.custField = custFieldCPU
			wiz.syncFocus()
		case "down", "enter":
			wiz.custField = custFieldComponents
			wiz.custSelectedIdx = 0
			wiz.syncFocus()
		}

	case custFieldComponents:
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
				if wiz.custSelectedIdx > 0 {
					wiz.custSelectedIdx--
				} else {
					wiz.custField = custFieldRAM
					wiz.syncFocus()
				}
			case "down":
				if wiz.custSelectedIdx < len(wiz.selectedComps) {
					wiz.custSelectedIdx++
				} else {
					wiz.custField = custFieldMFE
					wiz.syncFocus()
				}
			case "x":
				if wiz.custSelectedIdx < len(wiz.selectedComps) {
					wiz.selectedComps = append(wiz.selectedComps[:wiz.custSelectedIdx], wiz.selectedComps[wiz.custSelectedIdx+1:]...)
					if wiz.custSelectedIdx > len(wiz.selectedComps) {
						wiz.custSelectedIdx = len(wiz.selectedComps)
					}
				}
			case "enter":
				wiz.compPickerOpen = true
				wiz.compPickerSearch.SetValue("")
				wiz.updateCompFilter()
				wiz.syncFocus()
			}
		}

	case custFieldMFE:
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
				wiz.custField = custFieldComponents
				wiz.custSelectedIdx = len(wiz.selectedComps)
				wiz.syncFocus()
			case "down":
				wiz.custField = custFieldMode
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

	case custFieldMode:
		switch key {
		case "left":
			if wiz.custModeIdx > 0 {
				wiz.custModeIdx--
			}
		case "right":
			if wiz.custModeIdx < len(skaffoldModes)-1 {
				wiz.custModeIdx++
			}
		case "up":
			wiz.custField = custFieldMFE
			wiz.syncFocus()
		case "down", "enter":
			wiz.custField = custFieldButtons
			wiz.syncFocus()
		}

	case custFieldButtons:
		switch key {
		case "left":
			if wiz.custConfirmIdx > 0 {
				wiz.custConfirmIdx--
			}
		case "right":
			if wiz.custConfirmIdx < 1 {
				wiz.custConfirmIdx++
			}
		case "up":
			wiz.custField = custFieldMode
			wiz.syncFocus()
		case "down":
			wiz.custField = custFieldName
			wiz.syncFocus()
		case "enter":
			if wiz.custConfirmIdx == 0 {
				m.executeStartFromWizard()
			}
			m.flipTarget = 0.0
		}
	}
}
