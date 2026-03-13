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

// ResumeStep restarts steps with no log file after a session restore.
// Steps with a log file are already covered by WatchStep.
func ResumeStep(ctx context.Context, s Step, instanceName string) {
	if s.LogPath(instanceName) != "" {
		return
	}
	_ = s.Start(ctx, instanceName)
}
