package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// ── kubectl watcher ───────────────────────────────────────────────────────────

// watchKubectl polls `kubectl get pods` every 5 s and replaces the Minikube
// panel content. Cancelled via ctx when the instance switches.
func watchKubectl(ctx context.Context, instanceName string) {
	if instanceName == "" {
		prog.Send(minikubeSetMsg{"No instance selected"})
		return
	}
	prog.Send(minikubeSetMsg{"Waiting for cluster..."})
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			runKubectlGetPods()
		}
	}
}

func runKubectlGetPods() {
	out, err := exec.Command("kubectl", "get", "pods").CombinedOutput()
	lines := splitLines(string(out))
	if err != nil && len(lines) == 0 {
		lines = []string{"Waiting for cluster to be ready..."}
	}
	prog.Send(minikubeSetMsg(lines))
}

// kubectlGetPodsOnce runs kubectl get pods once and returns (lines, true) on
// success, or (nil, false) if the command fails. Used for the auto-switch trigger.
func kubectlGetPodsOnce() ([]string, bool) {
	out, err := exec.Command("kubectl", "get", "pods").CombinedOutput()
	if err != nil {
		return nil, false
	}
	return splitLines(string(out)), true
}

// ── Minikube log watcher + runner ─────────────────────────────────────────────

// watchMinikubeLog tails path and streams new lines to the Minikube log panel.
// Cancelled via ctx when the instance switches.
func watchMinikubeLog(ctx context.Context, path, instanceName string) {
	if instanceName == "" {
		prog.Send(minikubeLineMsg("No instance selected"))
		return
	}
	prog.Send(minikubeLineMsg("Waiting for minikube log..."))
	cmd := exec.CommandContext(ctx, "tail", "-F", "-n", "50", path)
	streamCmd(ctx, cmd, func(line string) tea.Msg {
		if strings.HasPrefix(line, "tail: ") {
			return nil // filter tail's own diagnostics
		}
		return minikubeLineMsg(line)
	})
}

// startMinikubeToLog runs `minikube start` and writes all output to the
// per-instance log file. Blocks until the command exits; returns a non-nil
// error if minikube failed (context cancellation is not reported as an error).
func startMinikubeToLog(instanceName, cpu, ram string) error {
	logPath := minikubeLogPath(instanceName)
	lf, err := os.Create(logPath)
	if err != nil {
		return fmt.Errorf("log create: %w", err)
	}
	defer lf.Close()

	cmd := exec.CommandContext(instanceCtx, "minikube", "start", "--cpus", cpu, "--memory", ram)
	cmd.Stdout = lf
	cmd.Stderr = lf
	if err := cmd.Run(); err != nil {
		if instanceCtx.Err() != nil {
			return nil // cancelled by instance switch — not a real error
		}
		return err
	}
	return nil
}

// ── Skaffold log watcher ──────────────────────────────────────────────────────

// watchSkaffoldLog tails path and streams new lines to the Skaffold panel.
// Cancelled via ctx when the instance switches.
func watchSkaffoldLog(ctx context.Context, path, instanceName string) {
	if instanceName == "" {
		prog.Send(skaffoldLineMsg("No instance selected"))
		return
	}
	prog.Send(skaffoldLineMsg("Waiting for skaffold..."))
	cmd := exec.CommandContext(ctx, "tail", "-F", "-n", "50", path)
	streamCmd(ctx, cmd, func(line string) tea.Msg {
		if strings.HasPrefix(line, "tail: ") {
			return nil // filter tail's own diagnostics
		}
		return skaffoldLineMsg(line)
	})
}

// ── Skaffold dev runner ───────────────────────────────────────────────────────

// startSkaffoldToLog runs `skaffold dev` and writes all output to the
// per-instance log file. watchSkaffoldLog then tails that file into the panel.
// Uses instanceCtx so it is cancelled when the instance switches.
func startSkaffoldToLog(instanceName, skaffoldPath, mode string) {
	if mode == "" {
		mode = "dev"
	}
	logPath := skaffoldLogPath(instanceName)
	lf, err := os.Create(logPath)
	if err != nil {
		prog.Send(commandLineMsg(fmt.Sprintf("skaffold log create error: %v", err)))
		return
	}
	defer lf.Close()

	absPath, err := filepath.Abs(skaffoldPath)
	if err != nil {
		absPath = skaffoldPath
	}
	cmd := exec.CommandContext(instanceCtx, "skaffold", mode, "--filename", absPath)
	cmd.Dir = filepath.Dir(absPath)
	cmd.Stdout = lf
	cmd.Stderr = lf
	if err := cmd.Start(); err != nil {
		prog.Send(commandLineMsg(fmt.Sprintf("skaffold start error: %v", err)))
		return
	}
	if err := cmd.Wait(); err != nil {
		if instanceCtx.Err() != nil {
			return // cancelled by instance switch — suppress the exit message
		}
		prog.Send(commandLineMsg(fmt.Sprintf("[skaffold exited: %v]", err)))
	} else {
		prog.Send(commandLineMsg("[skaffold exited cleanly]"))
	}
}

// ── MFE runner ────────────────────────────────────────────────────────────────

// startMFE runs `npm start` from the package.json directory, streaming output
// to the MFE panel. Uses instanceCtx so it is cancelled when the instance switches.
//
// npm spawns child node processes that survive a simple Process.Kill(), so we
// place the process in its own process group and send SIGTERM to the entire
// group on cancellation, killing all descendants.
func startMFE(packageJSONPath string) {
	dir := filepath.Dir(packageJSONPath)
	cmd := exec.Command("npm", "start")
	cmd.Dir = dir
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		prog.Send(mfeLineMsg(fmt.Sprintf("mfe pipe error: %v", err)))
		return
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		prog.Send(mfeLineMsg(fmt.Sprintf("mfe pipe error: %v", err)))
		return
	}
	if err := cmd.Start(); err != nil {
		prog.Send(mfeLineMsg(fmt.Sprintf("mfe start error: %v", err)))
		return
	}
	// Report the process group ID so it can be persisted to state.
	prog.Send(mfePIDMsg(cmd.Process.Pid))

	ctx := instanceCtx
	// Kill the entire process group when the instance context is cancelled.
	go func() {
		<-ctx.Done()
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
	}()

	go func() {
		s := bufio.NewScanner(stdout)
		for s.Scan() {
			prog.Send(mfeLineMsg(s.Text()))
		}
	}()
	s := bufio.NewScanner(stderr)
	for s.Scan() {
		prog.Send(mfeLineMsg(s.Text()))
	}

	if err := cmd.Wait(); err != nil {
		if ctx.Err() != nil {
			return // killed by instance switch — suppress the exit message
		}
		prog.Send(mfeLineMsg(fmt.Sprintf("[mfe exited: %v]", err)))
	} else {
		prog.Send(mfeLineMsg("[mfe exited cleanly]"))
	}
}

// killProcessGroup sends SIGTERM to the entire process group identified by pgid.
// Safe to call with pgid <= 0 (no-op).
func killProcessGroup(pgid int) {
	if pgid <= 0 {
		return
	}
	_ = syscall.Kill(-pgid, syscall.SIGTERM)
}

// ── Core streaming primitive ──────────────────────────────────────────────────

// streamToPanel builds a command from name+args and streams it via streamCmd.
func streamToPanel(ctx context.Context, factory func(string) tea.Msg, name string, args ...string) {
	streamCmd(ctx, exec.CommandContext(ctx, name, args...), factory)
}

// streamCmd drains stdout and stderr from cmd, sending each line through factory.
// A nil return from factory means "skip this line".
// Suppresses the exit message when the context was cancelled (e.g. instance switch).
func streamCmd(ctx context.Context, cmd *exec.Cmd, factory func(string) tea.Msg) {
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		if msg := factory(fmt.Sprintf("stdout pipe error: %v", err)); msg != nil {
			prog.Send(msg)
		}
		return
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		if msg := factory(fmt.Sprintf("stderr pipe error: %v", err)); msg != nil {
			prog.Send(msg)
		}
		return
	}
	if err := cmd.Start(); err != nil {
		if msg := factory(fmt.Sprintf("start error: %v", err)); msg != nil {
			prog.Send(msg)
		}
		return
	}

	// Drain stdout in a separate goroutine; drain stderr here.
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
		if ctx.Err() != nil {
			return // killed by context cancellation — don't report
		}
		if msg := factory(fmt.Sprintf("[exited: %v]", err)); msg != nil {
			prog.Send(msg)
		}
	} else {
		if msg := factory("[process exited cleanly]"); msg != nil {
			prog.Send(msg)
		}
	}
}

// splitLines splits s into lines using a scanner (handles \r\n too).
func splitLines(s string) []string {
	var lines []string
	sc := bufio.NewScanner(strings.NewReader(s))
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	return lines
}
