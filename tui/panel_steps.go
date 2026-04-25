package tui

import "strings"

// commandStep tracks a single named step shown in the commands panel.
// The line at bufIdx is updated in-place as the step progresses.
type commandStep struct {
	label       string
	bufIdx      int
	done        bool
	ok          bool
	pending     bool
	pendingDeps []string
}

func (m *model) startStep(id, label string) {
	if m.vs.steps == nil {
		m.vs.steps = map[string]*commandStep{}
	}
	m.app.commandsBuf = appendLine(m.app.commandsBuf, "  ⠋ "+label)
	m.vs.steps[id] = &commandStep{
		label:  label,
		bufIdx: len(m.app.commandsBuf) - 1,
	}
	m.vs.commandsVP.SetContent(wrapContent(m.app.commandsBuf, m.vs.commandsVP.Width))
	m.vs.commandsVP.GotoBottom()
}

func (m *model) startPendingStep(id, label string, deps []string) {
	if m.vs.steps == nil {
		m.vs.steps = map[string]*commandStep{}
	}
	line := "  ○ " + label + " (waiting for " + strings.Join(deps, ", ") + ")"
	m.app.commandsBuf = appendLine(m.app.commandsBuf, line)
	m.vs.steps[id] = &commandStep{
		label:       label,
		bufIdx:      len(m.app.commandsBuf) - 1,
		pending:     true,
		pendingDeps: append([]string(nil), deps...),
	}
	m.vs.commandsVP.SetContent(wrapContent(m.app.commandsBuf, m.vs.commandsVP.Width))
	m.vs.commandsVP.GotoBottom()
}

func (m *model) depReady(id, dep string) {
	s, ok := m.vs.steps[id]
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
	if s.bufIdx < len(m.app.commandsBuf) {
		m.app.commandsBuf[s.bufIdx] = line
	}
	m.vs.commandsVP.SetContent(wrapContent(m.app.commandsBuf, m.vs.commandsVP.Width))
}

func (m *model) finishStep(id string, ok bool, label string) {
	s, exists := m.vs.steps[id]
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
	if s.bufIdx < len(m.app.commandsBuf) {
		m.app.commandsBuf[s.bufIdx] = "  " + icon + " " + label
	}
	m.vs.commandsVP.SetContent(wrapContent(m.app.commandsBuf, m.vs.commandsVP.Width))

	if ok && m.app.instanceName != "" {
		m.app.completedSteps++
		if m.app.completedSteps == m.app.totalSteps && m.app.totalSteps > 0 {
			sp := m.app.statePath
			go func() {
				if err := MarkReady(sp); err != nil {
					debugLog("finishStep: MarkReady: %v", err)
				}
				prog.Send(allStepsReadyMsg{})
			}()
		}
	}
}
