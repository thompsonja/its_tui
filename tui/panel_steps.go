package tui

import "strings"

// commandStep tracks a single named step shown in the commands panel.
// The line at bufIdx is updated in-place as the step progresses.
type commandStep struct {
	label       string
	bufIdx      int
	done        bool
	ok          bool
	pending     bool     // waiting on a prerequisite; shows static ○ instead of spinner
	pendingDeps []string // deps not yet completed; shrinks as deps finish
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

// startPendingStep appends a static ○ line for a step that is waiting on
// deps. The label is updated in-place as deps complete via stepDepReadyMsg.
func (m *model) startPendingStep(id, label string, deps []string) {
	if m.steps == nil {
		m.steps = map[string]*commandStep{}
	}
	line := "  ○ " + label + " (waiting for " + strings.Join(deps, ", ") + ")"
	m.commandsBuf = appendLine(m.commandsBuf, line)
	m.steps[id] = &commandStep{
		label:       label,
		bufIdx:      len(m.commandsBuf) - 1,
		pending:     true,
		pendingDeps: append([]string(nil), deps...),
	}
	m.commandsVP.SetContent(wrapContent(m.commandsBuf, m.commandsVP.Width))
	m.commandsVP.GotoBottom()
}

// depReady removes dep from a pending step's waiting list and rewrites its
// line. Called from the stepDepReadyMsg handler in Update.
func (m *model) depReady(id, dep string) {
	s, ok := m.steps[id]
	if !ok {
		return
	}
	for i, d := range s.pendingDeps {
		if d == dep {
			s.pendingDeps = append(s.pendingDeps[:i], s.pendingDeps[i+1:]...)
			break
		}
	}
	line := "  ○ " + s.label
	if len(s.pendingDeps) > 0 {
		line += " (waiting for " + strings.Join(s.pendingDeps, ", ") + ")"
	}
	if s.bufIdx < len(m.commandsBuf) {
		m.commandsBuf[s.bufIdx] = line
	}
	m.commandsVP.SetContent(wrapContent(m.commandsBuf, m.commandsVP.Width))
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
