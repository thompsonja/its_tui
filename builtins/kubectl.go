package builtins

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"time"

	"github.com/thompsonja/its_tui/config"
	"github.com/thompsonja/its_tui/step"
	"github.com/thompsonja/its_tui/tui"
)

// KubectlStep runs `kubectl get pods --watch` as a persistent process that watches pod changes.
// It saves the PID to state and can reattach on resume if the process is still running.
type KubectlStep struct {
	send      func(any)
	StatePath string
}

// SetSender injects a message sender for testing. Falls back to the global Send.
func (s *KubectlStep) SetSender(fn func(any)) { s.send = fn }

func (s *KubectlStep) sender() func(any) {
	if s.send != nil {
		return s.send
	}
	return step.Send
}

func (s *KubectlStep) ID() string      { return "kubectl" }
func (s *KubectlStep) LogPath(_ string) string { return "" }

// Resume checks if the saved kubectl process is still running.
// Returns nil if running (continuing to use existing process), or error to trigger restart via Start().
func (s *KubectlStep) Resume(ctx context.Context, instanceName string) error {
	if s.StatePath == "" {
		s.StatePath = config.DefaultStatePath()
	}

	pidStr, ok := config.LoadStepData(s.StatePath, s.ID(), "pid")
	if !ok {
		// No saved PID, need to start fresh
		return fmt.Errorf("no saved pid")
	}

	pid, err := strconv.Atoi(pidStr)
	if err != nil {
		return fmt.Errorf("invalid pid: %w", err)
	}

	// Check if process is still running
	process, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("process not found: %w", err)
	}

	// Send signal 0 to test if process exists
	if err := process.Signal(syscall.Signal(0)); err != nil {
		return fmt.Errorf("process not running: %w", err)
	}

	// Process is still running - restart the polling loop to show updates
	go s.pollLoop(ctx)
	return nil
}

// Start launches a background polling loop that updates pod status.
func (s *KubectlStep) Start(ctx context.Context, instanceName string) error {
	if s.StatePath == "" {
		s.StatePath = config.DefaultStatePath()
	}

	// Save a marker PID (our own process) to indicate kubectl is active
	if s.StatePath != "" {
		if err := config.SaveStepData(s.StatePath, s.ID(), "pid", strconv.Itoa(os.Getpid())); err != nil {
			step.DebugLog("kubectl: SaveStepData pid: %v", err)
		}
	}

	go s.pollLoop(ctx)
	return nil
}

// pollLoop runs kubectl get pods periodically and sends the output as a table.
func (s *KubectlStep) pollLoop(ctx context.Context) {
	s.poll()
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.poll()
		}
	}
}

func (s *KubectlStep) poll() {
	out, err := exec.Command("kubectl", "get", "pods").CombinedOutput()
	lines := step.SplitLines(string(out))
	if err != nil && len(lines) == 0 {
		lines = []string{"Waiting for cluster to be ready..."}
	}
	s.sender()(step.SetMsg{ID: s.ID(), Content: lines})
}

// KubectlTemplate returns a StepTemplate for the kubectl pod watcher.
// It has no wizard fields: it starts automatically after minikube is ready
// and auto-activates its panel.
func KubectlTemplate() tui.StepTemplate {
	return tui.StepTemplate{
		ID:           "kubectl",
		Panel:        tui.PanelTopLeft,
		Label:        "kubectl",
		WaitFor:      []string{"minikube"},
		AutoActivate: true,
		Hidden:       true,
		Build: func(v tui.WizardValues) (step.Step, error) {
			return &KubectlStep{StatePath: config.DefaultStatePath()}, nil
		},
	}
}
