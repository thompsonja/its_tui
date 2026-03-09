package config

import (
	"encoding/json"
	"fmt"
	"os"
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
	Path     string   `yaml:"path"`               // path to skaffold.yaml
	Profiles []string `yaml:"profiles,omitempty"` // skaffold profiles to activate
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

