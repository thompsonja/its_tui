package builtins

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/thompsonja/its_tui/config"
	"github.com/thompsonja/its_tui/step"
	"github.com/thompsonja/its_tui/tui"
)

// MinikubeLogPath returns the per-instance log file written by minikube start.
func MinikubeLogPath(instanceName string) string {
	if instanceName == "" {
		return ""
	}
	return filepath.Join(config.GetLogDir(), fmt.Sprintf("minikube_%s.log", instanceName))
}

// MinikubeStep manages a minikube cluster: start and teardown via `minikube delete`.
type MinikubeStep struct {
	CPU  string
	RAM  string
	Args []string
}

func (s *MinikubeStep) ID() string                 { return "minikube" }
func (s *MinikubeStep) LogPath(name string) string { return MinikubeLogPath(name) }

// Start runs `minikube start` and blocks until the process exits.
// Context cancellation (e.g. instance switch) is not reported as an error.
func (s *MinikubeStep) Start(ctx context.Context, instanceName string) error {
	lf, err := os.Create(MinikubeLogPath(instanceName))
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
	step.StreamToPanel(ctx, s.ID(), "minikube", "delete")
	return nil
}

// MinikubeTemplate returns a StepTemplate for starting a minikube cluster.
// It contributes CPU and RAM selector fields to the wizard.
// Additional args can be passed to minikube start command via the args parameter.
func MinikubeTemplate(args ...string) tui.StepTemplate {
	return tui.StepTemplate{
		ID:    "minikube",
		Panel: tui.PanelTopLeft,
		Label: "Minikube",
		Fields: []tui.FieldSpec{
			{ID: "cpu", Label: "CPU", Kind: tui.FieldKindSelect, OptionsFunc: tui.StaticOptions("2", "4", "8", "16"), Default: 1},
			{ID: "ram", Label: "RAM", Kind: tui.FieldKindSelect, OptionsFunc: tui.StaticOptions("2g", "4g", "8g", "16g"), Default: 1},
		},
		Build: func(v tui.WizardValues) (step.Step, error) {
			cpu := v.String("cpu")
			if cpu == "" {
				cpu = "4"
			}
			ram := v.String("ram")
			if ram == "" {
				ram = "4g"
			}
			return &MinikubeStep{CPU: cpu, RAM: ram, Args: args}, nil
		},
	}
}
