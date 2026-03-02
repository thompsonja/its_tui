package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"gopkg.in/yaml.v3"
)

// ── YAML config ───────────────────────────────────────────────────────────────

// Configs maps instance name → InstanceConfig (the top-level YAML keys).
type Configs map[string]InstanceConfig

// InstanceConfig holds the per-instance configuration from the YAML file.
type InstanceConfig struct {
	Minikube MinikubeConfig `yaml:"minikube"`
	Skaffold SkaffoldConfig `yaml:"skaffold"`
	MFE      MFEConfig      `yaml:"mfe"`
}

// MinikubeConfig holds minikube start parameters.
type MinikubeConfig struct {
	CPU int    `yaml:"cpu"` // number of vCPUs
	RAM string `yaml:"ram"` // memory passed to --memory, e.g. "4Gi" or "4096"
}

// SkaffoldConfig holds the skaffold pipeline configuration.
type SkaffoldConfig struct {
	Path string `yaml:"path"` // path to skaffold.yaml
}

// MFEConfig holds the micro-frontend configuration.
type MFEConfig struct {
	Path string `yaml:"path"` // path to package.json
}

// validName matches instance names: alphanumeric, hyphens, underscores.
var validName = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// LoadConfigs parses the YAML file at path.
// A missing file returns an empty Configs (not an error) so the TUI starts without one.
func LoadConfigs(path string) (Configs, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Configs{}, nil
		}
		return nil, fmt.Errorf("reading config %s: %w", path, err)
	}
	var cfg Configs
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config %s: %w", path, err)
	}
	if cfg == nil {
		cfg = Configs{}
	}
	for name := range cfg {
		if !validName.MatchString(name) {
			return nil, fmt.Errorf("config: instance name %q must be alphanumeric/hyphen/underscore", name)
		}
	}
	return cfg, nil
}

// ── JSON state ────────────────────────────────────────────────────────────────

// State tracks which instances have been started.
type State struct {
	Instances       map[string]ActiveInstance `json:"instances"`
	CurrentInstance string                    `json:"current_instance,omitempty"`
	CommandHistory  []string                  `json:"command_history,omitempty"`
	Theme           string                    `json:"theme,omitempty"`
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

// ActiveInstance records runtime state for a started instance.
type ActiveInstance struct {
	StartedAt string `json:"started_at"`
	MFEPGID   int    `json:"mfe_pgid,omitempty"` // process group ID of the npm process; 0 if not running
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
// Upserts the entry so it works regardless of whether MarkActive has run yet.
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

// ── Component catalogue ───────────────────────────────────────────────────────

// ComponentEntry is one named component inside a system.
type ComponentEntry struct {
	Name string `json:"name"`
}

// System groups a set of components under a shared name.
type System struct {
	Name       string           `json:"name"`
	Components []ComponentEntry `json:"components"`
}

// ComponentsFile is the top-level structure of sample/components.json.
type ComponentsFile struct {
	Systems []System `json:"systems"`
}

// LoadComponents parses the JSON file at path.
// A missing file returns an empty ComponentsFile (not an error).
func LoadComponents(path string) (ComponentsFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return ComponentsFile{}, nil
		}
		return ComponentsFile{}, fmt.Errorf("reading components %s: %w", path, err)
	}
	var cf ComponentsFile
	if err := json.Unmarshal(data, &cf); err != nil {
		return ComponentsFile{}, fmt.Errorf("parsing components %s: %w", path, err)
	}
	return cf, nil
}

// ── Custom instance config ────────────────────────────────────────────────────

// CustomInstanceConfig holds the selections made in the custom-instance wizard.
type CustomInstanceConfig struct {
	Instance   string   `json:"instance"`
	CPU        string   `json:"cpu"`
	RAM        string   `json:"ram"`
	Components []string `json:"components"`
	MFE        string   `json:"mfe,omitempty"`
	Mode       string   `json:"mode"`
}

// WriteCustomConfig saves selections alongside the state file as
// <stateDir>/<instanceName>_selections.json.
func WriteCustomConfig(statePath, instanceName string, cfg CustomInstanceConfig) error {
	dir := filepath.Dir(statePath)
	path := filepath.Join(dir, instanceName+"_selections.json")
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
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

// skaffoldLogPath returns the per-instance log file written by skaffold dev.
func skaffoldLogPath(instanceName string) string {
	if instanceName == "" {
		return ""
	}
	return fmt.Sprintf("/tmp/skaffold_%s.log", instanceName)
}

// minikubeLogPath returns the per-instance log file written by minikube start.
func minikubeLogPath(instanceName string) string {
	if instanceName == "" {
		return ""
	}
	return fmt.Sprintf("/tmp/minikube_%s.log", instanceName)
}
