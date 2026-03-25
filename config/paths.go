package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func tuiDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".tui")
}

// defaultLogDir returns ~/.tui/logs.
func defaultLogDir() string {
	return filepath.Join(tuiDir(), "logs")
}

// DefaultStatePath returns ~/.tui/state.json.
func DefaultStatePath() string {
	return filepath.Join(tuiDir(), "state.json")
}

// logDir is the package-level root directory for log files.
// Set by SetLogDir; defaults to ~/.tui/logs.
var logDir = defaultLogDir()

// SetLogDir configures the root directory for log files.
// If dir is empty, defaults to ~/.tui/logs.
// The path is cleaned and validated to prevent path traversal attacks.
// Returns an error if the path contains invalid components.
func SetLogDir(dir string) error {
	if dir == "" {
		logDir = defaultLogDir()
		return nil
	}

	// Reject any path containing ".." to prevent traversal attacks
	if strings.Contains(dir, "..") {
		return fmt.Errorf("log directory path contains invalid traversal: %s", dir)
	}

	// Clean the path to normalize it
	cleaned := filepath.Clean(dir)

	// Ensure it's not empty after cleaning
	if cleaned == "" || cleaned == "." {
		logDir = defaultLogDir()
		return nil
	}

	logDir = cleaned
	return nil
}

// GetLogDir returns the current log directory.
func GetLogDir() string {
	return logDir
}

// EnsureLogDir creates the log directory if it doesn't exist.
// This should be called before writing any log files.
func EnsureLogDir() error {
	return os.MkdirAll(logDir, 0755)
}
