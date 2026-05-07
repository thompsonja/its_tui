package tui

import (
	"strings"
	"time"
)

const (
	barFilled    = "█"
	barShimmer   = "▓"
	barEmpty     = "░"
	minBarWidth  = 4
	minStepSpan  = time.Second // floor to avoid near-zero division
	stepEstSecs  = 60.0        // estimated batch duration; shimmer kicks in past this
	shimmerWidth = 5           // number of ▓ chars in the sweeping highlight
)

// commandStep tracks a single named step shown in the commands panel.
// The line at bufIdx is updated in-place as the step progresses.
type commandStep struct {
	label       string
	bufIdx      int
	done        bool
	ok          bool
	pending     bool
	pendingDeps []string
	startedAt   time.Time // zero until step becomes active
	finishedAt  time.Time // zero until step completes
}

// stepBarWidth returns the number of characters available for the progress bar.
// Line layout: "  " (2) + icon (1) + " " (1) + label (labelWidth) + " " (1) = 5 fixed.
func stepBarWidth(vpWidth, labelWidth int) int {
	avail := vpWidth - 5 - labelWidth
	if avail < minBarWidth {
		return minBarWidth
	}
	return avail
}

// computeStepSpan returns the total timeline width to use for bar scaling.
// While any step is still running it returns the fixed stepEstSecs estimate.
// Once all steps are done it returns max(finishedAt)-instanceStart so the
// last-finishing step fills all the way to the right edge with no trailing gray.
func computeStepSpan(instanceStart time.Time, steps map[string]*commandStep) time.Duration {
	if instanceStart.IsZero() {
		return minStepSpan
	}
	hasRunning := false
	var maxEnd time.Time
	for _, s := range steps {
		if !s.done && !s.pending {
			hasRunning = true
		}
		if s.done && s.finishedAt.After(maxEnd) {
			maxEnd = s.finishedAt
		}
	}
	if hasRunning {
		return time.Duration(stepEstSecs * float64(time.Second))
	}
	if !maxEnd.IsZero() {
		d := maxEnd.Sub(instanceStart)
		if d < minStepSpan {
			d = minStepSpan
		}
		return d
	}
	return minStepSpan
}

// shimmerBar returns a full █ bar with a ▓ highlight sweeping left-to-right,
// used when a running step has exceeded the estimated duration.
func shimmerBar(bw, tick int) string {
	buf := make([]rune, bw)
	for i := range buf {
		buf[i] = '█'
	}
	// Lead position advances one step every 2 ticks (~30 steps/sec at 60 fps).
	// Period extends past bw so the window enters and exits cleanly.
	lead := (tick / 2) % (bw + shimmerWidth)
	for w := 0; w < shimmerWidth; w++ {
		p := lead - shimmerWidth + 1 + w
		if p >= 0 && p < bw {
			buf[p] = '▓'
		}
	}
	return string(buf)
}

// stepBar builds the bar string showing when the step was running within the
// given total duration.
// stepStart.IsZero() → all empty (pending/never ran).
// stepEnd.IsZero()   → step is still running; use time.Now() as the right edge.
func stepBar(bw int, instanceStart, stepStart, stepEnd time.Time, totalDur time.Duration) string {
	if instanceStart.IsZero() || stepStart.IsZero() || totalDur <= 0 {
		return strings.Repeat(barEmpty, bw)
	}

	startFrac := float64(stepStart.Sub(instanceStart)) / float64(totalDur)
	if startFrac < 0 {
		startFrac = 0
	}
	if startFrac > 1 {
		startFrac = 1
	}

	end := stepEnd
	if end.IsZero() {
		end = time.Now()
	}
	endFrac := float64(end.Sub(instanceStart)) / float64(totalDur)
	if endFrac < 0 {
		endFrac = 0
	}
	if endFrac > 1 {
		endFrac = 1
	}

	startChar := int(startFrac * float64(bw))
	endChar := int(endFrac * float64(bw))
	// Guarantee at least 1 filled char so it's clear the step ran.
	if endChar <= startChar {
		endChar = startChar + 1
	}
	if endChar > bw {
		endChar = bw
		if startChar >= endChar {
			startChar = endChar - 1
		}
	}
	return strings.Repeat(barEmpty, startChar) +
		strings.Repeat(barFilled, endChar-startChar) +
		strings.Repeat(barEmpty, bw-endChar)
}

// renderStepLine assembles a full commands-panel line from a pre-built bar.
func renderStepLine(icon, label string, labelWidth int, bar string) string {
	vis := len([]rune(label))
	padded := label
	if vis < labelWidth {
		padded += strings.Repeat(" ", labelWidth-vis)
	}
	return "  " + icon + " " + padded + " " + bar
}

func (m *model) startStep(id, label string) {
	if m.vs.steps == nil {
		m.vs.steps = map[string]*commandStep{}
	}
	vpW := m.vs.commandsVP.Width
	lw := m.vs.stepLabelWidth
	now := time.Now()
	s := &commandStep{
		label:     label,
		bufIdx:    len(m.app.commandsBuf),
		startedAt: now,
	}
	m.vs.steps[id] = s
	frame := spinnerFrames[m.vs.spinnerTick%len(spinnerFrames)]
	bw := stepBarWidth(vpW, lw)
	span := computeStepSpan(m.app.inst.startedAt, m.vs.steps)
	bar := stepBar(bw, m.app.inst.startedAt, s.startedAt, time.Time{}, span)
	m.app.commandsBuf = appendLine(m.app.commandsBuf, renderStepLine(frame, label, lw, bar))
	m.vs.commandsVP.SetContent(wrapContent(m.app.commandsBuf, vpW))
	m.vs.commandsVP.GotoBottom()
}

func (m *model) startPendingStep(id, label string, deps []string) {
	if m.vs.steps == nil {
		m.vs.steps = map[string]*commandStep{}
	}
	vpW := m.vs.commandsVP.Width
	lw := m.vs.stepLabelWidth
	s := &commandStep{
		label:       label,
		bufIdx:      len(m.app.commandsBuf),
		pending:     true,
		pendingDeps: append([]string(nil), deps...),
	}
	m.vs.steps[id] = s
	bw := stepBarWidth(vpW, lw)
	m.app.commandsBuf = appendLine(m.app.commandsBuf, renderStepLine("○", label, lw,
		strings.Repeat(barEmpty, bw)))
	m.vs.commandsVP.SetContent(wrapContent(m.app.commandsBuf, vpW))
	m.vs.commandsVP.GotoBottom()
}

// reRenderStepBars redraws every step line in commandsBuf using the current
// commandsVP.Width and the dynamic span. Call this after any resize so all
// bars match the new pane dimensions.
func (m *model) reRenderStepBars() {
	if len(m.vs.steps) == 0 {
		return
	}
	vpW := m.vs.commandsVP.Width
	lw := m.vs.stepLabelWidth
	bw := stepBarWidth(vpW, lw)
	instanceStart := m.app.inst.startedAt
	span := computeStepSpan(instanceStart, m.vs.steps)
	frame := spinnerFrames[m.vs.spinnerTick%len(spinnerFrames)]
	pastEstimate := !instanceStart.IsZero() &&
		time.Since(instanceStart) >= time.Duration(stepEstSecs*float64(time.Second))

	for _, s := range m.vs.steps {
		if s.bufIdx >= len(m.app.commandsBuf) {
			continue
		}
		if s.pending {
			m.app.commandsBuf[s.bufIdx] = renderStepLine("○", s.label, lw,
				strings.Repeat(barEmpty, bw))
			continue
		}
		icon := frame
		if s.done {
			if s.ok {
				icon = "✓"
			} else {
				icon = "✗"
			}
		}
		var bar string
		if !s.done && pastEstimate {
			bar = shimmerBar(bw, m.vs.spinnerTick)
		} else {
			var stepEnd time.Time
			if s.done {
				stepEnd = s.finishedAt
			}
			bar = stepBar(bw, instanceStart, s.startedAt, stepEnd, span)
		}
		m.app.commandsBuf[s.bufIdx] = renderStepLine(icon, s.label, lw, bar)
	}
	m.vs.commandsVP.SetContent(wrapContent(m.app.commandsBuf, vpW))
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
	// The bar line stays as-is (○ icon, empty bar) until stepActivateMsg fires.
}

func (m *model) finishStep(id string, ok bool, label string) {
	s, exists := m.vs.steps[id]
	if !exists {
		return
	}
	s.done = true
	s.ok = ok
	s.finishedAt = time.Now()

	icon := "✓"
	if !ok {
		icon = "✗"
	}

	vpW := m.vs.commandsVP.Width
	lw := m.vs.stepLabelWidth
	bw := stepBarWidth(vpW, lw)
	instanceStart := m.app.inst.startedAt

	// Check whether all steps in this batch are now complete.
	allDone := len(m.vs.steps) > 0
	for _, step := range m.vs.steps {
		if !step.done {
			allDone = false
			break
		}
	}

	if allDone {
		// Final rescale: last-finishing step fills to the right edge, all others
		// scale proportionally with no trailing gray.
		span := computeStepSpan(instanceStart, m.vs.steps)
		for _, step := range m.vs.steps {
			if step.pending || step.bufIdx >= len(m.app.commandsBuf) {
				continue
			}
			stepIcon := "✓"
			if !step.ok {
				stepIcon = "✗"
			}
			bar := stepBar(bw, instanceStart, step.startedAt, step.finishedAt, span)
			m.app.commandsBuf[step.bufIdx] = renderStepLine(stepIcon, step.label, lw, bar)
		}
	} else {
		span := computeStepSpan(instanceStart, m.vs.steps)
		bar := stepBar(bw, instanceStart, s.startedAt, s.finishedAt, span)
		if s.bufIdx < len(m.app.commandsBuf) {
			m.app.commandsBuf[s.bufIdx] = renderStepLine(icon, s.label, lw, bar)
		}
	}

	m.vs.commandsVP.SetContent(wrapContent(m.app.commandsBuf, vpW))

	if ok && m.app.inst.name != "" {
		m.app.inst.completedSteps++
		if m.app.inst.completedSteps == m.app.inst.totalSteps && m.app.inst.totalSteps > 0 {
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
