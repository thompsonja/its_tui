package tui

import "time"

// tickMsg drives the 60fps render loop.
type tickMsg time.Time

// One message type per panel so Update can route cleanly.
type (
	minikubeLineMsg  string   // appends one line to the minikube log buffer
	minikubeSetMsg   []string // replaces the kubectl (pods) buffer
	minikubeReadyMsg struct{} // one-time: kubectl is up → auto-switch to kubectl tab
	skaffoldLineMsg  string
	commandLineMsg   string
	mfeLineMsg       string
	mfePIDMsg        int // process group ID reported by MFEStep.Start after cmd.Start()

	// stepDoneMsg marks a named step as complete (ok=true) or failed (ok=false),
	// updating its spinner line to ✓ / ✗ and the given label.
	stepDoneMsg struct {
		id    string
		ok    bool
		label string
	}

	// stepActivateMsg transitions a pending step to active (animating spinner).
	stepActivateMsg struct{ id string }

	// instanceStoppedMsg signals that full instance teardown is complete.
	instanceStoppedMsg struct{}

	// cmdActiveMsg adjusts the count of running background commands.
	// Send +1 when a command starts, -1 when it finishes.
	cmdActiveMsg int
)
