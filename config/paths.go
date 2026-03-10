package config

import (
	"fmt"
	"os"
	"path/filepath"
)

func tuiDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".tui")
}

// DefaultStatePath returns ~/.tui/state.json.
func DefaultStatePath() string {
	return filepath.Join(tuiDir(), "state.json")
}

// SkaffoldLogPath returns the per-instance log file written by skaffold.
func SkaffoldLogPath(instanceName string) string {
	if instanceName == "" {
		return ""
	}
	return fmt.Sprintf("/tmp/skaffold_%s.log", instanceName)
}

// MinikubeLogPath returns the per-instance log file written by minikube start.
func MinikubeLogPath(instanceName string) string {
	if instanceName == "" {
		return ""
	}
	return fmt.Sprintf("/tmp/minikube_%s.log", instanceName)
}

// MfeLogPath returns the per-instance log file written by the MFE process.
func MfeLogPath(instanceName string) string {
	if instanceName == "" {
		return ""
	}
	return fmt.Sprintf("/tmp/mfe_%s.log", instanceName)
}
