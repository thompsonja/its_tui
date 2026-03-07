package tui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// MinikubeStep manages a minikube cluster: start, ready-check via kubectl,
// and teardown via `minikube delete`.
type MinikubeStep struct {
	CPU string
	RAM string
}

func (s *MinikubeStep) ID() string                    { return "minikube" }
func (s *MinikubeStep) LogPath(name string) string    { return minikubeLogPath(name) }
func (s *MinikubeStep) PanelLine(line string) tea.Msg { return minikubeLineMsg(line) }

// Start runs `minikube start` and blocks until the process exits.
// Context cancellation (e.g. instance switch) is not reported as an error.
func (s *MinikubeStep) Start(ctx context.Context, instanceName string) error {
	lf, err := os.Create(minikubeLogPath(instanceName))
	if err != nil {
		return fmt.Errorf("log create: %w", err)
	}
	defer lf.Close()

	cmd := exec.CommandContext(ctx, "minikube", "start", "--cpus", s.CPU, "--memory", s.RAM)
	cmd.Stdout = lf
	cmd.Stderr = lf
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil // cancelled — not a real error
		}
		return err
	}
	return nil
}

// Stop runs `minikube delete`, streaming output to the minikube panel.
func (s *MinikubeStep) Stop(ctx context.Context, _ string) error {
	streamToPanel(ctx,
		func(line string) tea.Msg { return minikubeLineMsg(line) },
		"minikube", "delete",
	)
	return nil
}

// IsReady polls kubectl get pods once. It sends the result to the minikube panel
// and returns true if the cluster responded successfully.
func (s *MinikubeStep) IsReady(_ context.Context, _ string) bool {
	lines, ok := kubectlGetPodsOnce()
	if ok {
		prog.Send(minikubeSetMsg(lines))
	}
	return ok
}

// ReadConfig reads CPU and RAM from cfg.Minikube, leaving existing values
// untouched when the config field is zero/empty.
func (s *MinikubeStep) ReadConfig(cfg InstanceConfig) {
	if cfg.Minikube.CPU > 0 {
		s.CPU = strconv.Itoa(cfg.Minikube.CPU)
	}
	if cfg.Minikube.RAM != "" {
		s.RAM = cfg.Minikube.RAM
	}
}

// WriteConfig writes CPU and RAM back into cfg.Minikube.
func (s *MinikubeStep) WriteConfig(cfg *InstanceConfig) {
	cpu, _ := strconv.Atoi(s.CPU)
	cfg.Minikube.CPU = cpu
	cfg.Minikube.RAM = s.RAM
}

// ── kubectl helpers ────────────────────────────────────────────────────────────

// watchKubectl polls `kubectl get pods` every 5 s and updates the minikube panel.
func watchKubectl(ctx context.Context, instanceName string) {
	if instanceName == "" {
		return
	}
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
// success, or (nil, false) if the command fails.
func kubectlGetPodsOnce() ([]string, bool) {
	out, err := exec.Command("kubectl", "get", "pods").CombinedOutput()
	if err != nil {
		return nil, false
	}
	return splitLines(string(out)), true
}
