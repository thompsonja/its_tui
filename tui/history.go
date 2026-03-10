package tui

// addToHistory appends line to cmdHistory (skipping consecutive duplicates)
// and resets the navigation state.
func (m *model) addToHistory(line string) {
	if len(m.cmdHistory) == 0 || m.cmdHistory[len(m.cmdHistory)-1] != line {
		m.cmdHistory = append(m.cmdHistory, line)
	}
	m.historyIdx = -1
	m.historyDraft = ""
}

// historyUp moves one step back through command history.
func (m *model) historyUp() {
	if len(m.cmdHistory) == 0 {
		return
	}
	if m.historyIdx == -1 {
		m.historyDraft = m.input.Value()
		m.historyIdx = len(m.cmdHistory) - 1
	} else if m.historyIdx > 0 {
		m.historyIdx--
	}
	m.input.SetValue(m.cmdHistory[m.historyIdx])
}

// historyDown moves one step forward through command history, restoring the
// draft when the user navigates past the most recent entry.
func (m *model) historyDown() {
	if m.historyIdx == -1 {
		return
	}
	if m.historyIdx == len(m.cmdHistory)-1 {
		m.historyIdx = -1
		m.input.SetValue(m.historyDraft)
	} else {
		m.historyIdx++
		m.input.SetValue(m.cmdHistory[m.historyIdx])
	}
}
