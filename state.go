package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// ── JSON state ────────────────────────────────────────────────────────────────

// State tracks runtime data across TUI sessions.
type State struct {
	Instances       map[string]ActiveInstance `json:"instances"`
	CurrentInstance string                    `json:"current_instance,omitempty"`
	CommandHistory  []string                  `json:"command_history,omitempty"`
	Theme           string                    `json:"theme,omitempty"`
}

// ActiveInstance records runtime state for a started instance.
type ActiveInstance struct {
	StartedAt string `json:"started_at"`
	MFEPGID   int    `json:"mfe_pgid,omitempty"` // process group ID of the MFE process; 0 if not running
}

const maxHistoryLen = 10

// AppendCommandHistory adds line to the persisted command history, keeping the
// last maxHistoryLen entries.
func AppendCommandHistory(statePath, line string) error {
	s, err := LoadState(statePath)
	if err != nil {
		return err
	}
	s.CommandHistory = append(s.CommandHistory, line)
	if len(s.CommandHistory) > maxHistoryLen {
		s.CommandHistory = s.CommandHistory[len(s.CommandHistory)-maxHistoryLen:]
	}
	return SaveState(statePath, s)
}

// LoadState reads the state JSON file.
// A missing file returns an empty State (not an error).
func LoadState(path string) (State, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return State{Instances: map[string]ActiveInstance{}}, nil
		}
		return State{}, fmt.Errorf("reading state %s: %w", path, err)
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return State{}, fmt.Errorf("parsing state %s: %w", path, err)
	}
	if s.Instances == nil {
		s.Instances = map[string]ActiveInstance{}
	}
	return s, nil
}

// SaveState atomically writes the state JSON file, creating parent dirs as needed.
func SaveState(path string, s State) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating state dir: %w", err)
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	// Write atomically via a temp file + rename.
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// MarkActive records instanceName as active in the state file.
// Existing fields (e.g. MFEPGID) are preserved.
func MarkActive(statePath, instanceName string) error {
	s, err := LoadState(statePath)
	if err != nil {
		return err
	}
	inst := s.Instances[instanceName]
	inst.StartedAt = time.Now().UTC().Format(time.RFC3339)
	s.Instances[instanceName] = inst
	return SaveState(statePath, s)
}

// SaveMFEPGID persists the MFE process group ID for instanceName.
func SaveMFEPGID(statePath, instanceName string, pgid int) error {
	s, err := LoadState(statePath)
	if err != nil {
		return err
	}
	inst := s.Instances[instanceName]
	inst.MFEPGID = pgid
	s.Instances[instanceName] = inst
	return SaveState(statePath, s)
}

// MarkInactive removes instanceName from the active instances in the state file.
func MarkInactive(statePath, instanceName string) error {
	s, err := LoadState(statePath)
	if err != nil {
		return err
	}
	delete(s.Instances, instanceName)
	return SaveState(statePath, s)
}

// SaveTheme persists the chosen theme name to the state file.
func SaveTheme(statePath, themeName string) error {
	s, err := LoadState(statePath)
	if err != nil {
		return err
	}
	s.Theme = themeName
	return SaveState(statePath, s)
}

// SetCurrentInstance persists the currently-selected instance to the state file.
func SetCurrentInstance(statePath, instanceName string) error {
	s, err := LoadState(statePath)
	if err != nil {
		return err
	}
	s.CurrentInstance = instanceName
	return SaveState(statePath, s)
}

// ── Path helpers ──────────────────────────────────────────────────────────────

func tuiDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".tui")
}

// DefaultConfigPath returns the first config.yaml found: local then ~/.tui/.
func DefaultConfigPath() string {
	if _, err := os.Stat("config.yaml"); err == nil {
		return "config.yaml"
	}
	return filepath.Join(tuiDir(), "config.yaml")
}

// DefaultStatePath returns ~/.tui/state.json.
func DefaultStatePath() string {
	return filepath.Join(tuiDir(), "state.json")
}
