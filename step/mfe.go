package step

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"github.com/thompsonja/its_tui/config"
)

// MFEStep runs a micro-frontend command and streams output to the MFE panel.
// The process is placed in its own process group so SIGTERM reaches all
// child processes (e.g. node workers spawned by npm).
type MFEStep struct {
	Cmd  MFECommand
	pgid int // set by Start; used by Stop
}

func (s *MFEStep) ID() string             { return "mfe" }
func (s *MFEStep) LogPath(name string) string { return config.MfeLogPath(name) }

// Start launches the MFE command and returns once the process is running.
// The process runs in the background and is killed when ctx is cancelled.
func (s *MFEStep) Start(ctx context.Context, instanceName string) error {
	lf, err := os.Create(config.MfeLogPath(instanceName))
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
	Send(PIDMsg{PID: cmd.Process.Pid})

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
	KillProcessGroup(s.pgid)
	return nil
}
