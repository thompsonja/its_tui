package builtins

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
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/thompsonja/its_tui/config"
	"github.com/thompsonja/its_tui/step"
	"github.com/thompsonja/its_tui/tui"
)

// SkaffoldLogPath returns the per-instance log file written by skaffold.
func SkaffoldLogPath(instanceName, mode string) string {
	if instanceName == "" {
		return ""
	}
	return filepath.Join(config.GetLogDir(), fmt.Sprintf("skaffold_%s_%s.log", instanceName, mode))
}

// SkaffoldStep runs `skaffold <mode>` and streams output to the Skaffold panel.
// It depends on minikube being ready before Start is called.
type SkaffoldStep struct {
	Path       string    // path to skaffold.yaml
	Mode       string    // "dev", "run", or "debug"; defaults to "dev"
	Profiles   []string  // optional skaffold profiles to activate (--profile flags)
	ExtraArgs  []string  // additional flags appended to the skaffold command
	IDOverride string    // if set, overrides the default ID derived from Mode
	StatePath  string    // path to state.json; defaults to config.DefaultStatePath()
	send       func(any) // injected sender; falls back to global Send
}

// SetSender injects a message sender for testing. Falls back to the global Send.
func (s *SkaffoldStep) SetSender(fn func(any)) { s.send = fn }

func (s *SkaffoldStep) statePath() string {
	if s.StatePath != "" {
		return s.StatePath
	}
	return config.DefaultStatePath()
}

func (s *SkaffoldStep) sender() func(any) {
	if s.send != nil {
		return s.send
	}
	return step.Send
}

func (s *SkaffoldStep) ID() string {
	if s.IDOverride != "" {
		return s.IDOverride
	}
	if s.Mode == "build" {
		return "skaffold_build"
	}
	return "skaffold"
}

func (s *SkaffoldStep) LogPath(name string) string {
	if s.IDOverride != "" {
		return SkaffoldLogPath(name, s.IDOverride)
	}
	return SkaffoldLogPath(name, s.Mode)
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
	step.DebugLog("skaffold %s Start() called for instance %q", mode, instanceName)

	logPath := s.LogPath(instanceName)
	os.Remove(logPath) // clear previous log so tail -F starts fresh
	lf, err := os.Create(logPath)
	if err != nil {
		return fmt.Errorf("log create: %w", err)
	}

	absPath, err := filepath.Abs(s.Path)
	if err != nil {
		absPath = s.Path
	}

	if mode == "run" || mode == "build" || mode == "deploy" {
		return s.startRunMode(ctx, lf, absPath, mode)
	}
	return s.startWatchMode(ctx, instanceName, lf, absPath, mode)
}

// startRunMode runs `skaffold run` or `skaffold build` synchronously and blocks
// until the process exits. A zero exit code is "ready/done"; non-zero is an error.
func (s *SkaffoldStep) startRunMode(ctx context.Context, lf *os.File, absPath, mode string) error {
	defer lf.Close()
	args := []string{mode, "--filename", absPath}
	for _, p := range s.Profiles {
		args = append(args, "--profile", p)
	}
 	args = append(args, s.ExtraArgs...)
	cmd := exec.CommandContext(ctx, "skaffold", args...)
	cmd.Dir = filepath.Dir(absPath)
	cmd.Stdout = lf
	cmd.Stderr = lf
	fmt.Fprintf(lf, "[tui] running: %s\n", strings.Join(cmd.Args, " "))
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
//
// Skaffold runs in its own process group (Setpgid) so ctrl+c in the terminal
// only kills the TUI, not skaffold. The PID is saved synchronously to step_data
// so Resume() can check liveness after a TUI restart.
func (s *SkaffoldStep) startWatchMode(ctx context.Context, instanceName string, lf *os.File, absPath, mode string) error {
	port, err := step.RandomPort()
	if err != nil {
		lf.Close()
		return fmt.Errorf("finding free port: %w", err)
	}

	args := []string{mode, "--filename", absPath, "--rpc-http-port", strconv.Itoa(port)}
	for _, p := range s.Profiles {
		args = append(args, "--profile", p)
	}
	// Use exec.Command (not CommandContext) so we can manage the process group.
	args = append(args, s.ExtraArgs...)
	cmd := exec.Command("skaffold", args...)
	cmd.Dir = filepath.Dir(absPath)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Stdout = lf
	cmd.Stderr = lf
	fmt.Fprintf(lf, "[tui] running: %s\n", strings.Join(cmd.Args, " "))

	if err := cmd.Start(); err != nil {
		lf.Close()
		return err
	}

	pgid := cmd.Process.Pid // with Setpgid=true, pgid == pid

	// Save PID directly to step_data so Resume() can check liveness on restart.
	// Synchronous — the PID is on disk before we block waiting for deploy.
	sp := s.statePath()
	if err := config.SaveStepData(sp, s.ID(), "pid", strconv.Itoa(pgid)); err != nil {
		step.DebugLog("skaffold %s: SaveStepData pid: %v", mode, err)
	}

	// Kill the process group when the instance context is cancelled (stop command).
	// SIGTERM lets skaffold clean up; the process group ensures children are included.
	go func() {
		<-ctx.Done()
		syscall.Kill(-pgid, syscall.SIGTERM)
	}()

	// watchCtx is cancelled when the process exits, which unblocks the event
	// watcher so it doesn't linger after skaffold dies.
	watchCtx, cancelWatch := context.WithCancel(ctx)

	// exitErr receives the process exit status. It is filled before cancelWatch
	// is called so that when the watcher unblocks (due to the cancel) the exit
	// status is already available for the race-free check below.
	send := s.sender()
	exitErr := make(chan error, 1)
	go func() {
		defer lf.Close()
		err := cmd.Wait()
		// Clear the saved PID so Resume() restarts rather than probing a dead PID.
		config.SaveStepData(sp, s.ID(), "pid", "") //nolint:errcheck
		exitErr <- err                              // fill before cancelling the watcher
		cancelWatch()                               // unblock waitForSkaffoldDeploy
		if ctx.Err() != nil {
			return // instance was stopped — suppress noise
		}
		if err != nil {
			send(step.CommandMsg{Text: fmt.Sprintf("[skaffold exited: %v]", err)})
		}
	}()

	// ready is closed exactly once when the first deploy-complete event arrives.
	// processSkaffoldEvents continues running after that to capture port events.
	ready := make(chan struct{})
	var readyOnce sync.Once
	go func() {
		processSkaffoldEvents(watchCtx, port, func() {
			readyOnce.Do(func() { close(ready) })
		}, send)
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

// Resume implements step.Resumer. For run and build modes the work persists in
// the cluster/registry so we skip. For dev and debug, we read the PID from
// step_data and check liveness. Alive → reattach (WatchStep tails existing
// log). Dead or no PID → return error so Start() redeploys.
func (s *SkaffoldStep) Resume(_ context.Context, instanceName string) error {
	switch s.Mode {
	case "run", "build", "deploy":
		step.DebugLog("skaffold %s Resume(): run/build mode — skipping (work persists)", s.Mode)
		return nil // deployment / images already exist; nothing to do
	default:
		mode := s.Mode
		if mode == "" {
			mode = "dev"
		}
		pidStr, ok := config.LoadStepData(s.statePath(), s.ID(), "pid")
		step.DebugLog("skaffold %s Resume() for instance %q: loaded pid=%q ok=%v", mode, instanceName, pidStr, ok)
		if !ok || pidStr == "" {
			err := fmt.Errorf("skaffold %s: no saved PID, restarting", mode)
			step.DebugLog("skaffold %s Resume(): %v", mode, err)
			return err
		}
		pid, err := strconv.Atoi(pidStr)
		if err != nil || pid <= 0 {
			err := fmt.Errorf("skaffold %s: invalid saved PID %q, restarting", mode, pidStr)
			step.DebugLog("skaffold %s Resume(): %v", mode, err)
			return err
		}
		proc, err := os.FindProcess(pid)
		if err != nil {
			err := fmt.Errorf("skaffold %s: process not found (pid=%d), restarting", mode, pid)
			step.DebugLog("skaffold %s Resume(): %v", mode, err)
			return err
		}
		// Signal 0 checks liveness without delivering a signal.
		if sigErr := proc.Signal(syscall.Signal(0)); sigErr != nil {
			err := fmt.Errorf("skaffold %s: process dead (pid=%d, signal err=%v), restarting", mode, pid, sigErr)
			step.DebugLog("skaffold %s Resume(): %v", mode, err)
			return err
		}
		step.DebugLog("skaffold %s Resume(): pid=%d is alive — reattaching without redeploy", mode, pid)
		return nil // process still alive — reattach without redeploying
	}
}

// processSkaffoldEvents connects to the Skaffold HTTP event stream and reads
// it until ctx is cancelled. It calls onDeployed when the first deploy-complete
// event is seen, and sends a DebugPortMsg for each port-forward event.
func processSkaffoldEvents(ctx context.Context, port int, onDeployed func(), send func(any)) {
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
		line := scanner.Text()
		if skaffoldDeployComplete(line) {
			onDeployed()
		}
		if msg := skaffoldPortEvent(line); msg != nil {
			send(*msg)
		}
	}
}

// skaffoldPortEvent parses a port-forward event from the Skaffold event stream.
// Returns nil if the line is not a port event or cannot be parsed.
func skaffoldPortEvent(line string) *step.DebugPortMsg {
	var env struct {
		Result struct {
			Event struct {
				PortEvent struct {
					LocalPort    int    `json:"localPort"`
					RemotePort   int    `json:"remotePort"`
					ResourceName string `json:"resourceName"`
					PortName     string `json:"portName"`
					Address      string `json:"address"`
				} `json:"portEvent"`
			} `json:"event"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(line), &env); err != nil {
		return nil
	}
	pe := env.Result.Event.PortEvent
	if pe.LocalPort == 0 {
		return nil
	}
	return &step.DebugPortMsg{
		LocalPort:    pe.LocalPort,
		RemotePort:   pe.RemotePort,
		ResourceName: pe.ResourceName,
		PortName:     pe.PortName,
		Address:      pe.Address,
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

// SkaffoldConfig holds the generated skaffold file path and profiles.
// It is used to share configuration between SkaffoldFileGeneratorTemplate
// and multiple skaffold step templates (build, deploy, dev, run, debug).
type SkaffoldConfig struct {
	Path     string
	Profiles []string
}

// SkaffoldFileGeneratorTemplate generates a skaffold.yaml file once during Build()
// and stores the result in the provided SkaffoldConfig pointer.
//
// This is typically used as a hidden step that runs before other skaffold steps,
// allowing multiple steps (e.g., build + dev) to reference the same generated file.
//
// generate is called with the full wizard values and should return the path to
// the generated skaffold.yaml and an optional list of skaffold profiles to activate.
// Returning an empty path or error will be reported during wizard validation.
//
// Example:
//
//	cfg := &builtins.SkaffoldConfig{}
//	templates := []tui.StepTemplate{
//	    builtins.SkaffoldFileGeneratorTemplate(cfg, func(v tui.WizardValues) (string, []string, error) {
//	        return generateSkaffoldYAML(v.String("env"), v.String("port"))
//	    }),
//	    builtins.SkaffoldBuildTemplateFrom(cfg),
//	    builtins.SkaffoldTemplateFrom(cfg),
//	}
func SkaffoldFileGeneratorTemplate(cfg *SkaffoldConfig, generate func(v tui.WizardValues) (path string, profiles []string, err error)) tui.StepTemplate {
	return tui.StepTemplate{
		ID:     "skaffold_generator",
		Panel:  tui.PanelNone,
		Label:  "Skaffold Config",
		Hidden: true,
		Build: func(v tui.WizardValues) (step.Step, error) {
			if generate == nil {
				return nil, fmt.Errorf("skaffold generator: generate function is nil")
			}

			const genID = "skaffold_generator"
			sp := config.DefaultStatePath()

			// On session restore the wizard values are identical to last run, so
			// skip regeneration to avoid touching the file and triggering a
			// skaffold dev redeploy.
			if v.IsRestore() {
				savedPath, ok := config.LoadStepData(sp, genID, "path")
				if ok && savedPath != "" {
					if _, err := os.Stat(savedPath); err == nil {
						step.DebugLog("skaffold generator: restore — reusing cached path %q", savedPath)
						cfg.Path = savedPath
						if savedProfs, ok := config.LoadStepData(sp, genID, "profiles"); ok && savedProfs != "" {
							cfg.Profiles = strings.Split(savedProfs, ",")
						} else {
							cfg.Profiles = nil
						}
						return nil, nil
					}
					step.DebugLog("skaffold generator: restore — cached path %q gone, regenerating", savedPath)
				}
			}

			path, profiles, err := generate(v)
			if err != nil {
				return nil, fmt.Errorf("skaffold generator: %w", err)
			}
			if path == "" {
				return nil, fmt.Errorf("skaffold generator: generate returned empty path")
			}
			cfg.Path = path
			cfg.Profiles = profiles

			config.SaveStepData(sp, genID, "path", path)                           //nolint:errcheck
			config.SaveStepData(sp, genID, "profiles", strings.Join(profiles, ",")) //nolint:errcheck
			step.DebugLog("skaffold generator: generated path %q", path)

			return nil, nil // no step to run, just populate the config
		},
	}
}

// SkaffoldTemplateFrom returns a StepTemplate for skaffold that reads its
// configuration from a shared SkaffoldConfig (populated by SkaffoldFileGeneratorTemplate).
//
// This allows multiple skaffold steps to use the same generated configuration file.
// The step will use the path and profiles from cfg, and add a "mode" field to select
// between "dev", "run", or "debug".
func SkaffoldTemplateFrom(cfg *SkaffoldConfig) tui.StepTemplate {
	return tui.StepTemplate{
		// ID omitted: Step.ID() returns "skaffold_<mode>" which varies based on wizard selection
		Panel: tui.PanelTopRight,
		LabelFunc: func(v tui.WizardValues) string {
			mode := v.String("mode")
			if mode == "" {
				mode = "dev"
			}
			return "Skaffold (" + mode + ")"
		},
		WaitFor: []string{"minikube"},
		Fields: []tui.FieldSpec{
			{ID: "mode", Label: "Mode", Kind: tui.FieldKindSelect, OptionsFunc: tui.StaticOptions("dev", "run", "debug"), Default: 0},
		},
		Commands: []tui.CommandSpec{
			{
				Name: "deploy_status",
				Help: "show skaffold deployment status",
				Handler: func(args []string, instanceName string, values tui.WizardValues) error {
					if instanceName == "" {
						tui.PrintCommand("  no instance running - use: start")
						return nil
					}
					mode := values.String("mode")
					if mode == "" {
						mode = "dev"
					}
					tui.PrintCommand(fmt.Sprintf("  skaffold running in %s mode", mode))
					tui.PrintCommand(fmt.Sprintf("  instance: %s", instanceName))
					return nil
				},
			},
		},
		Build: func(v tui.WizardValues) (step.Step, error) {
			if cfg.Path == "" {
				return nil, fmt.Errorf("skaffold config path is empty - ensure SkaffoldFileGeneratorTemplate ran first")
			}
			mode := v.String("mode")
			if mode == "" {
				mode = "dev"
			}
			return &SkaffoldStep{Path: cfg.Path, Mode: mode, Profiles: cfg.Profiles, StatePath: config.DefaultStatePath()}, nil
		},
	}
}

// SkaffoldBuildTemplateFrom returns a StepTemplate for skaffold build that reads its
// configuration from a shared SkaffoldConfig (populated by SkaffoldFileGeneratorTemplate).
//
// This allows the build step to share the same configuration file as other skaffold steps.
func SkaffoldBuildTemplateFrom(cfg *SkaffoldConfig) tui.StepTemplate {
	return tui.StepTemplate{
		ID:    "skaffold_build",
		Panel: tui.PanelTopRight,
		LabelFunc: func(v tui.WizardValues) string {
			return "Skaffold (build)"
		},
		WaitFor: []string{"minikube"},
		Commands: []tui.CommandSpec{
			{
				Name: "build_status",
				Help: "show skaffold build status",
				Handler: func(args []string, instanceName string, values tui.WizardValues) error {
					if instanceName == "" {
						tui.PrintCommand("  no instance running - use: start")
						return nil
					}
					tui.PrintCommand("  skaffold running in build mode")
					tui.PrintCommand(fmt.Sprintf("  instance: %s", instanceName))
					return nil
				},
			},
		},
		Build: func(v tui.WizardValues) (step.Step, error) {
			if cfg.Path == "" {
				return nil, fmt.Errorf("skaffold config path is empty - ensure SkaffoldFileGeneratorTemplate ran first")
			}
			return &SkaffoldStep{Path: cfg.Path, Mode: "build", Profiles: cfg.Profiles, StatePath: config.DefaultStatePath()}, nil
		},
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// SkaffoldPipeline — recommended entry point for multi-step skaffold pipelines.
// ──────────────────────────────────────────────────────────────────────────────

// SkaffoldPipeline coordinates a skaffold generator, build, and dev/run/debug step
// that share a single generated configuration file. Create one with
// NewSkaffoldPipeline, then include the templates it returns in Config.Steps.
//
// Typical usage:
//
//	pipeline := builtins.NewSkaffoldPipeline(func(v tui.WizardValues) (string, []string, error) {
//	    return generateSkaffoldYAML(v.String("env"))
//	})
//	cfg := tui.Config{
//	    Steps: []tui.StepTemplate{
//	        builtins.MinikubeTemplate(),
//	        builtins.KubectlTemplate(),
//	        pipeline.GeneratorTemplate(),
//	        pipeline.BuildTemplate(),
//	        pipeline.DevTemplate(),
//	        builtins.MFETemplate(...),
//	    },
//	}
type SkaffoldPipeline struct {
	cfg      *SkaffoldConfig
	generate func(v tui.WizardValues) (path string, profiles []string, err error)
}

// NewSkaffoldPipeline creates a pipeline for the given generate function.
// The function is called once per wizard submission; its result is shared
// by all templates from this pipeline.
func NewSkaffoldPipeline(generate func(v tui.WizardValues) (path string, profiles []string, err error)) *SkaffoldPipeline {
	return &SkaffoldPipeline{cfg: &SkaffoldConfig{}, generate: generate}
}

// GeneratorTemplate returns the hidden step that runs the generate function and
// caches the result for the build and dev templates. Register this before
// BuildTemplate and DevTemplate in Config.Steps.
func (p *SkaffoldPipeline) GeneratorTemplate() tui.StepTemplate {
	return SkaffoldFileGeneratorTemplate(p.cfg, p.generate)
}

// BuildTemplate returns the step that runs `skaffold build`. It waits for minikube.
func (p *SkaffoldPipeline) BuildTemplate() tui.StepTemplate {
	return SkaffoldBuildTemplateFrom(p.cfg)
}

// DevTemplate returns the step that runs `skaffold dev`, `run`, or `debug`.
// It waits for minikube and, if BuildTemplate is also included, waits for the
// build step to complete first. Including DevTemplate without BuildTemplate is
// safe — the build dependency is ignored at runtime when no build step is present.
func (p *SkaffoldPipeline) DevTemplate() tui.StepTemplate {
	t := SkaffoldTemplateFrom(p.cfg)
	t.WaitFor = append(t.WaitFor, "skaffold_build")
	return t
}

// Templates returns all three templates in registration order: generator, build,
// then dev. Append them to Config.Steps for the full build-then-deploy pipeline.
//
//	steps = append(steps, pipeline.Templates()...)
func (p *SkaffoldPipeline) Templates() []tui.StepTemplate {
	return []tui.StepTemplate{
		p.GeneratorTemplate(),
		p.BuildTemplate(),
		p.DevTemplate(),
	}
}
