package tui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
)

// SkaffoldStep runs `skaffold <mode>` and streams output to the Skaffold panel.
// It depends on minikube being ready before Start is called.
type SkaffoldStep struct {
	Path string // path to skaffold.yaml
	Mode string // "dev", "run", or "debug"; defaults to "dev"
}

func (s *SkaffoldStep) ID() string                    { return "skaffold" }
func (s *SkaffoldStep) LogPath(name string) string    { return skaffoldLogPath(name) }
func (s *SkaffoldStep) PanelLine(line string) tea.Msg { return skaffoldLineMsg(line) }

// ReadConfig reads the skaffold.yaml path from cfg.Skaffold.
func (s *SkaffoldStep) ReadConfig(cfg InstanceConfig) {
	if cfg.Skaffold.Path != "" {
		s.Path = cfg.Skaffold.Path
	}
}

// WriteConfig writes the skaffold.yaml path back into cfg.Skaffold.
func (s *SkaffoldStep) WriteConfig(cfg *InstanceConfig) {
	cfg.Skaffold.Path = s.Path
}

// Start launches skaffold and returns once the process is running.
// Skaffold continues in the background; its exit is reported via commandLineMsg.
func (s *SkaffoldStep) Start(ctx context.Context, instanceName string) error {
	mode := s.Mode
	if mode == "" {
		mode = "dev"
	}

	logPath := skaffoldLogPath(instanceName)
	os.Remove(logPath) // clear previous log so tail -F starts from the beginning
	lf, err := os.Create(logPath)
	if err != nil {
		return fmt.Errorf("log create: %w", err)
	}

	absPath, err := filepath.Abs(s.Path)
	if err != nil {
		absPath = s.Path
	}

	cmd := exec.CommandContext(ctx, "skaffold", mode, "--filename", absPath)
	cmd.Dir = filepath.Dir(absPath)
	cmd.Stdout = lf
	cmd.Stderr = lf

	if err := cmd.Start(); err != nil {
		lf.Close()
		return err
	}

	// Report exit to the commands panel once the process finishes.
	go func() {
		defer lf.Close()
		if err := cmd.Wait(); err != nil {
			if ctx.Err() != nil {
				return // cancelled by instance switch — suppress
			}
			prog.Send(commandLineMsg(fmt.Sprintf("[skaffold exited: %v]", err)))
		} else {
			prog.Send(commandLineMsg("[skaffold exited cleanly]"))
		}
	}()

	return nil
}

// Stop is a no-op: skaffold is terminated when the instance context is cancelled.
func (s *SkaffoldStep) Stop(_ context.Context, _ string) error { return nil }

// IsReady returns true immediately — skaffold is considered ready as soon as
// the process has started.
func (s *SkaffoldStep) IsReady(_ context.Context, _ string) bool { return true }
