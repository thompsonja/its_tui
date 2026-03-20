package step

import (
	"context"
	"os/exec"
	"strings"
)

// Send is the package-level sender registered by SetSender.
// Step implementations use it to forward messages to the TUI program.
var Send func(any)

// SetSender registers the TUI message sender. Call once, before starting any steps.
func SetSender(fn func(any)) { Send = fn }

// Sender is an optional interface that steps can implement to receive an
// injected message sender. Steps that implement Sender can be tested
// independently by injecting a test sender, without relying on the global Send.
type Sender interface {
	SetSender(fn func(any))
}

// MFECommand describes how to run a micro-frontend.
type MFECommand struct {
	Cmd  string            // executable name
	Args []string          // arguments
	Dir  string            // working directory
	Env  map[string]string // extra environment variables (merged with os.Environ)
}

// Step describes a managed process within an instance.
type Step interface {
	// ID returns a unique key used to track this step.
	ID() string

	// LogPath returns the path to this step's log file for the given instance.
	// Return "" for steps that send output directly (e.g. polling steps).
	LogPath(instanceName string) string

	// Start launches the step and blocks until it is running/ready or fails.
	// ctx is cancelled when the instance is stopped or switched.
	Start(ctx context.Context, instanceName string) error

	// Stop performs cleanup when the instance is stopped.
	// Return nil if no cleanup is needed.
	Stop(ctx context.Context, instanceName string) error
}

// Resumer is an optional interface that steps can implement to customize
// their resume behavior when the TUI is restarted with an existing instance.
// If a step implements Resumer, Resume() is called instead of Start() during
// session restore (unless the step previously failed, in which case Start() is
// always called to retry).
type Resumer interface {
	// Resume is called when restoring a session for a step that previously
	// completed or was running when the TUI exited.
	//
	// Return nil to indicate the step is ready (e.g., process still running,
	// or work already done and still valid).
	//
	// Return an error if the step needs to be restarted via Start().
	//
	// Common patterns:
	//   - Check if process is still running and return nil if so
	//   - Verify previous work is still valid (e.g., cluster exists)
	//   - Always return error to force restart (same as not implementing Resumer)
	Resume(ctx context.Context, instanceName string) error
}

// WatchStep tails the step's log file and forwards each line via Send.
// Steps with no log file (LogPath=="") are skipped — they send output themselves.
// Blocks until ctx is cancelled — call it in a goroutine.
func WatchStep(ctx context.Context, s Step, instanceName string) {
	logPath := s.LogPath(instanceName)
	if logPath == "" {
		return
	}
	id := s.ID()
	cmd := exec.CommandContext(ctx, "tail", "-F", "-n", "50", logPath)
	streamCmd(ctx, cmd, func(line string) {
		if strings.HasPrefix(line, "tail: ") {
			return
		}
		Send(LineMsg{ID: id, Line: line})
	})
}

// ResumeStep handles session restore for steps based on their previous state.
//
// The resume logic:
// 1. If step already completed and doesn't implement Resumer: skip (work is done)
// 2. If step implements Resumer: call Resume() to check if step is ready
//    - If Resume() returns nil: step is ready, no action needed
//    - If Resume() returns error: call Start() to restart the step
// 3. If step was running or pending: call Start() to restart
//
// This allows steps to define custom resume behavior:
//   - Minikube can check if cluster is still running
//   - Skaffold can always restart its process
//   - One-time setup steps can skip if work is done
func ResumeStep(ctx context.Context, s Step, instanceName string, wasCompleted bool) error {
	// If step has custom resume logic, use it
	if resumer, ok := s.(Resumer); ok {
		if err := resumer.Resume(ctx, instanceName); err != nil {
			// Resume failed, need to call Start()
			return s.Start(ctx, instanceName)
		}
		// Resume succeeded, step is ready
		return nil
	}

	// Default behavior: skip completed steps, restart others
	if wasCompleted {
		// No custom resume logic and step completed - assume work is still valid
		return nil
	}

	// Step was not completed (running/pending/failed) - restart it
	return s.Start(ctx, instanceName)
}
