package step

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"
	"tui/config"
)

// SkaffoldStep runs `skaffold <mode>` and streams output to the Skaffold panel.
// It depends on minikube being ready before Start is called.
type SkaffoldStep struct {
	Path string // path to skaffold.yaml
	Mode string // "dev", "run", or "debug"; defaults to "dev"
}

func (s *SkaffoldStep) ID() string                { return "skaffold" }
func (s *SkaffoldStep) LogPath(name string) string { return config.SkaffoldLogPath(name) }

func (s *SkaffoldStep) ReadConfig(cfg config.InstanceConfig) {
	if cfg.Skaffold.Path != "" {
		s.Path = cfg.Skaffold.Path
	}
}

func (s *SkaffoldStep) WriteConfig(cfg *config.InstanceConfig) {
	cfg.Skaffold.Path = s.Path
}

// Start launches skaffold and blocks until it signals readiness:
//   - run mode: blocks until the process exits (success = ready, failure = error).
//   - dev/debug mode: blocks until the first successful deploy is detected via
//     the Skaffold HTTP event API, then returns while the process keeps running.
func (s *SkaffoldStep) Start(ctx context.Context, instanceName string) error {
	mode := s.Mode
	if mode == "" {
		mode = "dev"
	}

	logPath := config.SkaffoldLogPath(instanceName)
	os.Remove(logPath) // clear previous log so tail -F starts fresh
	lf, err := os.Create(logPath)
	if err != nil {
		return fmt.Errorf("log create: %w", err)
	}

	absPath, err := filepath.Abs(s.Path)
	if err != nil {
		absPath = s.Path
	}

	if mode == "run" {
		return s.startRunMode(ctx, lf, absPath)
	}
	return s.startWatchMode(ctx, lf, absPath, mode)
}

// startRunMode runs `skaffold run` synchronously and blocks until the process
// exits. A zero exit code is treated as "ready/done"; non-zero is an error.
func (s *SkaffoldStep) startRunMode(ctx context.Context, lf *os.File, absPath string) error {
	defer lf.Close()
	cmd := exec.CommandContext(ctx, "skaffold", "run", "--filename", absPath)
	cmd.Dir = filepath.Dir(absPath)
	cmd.Stdout = lf
	cmd.Stderr = lf
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil // cancelled
		}
		return err
	}
	return nil
}

// startWatchMode runs `skaffold dev|debug` with --rpc-http-port and blocks
// until the first successful deploy is detected via the event stream, then
// returns while skaffold continues running in the background.
func (s *SkaffoldStep) startWatchMode(ctx context.Context, lf *os.File, absPath, mode string) error {
	port, err := RandomPort()
	if err != nil {
		lf.Close()
		return fmt.Errorf("finding free port: %w", err)
	}

	cmd := exec.CommandContext(ctx, "skaffold", mode,
		"--filename", absPath,
		"--rpc-http-port", strconv.Itoa(port),
	)
	cmd.Dir = filepath.Dir(absPath)
	cmd.Stdout = lf
	cmd.Stderr = lf

	if err := cmd.Start(); err != nil {
		lf.Close()
		return err
	}

	// watchCtx is cancelled when the process exits, which unblocks the event
	// watcher so it doesn't linger after skaffold dies.
	watchCtx, cancelWatch := context.WithCancel(ctx)

	// exitErr receives the process exit status. It is filled before cancelWatch
	// is called so that when the watcher unblocks (due to the cancel) the exit
	// status is already available for the race-free check below.
	exitErr := make(chan error, 1)
	go func() {
		defer lf.Close()
		err := cmd.Wait()
		exitErr <- err   // fill before cancelling the watcher
		cancelWatch()    // unblock waitForSkaffoldDeploy
		if ctx.Err() != nil {
			return // instance was stopped — suppress noise
		}
		if err != nil {
			Send(CommandMsg{Text: fmt.Sprintf("[skaffold exited: %v]", err)})
		} else {
			Send(CommandMsg{Text: "[skaffold exited cleanly]"})
		}
	}()

	// ready is closed by the watcher goroutine when a deploy-complete event
	// is received (or when the watcher context is cancelled).
	ready := make(chan struct{})
	go func() {
		waitForSkaffoldDeploy(watchCtx, port)
		close(ready)
	}()

	select {
	case <-ready:
		// The watcher returned — check whether it was triggered by a genuine
		// deploy-complete or by the process exiting (cancelWatch).
		select {
		case err := <-exitErr:
			// Process exited around the same time; treat it as a failure.
			if ctx.Err() != nil {
				return nil
			}
			if err != nil {
				return fmt.Errorf("skaffold: %w", err)
			}
			return nil // exited cleanly (unusual but OK)
		default:
			return nil // process still running — genuinely ready
		}

	case err := <-exitErr:
		cancelWatch()
		if ctx.Err() != nil {
			return nil
		}
		if err != nil {
			return fmt.Errorf("skaffold: %w", err)
		}
		return nil

	case <-ctx.Done():
		cancelWatch()
		return nil
	}
}

// Stop is a no-op: skaffold is terminated when the instance context is cancelled.
func (s *SkaffoldStep) Stop(_ context.Context, _ string) error { return nil }

// waitForSkaffoldDeploy connects to the Skaffold HTTP event stream and blocks
// until a successful deploy event is received, the stream closes, or ctx is
// cancelled.
func waitForSkaffoldDeploy(ctx context.Context, port int) {
	url := fmt.Sprintf("http://localhost:%d/v1/events", port)

	// Retry until the HTTP server comes up. Skaffold starts the HTTP listener
	// before beginning work, so this is usually just one or two retries.
	var resp *http.Response
	for {
		if ctx.Err() != nil {
			return
		}
		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			return
		}
		r, err := http.DefaultClient.Do(req)
		if err == nil {
			resp = r
			break
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(500 * time.Millisecond):
		}
	}
	defer resp.Body.Close()

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		if ctx.Err() != nil {
			return
		}
		if skaffoldDeployComplete(scanner.Text()) {
			return
		}
	}
}

// skaffoldDeployComplete returns true if the JSON event line signals that the
// deploy phase completed successfully. Skaffold emits two overlapping event
// schemas (deployEvent and taskEvent) depending on version; we check both.
func skaffoldDeployComplete(line string) bool {
	var env struct {
		Result struct {
			Event struct {
				DeployEvent struct {
					Status string `json:"status"`
				} `json:"deployEvent"`
				TaskEvent struct {
					Task   string `json:"task"`
					Status string `json:"status"`
				} `json:"taskEvent"`
			} `json:"event"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(line), &env); err != nil {
		return false
	}
	if s := env.Result.Event.DeployEvent.Status; s == "Complete" || s == "Succeeded" {
		return true
	}
	te := env.Result.Event.TaskEvent
	return te.Task == "Deploy" && (te.Status == "Succeeded" || te.Status == "Complete")
}
