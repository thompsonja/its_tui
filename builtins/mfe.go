package builtins

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/thompsonja/its_tui/config"
	"github.com/thompsonja/its_tui/step"
	"github.com/thompsonja/its_tui/tui"
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
	Cmd          string            // executable name
	Args         []string          // arguments
	Dir          string            // working directory
	Env          map[string]string // extra environment variables (merged with os.Environ)
	ReadyPattern *regexp.Regexp    // if set, Start blocks until output matches this pattern
	ReadyTimeout time.Duration     // timeout waiting for ReadyPattern; 0 defaults to 10 minutes
}

// MFEStep runs a micro-frontend command and streams output to the MFE panel.
// The process is placed in its own process group so SIGTERM reaches all
// child processes (e.g. node workers spawned by npm).
type MFEStep struct {
	Cmd       MFECommand
	StatePath string    // path to state.json; defaults to config.DefaultStatePath()
	pgid      int       // set by Start; used by Stop
	send      func(any) // injected sender; falls back to global Send
}

func (s *MFEStep) statePath() string {
	if s.StatePath != "" {
		return s.StatePath
	}
	return config.DefaultStatePath()
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
//
// When ReadyPattern is set, Start blocks until the pattern appears in the
// process output, the process exits, or the timeout is reached.
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

	if s.Cmd.ReadyPattern != nil {
		return s.startAndWaitForReady(ctx, cmd, lf)
	}

	cmd.Stdout = lf
	cmd.Stderr = lf

	if err := cmd.Start(); err != nil {
		lf.Close()
		return err
	}
	s.pgid = cmd.Process.Pid
	s.sender()(step.PIDMsg{PID: cmd.Process.Pid})

	sp := s.statePath()
	if err := config.SaveStepData(sp, s.ID(), "pid", strconv.Itoa(s.pgid)); err != nil {
		step.DebugLog("mfe: SaveStepData pid: %v", err)
	}

	// Kill the process group when the instance context is cancelled.
	go func() {
		<-ctx.Done()
		_ = syscall.Kill(-s.pgid, syscall.SIGTERM)
	}()

	// Wait for the process and write a final log line.
	go func() {
		defer lf.Close()
		err := cmd.Wait()
		config.SaveStepData(sp, s.ID(), "pid", "") //nolint:errcheck
		if err != nil {
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

// startAndWaitForReady launches the process with output directed to the log
// file and scans the file for ReadyPattern. Blocks until the pattern matches,
// the process exits, or the timeout fires. The process writes directly to the
// log file so it survives TUI restarts (enabling resume).
func (s *MFEStep) startAndWaitForReady(ctx context.Context, cmd *exec.Cmd, lf *os.File) error {
	cmd.Stdout = lf
	cmd.Stderr = lf

	if err := cmd.Start(); err != nil {
		lf.Close()
		return err
	}
	s.pgid = cmd.Process.Pid
	s.sender()(step.PIDMsg{PID: cmd.Process.Pid})

	sp := s.statePath()
	if err := config.SaveStepData(sp, s.ID(), "pid", strconv.Itoa(s.pgid)); err != nil {
		step.DebugLog("mfe: SaveStepData pid: %v", err)
	}

	go func() {
		<-ctx.Done()
		_ = syscall.Kill(-s.pgid, syscall.SIGTERM)
	}()

	exitErr := make(chan error, 1)
	go func() {
		err := cmd.Wait()
		config.SaveStepData(sp, s.ID(), "pid", "") //nolint:errcheck
		exitErr <- err
	}()

	ready := make(chan struct{})
	var readyOnce sync.Once
	scanCtx, cancelScan := context.WithCancel(ctx)
	go s.scanFileForPattern(scanCtx, lf.Name(), func() {
		readyOnce.Do(func() { close(ready) })
	})

	timeout := s.Cmd.ReadyTimeout
	if timeout == 0 {
		timeout = 10 * time.Minute
	}

	select {
	case <-ready:
		cancelScan()
		// Pattern matched — check if process also exited at the same time.
		select {
		case err := <-exitErr:
			lf.Close()
			if ctx.Err() != nil {
				return nil
			}
			if err != nil {
				return fmt.Errorf("mfe: %w", err)
			}
			return nil
		default:
			// Process still running — genuinely ready.
			go func() {
				defer lf.Close()
				if err := <-exitErr; err != nil {
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

	case err := <-exitErr:
		cancelScan()
		lf.Close()
		if ctx.Err() != nil {
			return nil
		}
		if err != nil {
			return fmt.Errorf("mfe: %w", err)
		}
		return nil

	case <-ctx.Done():
		cancelScan()
		go func() {
			defer lf.Close()
			<-exitErr
		}()
		return nil

	case <-time.After(timeout):
		cancelScan()
		lf.WriteString(fmt.Sprintf("[mfe: timed out after %v waiting for readiness]\n", timeout)) //nolint:errcheck
		go func() {
			defer lf.Close()
			<-exitErr
		}()
		return fmt.Errorf("mfe: timed out after %v waiting for ready pattern", timeout)
	}
}

// scanFileForPattern tails a file and calls onMatch when ReadyPattern is found.
// Accumulates partial lines across reads to handle line splits at read boundaries.
func (s *MFEStep) scanFileForPattern(ctx context.Context, path string, onMatch func()) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	r := bufio.NewReader(f)
	var partial string
	for {
		line, err := r.ReadString('\n')
		partial += line
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			if s.Cmd.ReadyPattern.MatchString(partial) {
				onMatch()
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(100 * time.Millisecond):
				continue
			}
		}
		if s.Cmd.ReadyPattern.MatchString(partial) {
			onMatch()
			return
		}
		partial = ""
	}
}

// Resume implements step.Resumer. It checks if the MFE process from a
// previous session is still alive. If so, it reattaches (the log file is
// still being written to and WatchStep will tail it). If dead, it returns
// an error to trigger a fresh Start().
func (s *MFEStep) Resume(_ context.Context, _ string) error {
	pidStr, ok := config.LoadStepData(s.statePath(), s.ID(), "pid")
	if !ok || pidStr == "" {
		return fmt.Errorf("mfe: no saved PID, restarting")
	}
	pid, err := strconv.Atoi(pidStr)
	if err != nil || pid <= 0 {
		return fmt.Errorf("mfe: invalid saved PID %q, restarting", pidStr)
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("mfe: process not found (pid=%d), restarting", pid)
	}
	if sigErr := proc.Signal(syscall.Signal(0)); sigErr != nil {
		return fmt.Errorf("mfe: process dead (pid=%d), restarting", pid)
	}
	s.pgid = pid
	return nil
}

// Stop sends SIGTERM to the MFE process group. Falls back to the saved PID
// if the in-memory pgid is not set (e.g. after a TUI restart).
func (s *MFEStep) Stop(_ context.Context, _ string) error {
	pgid := s.pgid
	if pgid <= 0 {
		if pidStr, ok := config.LoadStepData(s.statePath(), s.ID(), "pid"); ok {
			if p, err := strconv.Atoi(pidStr); err == nil && p > 0 {
				pgid = p
			}
		}
	}
	if pgid > 0 {
		step.KillProcessGroup(pgid)
		config.SaveStepData(s.statePath(), s.ID(), "pid", "") //nolint:errcheck
	}
	return nil
}

// MFETemplate returns a StepTemplate for a micro-frontend runner.
//
// mfes is the list of available MFE names shown in the single-select picker.
// run is called with the selected MFE name and the full wizard values (so port
// fields or other selections can be read); if nil, defaults to "npm start" in
// the MFE name directory.
func MFETemplate(mfes []string, run func(name string, v tui.WizardValues) MFECommand) tui.StepTemplate {
	return tui.StepTemplate{
		ID:    "mfe",
		Panel: tui.PanelBottomRight,
		Label: "MFE",
		Fields: []tui.FieldSpec{
			{ID: "mfe", Label: "MFE", Kind: tui.FieldKindSingleSelect, OptionsFunc: tui.StaticOptions(mfes...)},
		},
		Build: func(v tui.WizardValues) (step.Step, error) {
			mfe := v.String("mfe")
			if mfe == "" {
				return nil, nil
			}
			var cmd MFECommand
			if run != nil {
				cmd = run(mfe, v)
			} else {
				cmd = MFECommand{Cmd: "npm", Args: []string{"start"}, Dir: mfe}
			}
			if cmd.Cmd == "" {
				return nil, nil
			}
			return &MFEStep{Cmd: cmd}, nil
		},
	}
}
