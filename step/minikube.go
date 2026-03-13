package step

import (
	"context"
	"fmt"
	"github.com/thompsonja/its_tui/config"
	"os"
	"os/exec"
)

// MinikubeStep manages a minikube cluster: start and teardown via `minikube delete`.
type MinikubeStep struct {
	CPU  string
	RAM  string
	Args []string
}

func (s *MinikubeStep) ID() string                 { return "minikube" }
func (s *MinikubeStep) LogPath(name string) string { return config.MinikubeLogPath(name) }

// Start runs `minikube start` and blocks until the process exits.
// Context cancellation (e.g. instance switch) is not reported as an error.
func (s *MinikubeStep) Start(ctx context.Context, instanceName string) error {
	lf, err := os.Create(config.MinikubeLogPath(instanceName))
	if err != nil {
		return fmt.Errorf("log create: %w", err)
	}
	defer lf.Close()

	args := []string{"start", "--cpus", s.CPU, "--memory", s.RAM}
	args = append(args, s.Args...)
	cmd := exec.CommandContext(ctx, "minikube", args...)
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
