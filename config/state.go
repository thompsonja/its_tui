package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// stateMu protects concurrent access to the state file.
// All LoadState and SaveState operations must hold this lock.
var stateMu sync.Mutex

// State tracks runtime data across TUI sessions.
type State struct {
	Theme          string         `json:"theme,omitempty"`
	CommandHistory []string       `json:"command_history,omitempty"`
	Instance       *InstanceState `json:"instance,omitempty"`
}

// StepStatus represents the lifecycle state of a step.
type StepStatus string

const (
	StepStatusPending   StepStatus = "pending"   // waiting for dependencies
	StepStatusRunning   StepStatus = "running"   // Start() called
	StepStatusCompleted StepStatus = "completed" // Start() returned nil
	StepStatusFailed    StepStatus = "failed"    // Start() returned error
	StepStatusSkipped   StepStatus = "skipped"   // dependency failed
)

// StepState tracks the execution state of a single step.
type StepState struct {
	ID          string     `json:"id"`
	Status      StepStatus `json:"status"`
	StartedAt   string     `json:"started_at,omitempty"`   // RFC3339
	CompletedAt string     `json:"completed_at,omitempty"` // RFC3339
	Error       string     `json:"error,omitempty"`        // error message
}

// InstanceState holds everything about the currently-running instance.
// It is written incrementally: selections are saved at wizard submission,
// StartedAt is stamped when the cluster is healthy, and MFEPGID is set when
// the MFE process starts. Instance is nil when nothing is running.
type InstanceState struct {
	StartedAt      string                       `json:"started_at,omitempty"`
	ReadyAt        string                       `json:"ready_at,omitempty"`            // timestamp when all steps completed
	MFEPGID        int                          `json:"mfe_pgid,omitempty"`
	StringValues   map[string]string            `json:"string_values,omitempty"`
	SliceValues    map[string][]string          `json:"slice_values,omitempty"`
	DebugPorts     []DebugPort                  `json:"debug_ports,omitempty"`
	ForwardedPorts []DebugPort                  `json:"forwarded_ports,omitempty"`
	StepStates     map[string]StepState         `json:"step_states,omitempty"`         // step execution state
	StepData       map[string]map[string]string `json:"step_data,omitempty"`           // per-step key-value storage
	PanelTabs      [3]int                       `json:"panel_tabs,omitempty"`          // active tab index for each panel (TopLeft, TopRight, BottomRight)
}

// DebugPort records one forwarded debug port from skaffold debug.
// It mirrors step.DebugPortMsg but lives in config to avoid a circular import.
type DebugPort struct {
	LocalPort    int    `json:"local_port"`
	RemotePort   int    `json:"remote_port,omitempty"`
	ResourceName string `json:"resource_name,omitempty"`
	PortName     string `json:"port_name,omitempty"`
	Address      string `json:"address,omitempty"`
}

const maxHistoryLen = 200

// AppendCommandHistory adds line to the persisted command history, keeping the
// last maxHistoryLen entries.
func AppendCommandHistory(statePath, line string) error {
	stateMu.Lock()
	defer stateMu.Unlock()

	s, err := loadStateUnsafe(statePath)
	if err != nil {
		return err
	}
	s.CommandHistory = append(s.CommandHistory, line)
	if len(s.CommandHistory) > maxHistoryLen {
		s.CommandHistory = s.CommandHistory[len(s.CommandHistory)-maxHistoryLen:]
	}
	return saveStateUnsafe(statePath, s)
}

// LoadState reads the state JSON file with lock protection.
// A missing file returns an empty State (not an error).
func LoadState(path string) (State, error) {
	stateMu.Lock()
	defer stateMu.Unlock()
	return loadStateUnsafe(path)
}

// loadStateUnsafe reads the state JSON file without acquiring the lock.
// Caller must hold stateMu.
func loadStateUnsafe(path string) (State, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return State{}, nil
		}
		return State{}, fmt.Errorf("reading state %s: %w", path, err)
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return State{}, fmt.Errorf("parsing state %s: %w", path, err)
	}
	return s, nil
}

// SaveState atomically writes the state JSON file with lock protection, creating parent dirs as needed.
func SaveState(path string, s State) error {
	stateMu.Lock()
	defer stateMu.Unlock()
	return saveStateUnsafe(path, s)
}

// saveStateUnsafe atomically writes the state JSON file without acquiring the lock.
// Caller must hold stateMu.
func saveStateUnsafe(path string, s State) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating state dir: %w", err)
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// SaveInstanceState writes the instance selections to state.Instance before
// the cluster is started. StartedAt is left empty until MarkActive is called.
func SaveInstanceState(statePath string, inst InstanceState) error {
	stateMu.Lock()
	defer stateMu.Unlock()

	s, err := loadStateUnsafe(statePath)
	if err != nil {
		return err
	}
	s.Instance = &inst
	return saveStateUnsafe(statePath, s)
}

// MarkActive stamps state.Instance.StartedAt with the current time.
// If state.Instance is nil (e.g. file-wizard path that skipped SaveInstanceState)
// it creates a new InstanceState first.
func MarkActive(statePath string) error {
	stateMu.Lock()
	defer stateMu.Unlock()

	s, err := loadStateUnsafe(statePath)
	if err != nil {
		return err
	}
	if s.Instance == nil {
		s.Instance = &InstanceState{}
	}
	s.Instance.StartedAt = time.Now().UTC().Format(time.RFC3339)
	return saveStateUnsafe(statePath, s)
}

// MarkInactive clears the running instance from state.
func MarkInactive(statePath string) error {
	stateMu.Lock()
	defer stateMu.Unlock()

	s, err := loadStateUnsafe(statePath)
	if err != nil {
		return err
	}
	s.Instance = nil
	return saveStateUnsafe(statePath, s)
}

// SaveMFEPGID persists the MFE process group ID into the running instance state.
func SaveMFEPGID(statePath string, pgid int) error {
	stateMu.Lock()
	defer stateMu.Unlock()

	s, err := loadStateUnsafe(statePath)
	if err != nil {
		return err
	}
	if s.Instance == nil {
		s.Instance = &InstanceState{}
	}
	s.Instance.MFEPGID = pgid
	return saveStateUnsafe(statePath, s)
}

// SaveDebugPorts persists the current debug port list into the running instance state.
func SaveDebugPorts(statePath string, ports []DebugPort) error {
	stateMu.Lock()
	defer stateMu.Unlock()

	s, err := loadStateUnsafe(statePath)
	if err != nil {
		return err
	}
	if s.Instance == nil {
		s.Instance = &InstanceState{}
	}
	s.Instance.DebugPorts = ports
	return saveStateUnsafe(statePath, s)
}

// SavePorts persists both forwarded service ports and debug ports into the running instance state.
func SavePorts(statePath string, fwd, dbg []DebugPort) error {
	stateMu.Lock()
	defer stateMu.Unlock()

	s, err := loadStateUnsafe(statePath)
	if err != nil {
		return err
	}
	if s.Instance == nil {
		s.Instance = &InstanceState{}
	}
	s.Instance.ForwardedPorts = fwd
	s.Instance.DebugPorts = dbg
	return saveStateUnsafe(statePath, s)
}

// SaveTheme persists the chosen theme name to the state file.
func SaveTheme(statePath, themeName string) error {
	stateMu.Lock()
	defer stateMu.Unlock()

	s, err := loadStateUnsafe(statePath)
	if err != nil {
		return err
	}
	s.Theme = themeName
	return saveStateUnsafe(statePath, s)
}

// UpdateStepState atomically updates the status of a single step in the state file.
// It initializes the StepStates map if needed and updates timestamps based on status transitions.
func UpdateStepState(statePath, stepID string, status StepStatus, err error) error {
	stateMu.Lock()
	defer stateMu.Unlock()

	s, loadErr := loadStateUnsafe(statePath)
	if loadErr != nil || s.Instance == nil {
		return loadErr
	}
	if s.Instance.StepStates == nil {
		s.Instance.StepStates = make(map[string]StepState)
	}

	ss := s.Instance.StepStates[stepID]
	ss.ID = stepID
	ss.Status = status

	now := time.Now().UTC().Format(time.RFC3339)
	switch status {
	case StepStatusRunning:
		if ss.StartedAt == "" {
			ss.StartedAt = now
		}
	case StepStatusCompleted, StepStatusFailed, StepStatusSkipped:
		if ss.CompletedAt == "" {
			ss.CompletedAt = now
		}
		if err != nil {
			ss.Error = err.Error()
		}
	}

	s.Instance.StepStates[stepID] = ss
	return saveStateUnsafe(statePath, s)
}

// SaveStepData atomically saves a key-value pair for a specific step.
// Steps can use this to persist configuration or state across TUI sessions.
// Returns an error if there's no active instance or if the save fails.
func SaveStepData(statePath, stepID, key, value string) error {
	stateMu.Lock()
	defer stateMu.Unlock()

	s, err := loadStateUnsafe(statePath)
	if err != nil || s.Instance == nil {
		return err
	}
	if s.Instance.StepData == nil {
		s.Instance.StepData = make(map[string]map[string]string)
	}
	if s.Instance.StepData[stepID] == nil {
		s.Instance.StepData[stepID] = make(map[string]string)
	}
	s.Instance.StepData[stepID][key] = value
	return saveStateUnsafe(statePath, s)
}

// LoadStepData retrieves a saved value for a step's key.
// Returns the value and true if found, or empty string and false if not found.
func LoadStepData(statePath, stepID, key string) (string, bool) {
	stateMu.Lock()
	defer stateMu.Unlock()

	s, err := loadStateUnsafe(statePath)
	if err != nil || s.Instance == nil {
		return "", false
	}
	if s.Instance.StepData == nil {
		return "", false
	}
	stepData, ok := s.Instance.StepData[stepID]
	if !ok {
		return "", false
	}
	value, ok := stepData[key]
	return value, ok
}

// GetStepData returns all saved key-value pairs for a specific step.
// Returns an empty map if the step has no saved data.
func GetStepData(statePath, stepID string) map[string]string {
	stateMu.Lock()
	defer stateMu.Unlock()

	s, err := loadStateUnsafe(statePath)
	if err != nil || s.Instance == nil {
		return map[string]string{}
	}
	if s.Instance.StepData == nil {
		return map[string]string{}
	}
	if stepData, ok := s.Instance.StepData[stepID]; ok {
		// Return a copy to prevent external mutations
		result := make(map[string]string, len(stepData))
		for k, v := range stepData {
			result[k] = v
		}
		return result
	}
	return map[string]string{}
}

// SavePanelTabs persists the active tab index for each panel.
func SavePanelTabs(statePath string, tabs [3]int) error {
	stateMu.Lock()
	defer stateMu.Unlock()

	s, err := loadStateUnsafe(statePath)
	if err != nil || s.Instance == nil {
		return err
	}
	s.Instance.PanelTabs = tabs
	return saveStateUnsafe(statePath, s)
}

// MarkReady stamps state.Instance.ReadyAt with the current time to indicate
// that all steps have completed their startup.
func MarkReady(statePath string) error {
	stateMu.Lock()
	defer stateMu.Unlock()

	s, err := loadStateUnsafe(statePath)
	if err != nil || s.Instance == nil {
		return err
	}
	// Only set ReadyAt once
	if s.Instance.ReadyAt == "" {
		s.Instance.ReadyAt = time.Now().UTC().Format(time.RFC3339)
	}
	return saveStateUnsafe(statePath, s)
}
