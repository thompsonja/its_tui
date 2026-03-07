package tui

// commandStep tracks a single named step shown in the commands panel.
// The line at bufIdx is updated in-place as the step progresses.
type commandStep struct {
	label   string
	bufIdx  int
	done    bool
	ok      bool
	pending bool // waiting on a prerequisite; shows static ○ instead of spinner
}

// startStep appends a spinner line for the given step id and records its
// buffer index so finishStep can update it in-place.
func (m *model) startStep(id, label string) {
	if m.steps == nil {
		m.steps = map[string]*commandStep{}
	}
	m.commandsBuf = appendLine(m.commandsBuf, "  ⠋ "+label)
	m.steps[id] = &commandStep{
		label:  label,
		bufIdx: len(m.commandsBuf) - 1,
	}
	m.commandsVP.SetContent(wrapContent(m.commandsBuf, m.commandsVP.Width))
	m.commandsVP.GotoBottom()
}

// startPendingStep appends a static ○ line for a step that is waiting on a
// prerequisite. Call stepActivateMsg when it is ready to begin.
func (m *model) startPendingStep(id, label string) {
	if m.steps == nil {
		m.steps = map[string]*commandStep{}
	}
	m.commandsBuf = appendLine(m.commandsBuf, "  ○ "+label)
	m.steps[id] = &commandStep{
		label:   label,
		bufIdx:  len(m.commandsBuf) - 1,
		pending: true,
	}
	m.commandsVP.SetContent(wrapContent(m.commandsBuf, m.commandsVP.Width))
	m.commandsVP.GotoBottom()
}

// finishStep marks a step done, replacing its indicator with ✓ or ✗.
func (m *model) finishStep(id string, ok bool, label string) {
	s, exists := m.steps[id]
	if !exists {
		return
	}
	s.done = true
	s.ok = ok
	s.label = label
	icon := "✓"
	if !ok {
		icon = "✗"
	}
	if s.bufIdx < len(m.commandsBuf) {
		m.commandsBuf[s.bufIdx] = "  " + icon + " " + label
	}
	m.commandsVP.SetContent(wrapContent(m.commandsBuf, m.commandsVP.Width))
}
