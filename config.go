package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

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
	Path string `yaml:"path"` // path to package.json directory
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

// ── Component catalogue ───────────────────────────────────────────────────────

// Component is a named deployable unit within a system.
type Component struct {
	Name string `json:"name"`
}

// System groups a set of components under a shared name.
type System struct {
	Name       string      `json:"name"`
	Components []Component `json:"components"`
}

// ComponentsFile is the top-level structure of a components JSON file.
// Use LoadComponents to parse it.
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

// mfeLogPath returns the per-instance log file written by the MFE process.
func mfeLogPath(instanceName string) string {
	if instanceName == "" {
		return ""
	}
	return fmt.Sprintf("/tmp/mfe_%s.log", instanceName)
}
