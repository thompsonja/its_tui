package tui

import "time"

// tickMsg drives the 60fps render loop.
type tickMsg time.Time

type (
	// commandLineMsg appends a line to the commands panel.
	commandLineMsg string

	// stepDoneMsg marks a named step as complete (ok=true) or failed (ok=false),
	// updating its spinner line to ✓ / ✗ and the given label.
	stepDoneMsg struct {
		id    string
		ok    bool
		label string
	}

	// stepActivateMsg transitions a pending step to active (animating spinner)
	// and triggers AutoActivate panel switching if configured.
	stepActivateMsg struct{ id string }

	// stepDepReadyMsg signals that one dependency of a pending step has
	// completed. The waiting label is updated to cross off the finished dep.
	stepDepReadyMsg struct{ id, dep string }

	// instanceStoppedMsg signals that full instance teardown is complete.
	instanceStoppedMsg struct{}

	// cmdActiveMsg adjusts the count of running background commands.
	// Send +1 when a command starts, -1 when it finishes.
	cmdActiveMsg int

	// copyResultMsg is sent after a clipboard copy attempt.
	copyResultMsg struct {
		ok  bool
		msg string
	}

	// testLineMsg carries one line of streaming test output.
	testLineMsg string

	// testDoneMsg signals that a test run has completed.
	testDoneMsg struct{ ok bool }
)
