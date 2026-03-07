package tui

import (
	"context"
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// Step describes a managed process within an instance.
// Implement this interface to add new runnable components.
type Step interface {
	// ID returns a unique key used to track this step in the commands panel.
	ID() string

	// LogPath returns the path to this step's log file for the given instance.
	LogPath(instanceName string) string

	// PanelLine wraps a log line in the correct panel message so the TUI routes
	// it to the right viewport.
	PanelLine(line string) tea.Msg

	// Start launches the step and blocks until it is running/ready or fails.
	// ctx is cancelled when the instance is stopped or switched.
	Start(ctx context.Context, instanceName string) error

	// Stop gracefully shuts the step down.
	// Steps terminated solely by context cancellation may return nil immediately.
	Stop(ctx context.Context, instanceName string) error

	// IsReady reports whether the step is fully ready for dependent steps to start.
	// For most steps this is true immediately after Start returns nil.
	IsReady(ctx context.Context, instanceName string) bool

	// ReadConfig reads the step's settings from the relevant fields of cfg,
	// leaving any fields that are not present in cfg at their current values.
	ReadConfig(cfg InstanceConfig)

	// WriteConfig writes the step's current settings into the relevant fields of cfg.
	WriteConfig(cfg *InstanceConfig)
}

// watchStep tails the step's log file and forwards each line to its TUI panel.
// It blocks until ctx is cancelled — call it in a goroutine.
func watchStep(ctx context.Context, s Step, instanceName string) {
	if instanceName == "" {
		return
	}
	cmd := exec.CommandContext(ctx, "tail", "-F", "-n", "50", s.LogPath(instanceName))
	streamCmd(ctx, cmd, func(line string) tea.Msg {
		if strings.HasPrefix(line, "tail: ") {
			return nil
		}
		return s.PanelLine(line)
	})
}
