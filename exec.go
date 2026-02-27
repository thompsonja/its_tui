package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// ── kubectl watcher ───────────────────────────────────────────────────────────

// watchKubectl polls `kubectl get pods -A` every 5 s and replaces the minikube
// panel content with the current output. If no instance is selected the panel
// shows a single status line instead.
func watchKubectl(instanceName string) {
	if instanceName == "" {
		prog.Send(minikubeSetMsg{"No instance selected"})
		return
	}
	runKubectlGetPods() // immediate first run
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		runKubectlGetPods()
	}
}

func runKubectlGetPods() {
	out, err := exec.Command("kubectl", "get", "pods", "-A").CombinedOutput()
	lines := splitLines(string(out))
	if err != nil && len(lines) == 0 {
		lines = []string{fmt.Sprintf("error: %v", err)}
	}
	prog.Send(minikubeSetMsg(lines))
}

// ── Skaffold log watcher ──────────────────────────────────────────────────────

// watchSkaffoldLog tails path and streams each new line to the skaffold panel.
//
//   - If instanceName is empty the panel shows "No instance selected" and returns.
//   - If the file doesn't exist yet, a "not found" notice is shown first; tail -F
//     then waits for the file to be created before streaming begins.
//   - tail -F follows the file by name, surviving log rotation and recreation.
//   - -n 0 starts from the current end of the file so the TUI never reads the
//     entire existing log into memory — only new lines are ingested.
//
// Memory is further bounded by maxBufLines in the skaffold buffer: once that
// cap is hit, old lines are evicted from the front of the slice. The viewport
// holds exactly the buffered lines and renders a scrollable window into them,
// so scrollback depth == maxBufLines, not the full file.
//
// Run as: go watchSkaffoldLog("/tmp/skaffold.log", instance.Name)
func watchSkaffoldLog(path, instanceName string) {
	if instanceName == "" {
		prog.Send(skaffoldLineMsg("No instance selected"))
		return
	}
	if _, err := os.Stat(path); err != nil {
		prog.Send(skaffoldLineMsg(fmt.Sprintf("Skaffold log `%s` not found", path)))
	}
	// tail -F waits for the file to appear if it doesn't exist yet.
	// Filter out tail's own diagnostic lines so they don't clutter the panel.
	streamToPanel(func(line string) tea.Msg {
		if strings.HasPrefix(line, "tail: ") {
			return nil
		}
		return skaffoldLineMsg(line)
	}, "tail", "-F", "-n", "0", path)
}

// ── Core streaming primitive ─────────────────────────────────────────────────

// streamToPanel runs name+args as a subprocess. Each line from stdout/stderr
// is converted via factory and sent to the bubbletea program. Blocks until
// the process exits — call in a goroutine.
func streamToPanel(factory func(string) tea.Msg, name string, args ...string) {
	cmd := exec.Command(name, args...)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		prog.Send(factory(fmt.Sprintf("stdout pipe error: %v", err)))
		return
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		prog.Send(factory(fmt.Sprintf("stderr pipe error: %v", err)))
		return
	}

	if err := cmd.Start(); err != nil {
		prog.Send(factory(fmt.Sprintf("start error: %v", err)))
		return
	}

	// drain stdout in a separate goroutine; drain stderr in this one.
	// A nil return from factory means "skip this line".
	go func() {
		s := bufio.NewScanner(stdout)
		for s.Scan() {
			if msg := factory(s.Text()); msg != nil {
				prog.Send(msg)
			}
		}
	}()

	s := bufio.NewScanner(stderr)
	for s.Scan() {
		if msg := factory(s.Text()); msg != nil {
			prog.Send(msg)
		}
	}

	if err := cmd.Wait(); err != nil {
		prog.Send(factory(fmt.Sprintf("[exited: %v]", err)))
	} else {
		prog.Send(factory("[process exited cleanly]"))
	}
}

// splitLines splits a string into lines using a scanner (handles \r\n too).
func splitLines(s string) []string {
	var lines []string
	sc := bufio.NewScanner(strings.NewReader(s))
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	return lines
}
