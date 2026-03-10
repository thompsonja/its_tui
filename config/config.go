package config

import (
	"encoding/json"
	"fmt"
	"os"
)

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

