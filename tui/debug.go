package tui

import (
	"fmt"
	"os/exec"
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
