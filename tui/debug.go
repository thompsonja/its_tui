package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/thompsonja/its_tui/step"
)

// copyToClipboard writes text to the system clipboard by piping to the first
// available clipboard tool: wl-copy (Wayland), xclip, xsel (X11), pbcopy (macOS).
func copyToClipboard(text string) error {
	tools := [][]string{
		{"wl-copy"},
		{"xclip", "-selection", "clipboard"},
		{"xsel", "--clipboard", "--input"},
		{"pbcopy"},
	}
	for _, args := range tools {
		path, err := exec.LookPath(args[0])
		if err != nil {
			continue
		}
		cmd := exec.Command(path, args[1:]...)
		cmd.Stdin = strings.NewReader(text)
		if err := cmd.Run(); err == nil {
			return nil
		}
	}
	return fmt.Errorf("no clipboard tool found (install xclip, xsel, or wl-clipboard)")
}

// debugRuntime returns a human-readable runtime name for a skaffold portName.
func debugRuntime(portName string) string {
	switch portName {
	case "dlv":
		return "dlv/Go"
	case "jvm":
		return "jvm/Java"
	case "ptvsd", "debugpy":
		return "debugpy/Python"
	case "node", "nodejs":
		return "node/Node.js"
	default:
		if portName != "" {
			return portName
		}
		return "unknown"
	}
}

// vscodeLaunchConfig returns the lines of a VSCode launch configuration object
// (without the surrounding braces) for the given debug port.
func vscodeLaunchConfig(p step.DebugPortMsg, addr string) []string {
	name := p.ResourceName
	if name == "" {
		name = fmt.Sprintf("port-%d", p.LocalPort)
	}
	switch p.PortName {
	case "dlv":
		return []string{
			`{`,
			fmt.Sprintf(`  "name": "Attach %s",`, name),
			`  "type": "go",`,
			`  "request": "attach",`,
			`  "mode": "remote",`,
			fmt.Sprintf(`  "port": %d,`, p.LocalPort),
			fmt.Sprintf(`  "host": "%s"`, addr),
		}
	case "jvm":
		return []string{
			`{`,
			fmt.Sprintf(`  "name": "Attach %s",`, name),
			`  "type": "java",`,
			`  "request": "attach",`,
			fmt.Sprintf(`  "hostName": "%s",`, addr),
			fmt.Sprintf(`  "port": %d`, p.LocalPort),
		}
	case "ptvsd", "debugpy":
		return []string{
			`{`,
			fmt.Sprintf(`  "name": "Attach %s",`, name),
			`  "type": "python",`,
			`  "request": "attach",`,
			`  "connect": {`,
			fmt.Sprintf(`    "host": "%s",`, addr),
			fmt.Sprintf(`    "port": %d`, p.LocalPort),
			`  }`,
		}
	case "node", "nodejs":
		return []string{
			`{`,
			fmt.Sprintf(`  "name": "Attach %s",`, name),
			`  "type": "node",`,
			`  "request": "attach",`,
			fmt.Sprintf(`  "address": "%s",`, addr),
			fmt.Sprintf(`  "port": %d,`, p.LocalPort),
			`  "localRoot": "${workspaceFolder}",`,
			`  "remoteRoot": "/app"`,
		}
	default:
		return []string{
			`{`,
			fmt.Sprintf(`  "name": "Attach %s",`, name),
			fmt.Sprintf(`  "port": %d,`, p.LocalPort),
			fmt.Sprintf(`  "address": "%s"`, addr),
		}
	}
}

// vscodeLaunchEntryMap builds a launch configuration map for the given debug port.
// name is the full configuration name (including any prefix).
func vscodeLaunchEntryMap(name string, p step.DebugPortMsg, addr string) map[string]interface{} {
	switch p.PortName {
	case "dlv":
		return map[string]interface{}{
			"name":    name,
			"type":    "go",
			"request": "attach",
			"mode":    "remote",
			"port":    p.LocalPort,
			"host":    addr,
		}
	case "jvm":
		return map[string]interface{}{
			"name":     name,
			"type":     "java",
			"request":  "attach",
			"hostName": addr,
			"port":     p.LocalPort,
		}
	case "ptvsd", "debugpy":
		return map[string]interface{}{
			"name":    name,
			"type":    "python",
			"request": "attach",
			"connect": map[string]interface{}{
				"host": addr,
				"port": p.LocalPort,
			},
		}
	case "node", "nodejs":
		return map[string]interface{}{
			"name":       name,
			"type":       "node",
			"request":    "attach",
			"address":    addr,
			"port":       p.LocalPort,
			"localRoot":  "${workspaceFolder}",
			"remoteRoot": "/app",
		}
	default:
		return map[string]interface{}{
			"name":    name,
			"port":    p.LocalPort,
			"address": addr,
		}
	}
}

// tuiLaunchPrefix returns the name prefix used to identify TUI-managed launch
// configurations for the given instance. All entries written by the TUI begin
// with this prefix so they can be removed cleanly on stop.
func tuiLaunchPrefix(instanceName string) string {
	return "its_tui_" + instanceName + " "
}

// updateVSCodeLaunchJSON writes TUI-managed debug configurations into the
// .vscode/launch.json file inside workspaceDir, replacing any previously
// written entries for this instance. If ports is empty the TUI-managed entries
// are removed and nothing new is added (equivalent to removeVSCodeLaunchConfigs).
//
// The function is a no-op when:
//   - workspaceDir is empty
//   - a .vscode directory does not exist at workspaceDir
func updateVSCodeLaunchJSON(workspaceDir, instanceName string, ports []step.DebugPortMsg) error {
	if workspaceDir == "" {
		return nil
	}
	vscodePath := filepath.Join(workspaceDir, ".vscode")
	info, err := os.Stat(vscodePath)
	if err != nil || !info.IsDir() {
		return nil // .vscode doesn't exist — skip silently
	}

	launchPath := filepath.Join(vscodePath, "launch.json")
	prefix := tuiLaunchPrefix(instanceName)

	// Read the existing file or start with a minimal skeleton.
	data := map[string]interface{}{
		"version": "0.2.0",
	}
	if raw, err := os.ReadFile(launchPath); err == nil {
		var parsed map[string]interface{}
		if json.Unmarshal(raw, &parsed) == nil {
			data = parsed
		}
	}

	// Rebuild configurations, stripping out any previous TUI-managed entries.
	var configs []interface{}
	if cfgs, ok := data["configurations"].([]interface{}); ok {
		for _, c := range cfgs {
			if m, ok := c.(map[string]interface{}); ok {
				if name, _ := m["name"].(string); strings.HasPrefix(name, prefix) {
					continue
				}
				configs = append(configs, c)
			}
		}
	}

	// Append new TUI-managed configurations.
	for _, p := range ports {
		addr := p.Address
		if addr == "" {
			addr = "127.0.0.1"
		}
		resName := p.ResourceName
		if resName == "" {
			resName = fmt.Sprintf("port-%d", p.LocalPort)
		}
		configs = append(configs, vscodeLaunchEntryMap(prefix+"Attach "+resName, p, addr))
	}

	if len(configs) > 0 {
		data["configurations"] = configs
	} else {
		delete(data, "configurations")
	}

	// Rebuild compounds, stripping out previous TUI-managed ones.
	var compounds []interface{}
	if cpds, ok := data["compounds"].([]interface{}); ok {
		for _, c := range cpds {
			if m, ok := c.(map[string]interface{}); ok {
				if name, _ := m["name"].(string); strings.HasPrefix(name, prefix) {
					continue
				}
				compounds = append(compounds, c)
			}
		}
	}

	// Add a compound "Attach All" when there are multiple debug ports.
	if len(ports) > 1 {
		var names []interface{}
		for _, p := range ports {
			resName := p.ResourceName
			if resName == "" {
				resName = fmt.Sprintf("port-%d", p.LocalPort)
			}
			names = append(names, prefix+"Attach "+resName)
		}
		compounds = append(compounds, map[string]interface{}{
			"name":           prefix + "Attach All",
			"configurations": names,
		})
	}

	if len(compounds) > 0 {
		data["compounds"] = compounds
	} else {
		delete(data, "compounds")
	}

	out, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(launchPath, append(out, '\n'), 0644)
}

// removeVSCodeLaunchConfigs removes all TUI-managed launch configurations for
// instanceName from the .vscode/launch.json inside workspaceDir.
func removeVSCodeLaunchConfigs(workspaceDir, instanceName string) error {
	return updateVSCodeLaunchJSON(workspaceDir, instanceName, nil)
}
