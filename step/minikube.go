package step

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"tui/config"
)

// MinikubeStep manages a minikube cluster: start and teardown via `minikube delete`.
type MinikubeStep struct {
	CPU string
	RAM string
}

func (s *MinikubeStep) ID() string             { return "minikube" }
func (s *MinikubeStep) LogPath(name string) string { return config.MinikubeLogPath(name) }

// Start runs `minikube start` and blocks until the process exits.
// Context cancellation (e.g. instance switch) is not reported as an error.
func (s *MinikubeStep) Start(ctx context.Context, instanceName string) error {
	lf, err := os.Create(config.MinikubeLogPath(instanceName))
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
	StreamToPanel(ctx, s.ID(), "minikube", "delete")
	return nil
}

// ReadConfig reads CPU and RAM from cfg.Minikube.
func (s *MinikubeStep) ReadConfig(cfg config.InstanceConfig) {
	if cfg.Minikube.CPU > 0 {
		s.CPU = strconv.Itoa(cfg.Minikube.CPU)
	}
	if cfg.Minikube.RAM != "" {
		s.RAM = cfg.Minikube.RAM
	}
}

// WriteConfig writes CPU and RAM back into cfg.Minikube.
func (s *MinikubeStep) WriteConfig(cfg *config.InstanceConfig) {
	cpu, _ := strconv.Atoi(s.CPU)
	cfg.Minikube.CPU = cpu
	cfg.Minikube.RAM = s.RAM
}
