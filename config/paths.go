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

// DefaultStatePath returns ~/.tui/state.json.
func DefaultStatePath() string {
	return filepath.Join(tuiDir(), "state.json")
}

// logDir is the package-level root directory for log files.
// Set by SetLogDir; defaults to /tmp.
var logDir = "/tmp"

// SetLogDir configures the root directory for log files.
// If dir is empty, defaults to /tmp.
// The path is cleaned and validated to prevent path traversal attacks.
// Returns an error if the path contains invalid components.
func SetLogDir(dir string) error {
	if dir == "" {
		logDir = "/tmp"
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
		logDir = "/tmp"
		return nil
	}

	logDir = cleaned
	return nil
}

// GetLogDir returns the current log directory.
func GetLogDir() string {
	return logDir
}

// SkaffoldLogPath returns the per-instance log file written by skaffold.
func SkaffoldLogPath(instanceName string) string {
	if instanceName == "" {
		return ""
	}
	return filepath.Join(logDir, fmt.Sprintf("skaffold_%s.log", instanceName))
}

// MinikubeLogPath returns the per-instance log file written by minikube start.
func MinikubeLogPath(instanceName string) string {
	if instanceName == "" {
		return ""
	}
	return filepath.Join(logDir, fmt.Sprintf("minikube_%s.log", instanceName))
}

// MfeLogPath returns the per-instance log file written by the MFE process.
func MfeLogPath(instanceName string) string {
	if instanceName == "" {
		return ""
	}
	return filepath.Join(logDir, fmt.Sprintf("mfe_%s.log", instanceName))
}
