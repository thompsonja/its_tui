package tui

func (m *model) addToHistory(line string) {
	if len(m.app.cmdHistory) == 0 || m.app.cmdHistory[len(m.app.cmdHistory)-1] != line {
		m.app.cmdHistory = append(m.app.cmdHistory, line)
	}
	m.app.historyIdx = -1
	m.app.historyDraft = ""
}

func (m *model) historyUp() {
	if len(m.app.cmdHistory) == 0 {
		return
	}
	if m.app.historyIdx == -1 {
		m.app.historyDraft = m.vs.input.Value()
		m.app.historyIdx = len(m.app.cmdHistory) - 1
	} else if m.app.historyIdx > 0 {
		m.app.historyIdx--
	}
	m.vs.input.SetValue(m.app.cmdHistory[m.app.historyIdx])
}

func (m *model) historyDown() {
	if m.app.historyIdx == -1 {
		return
	}
	if m.app.historyIdx == len(m.app.cmdHistory)-1 {
		m.app.historyIdx = -1
		m.vs.input.SetValue(m.app.historyDraft)
	} else {
		m.app.historyIdx++
		m.vs.input.SetValue(m.app.cmdHistory[m.app.historyIdx])
	}
}
