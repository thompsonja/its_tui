package builtins

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	"github.com/thompsonja/its_tui/config"
	"github.com/thompsonja/its_tui/step"
)

// MfeLogPath returns the per-instance log file written by the MFE process.
func MfeLogPath(instanceName string) string {
	if instanceName == "" {
		return ""
	}
	return filepath.Join(config.GetLogDir(), fmt.Sprintf("mfe_%s.log", instanceName))
}

// MFECommand describes how to run a micro-frontend.
type MFECommand struct {
	Cmd  string            // executable name
	Args []string          // arguments
	Dir  string            // working directory
	Env  map[string]string // extra environment variables (merged with os.Environ)
}

// MFEStep runs a micro-frontend command and streams output to the MFE panel.
// The process is placed in its own process group so SIGTERM reaches all
// child processes (e.g. node workers spawned by npm).
type MFEStep struct {
	Cmd  MFECommand
	pgid int      // set by Start; used by Stop
	send func(any) // injected sender; falls back to global Send
}

// SetSender injects a message sender for testing. Falls back to the global Send.
func (s *MFEStep) SetSender(fn func(any)) { s.send = fn }

func (s *MFEStep) sender() func(any) {
	if s.send != nil {
		return s.send
	}
	return step.Send
}

func (s *MFEStep) ID() string                 { return "mfe" }
func (s *MFEStep) LogPath(name string) string { return MfeLogPath(name) }

// Start launches the MFE command and returns once the process is running.
// The process runs in the background and is killed when ctx is cancelled.
func (s *MFEStep) Start(ctx context.Context, instanceName string) error {
	lf, err := os.Create(MfeLogPath(instanceName))
	if err != nil {
		return fmt.Errorf("log create: %w", err)
	}

	cmd := exec.Command(s.Cmd.Cmd, s.Cmd.Args...)
	cmd.Dir = s.Cmd.Dir
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if len(s.Cmd.Env) > 0 {
		cmd.Env = os.Environ()
		for k, v := range s.Cmd.Env {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
	}
	cmd.Stdout = lf
	cmd.Stderr = lf

	if err := cmd.Start(); err != nil {
		lf.Close()
		return err
	}
	s.pgid = cmd.Process.Pid
	s.sender()(step.PIDMsg{PID: cmd.Process.Pid})

	// Kill the process group when the instance context is cancelled.
	go func() {
		<-ctx.Done()
		_ = syscall.Kill(-s.pgid, syscall.SIGTERM)
	}()

	// Wait for the process and write a final log line.
	go func() {
		defer lf.Close()
		if err := cmd.Wait(); err != nil {
			if ctx.Err() != nil {
				lf.WriteString("[mfe stopped]\n") //nolint:errcheck
				return
			}
			lf.WriteString(fmt.Sprintf("[mfe exited: %v]\n", err)) //nolint:errcheck
		} else {
			lf.WriteString("[mfe exited cleanly]\n") //nolint:errcheck
		}
	}()

	return nil
}

// Stop sends SIGTERM to the MFE process group.
func (s *MFEStep) Stop(_ context.Context, _ string) error {
	step.KillProcessGroup(s.pgid)
	return nil
}
