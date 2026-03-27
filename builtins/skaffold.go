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
	Path     string    // path to skaffold.yaml
	Mode     string    // "dev", "run", or "debug"; defaults to "dev"
	Profiles []string  // optional skaffold profiles to activate (--profile flags)
	send     func(any) // injected sender; falls back to global Send
}

// SetSender injects a message sender for testing. Falls back to the global Send.
func (s *SkaffoldStep) SetSender(fn func(any)) { s.send = fn }

func (s *SkaffoldStep) sender() func(any) {
	if s.send != nil {
		return s.send
	}
	return step.Send
}

func (s *SkaffoldStep) ID() string                 { return fmt.Sprintf("skaffold_%s", s.Mode) }
func (s *SkaffoldStep) LogPath(name string) string { return SkaffoldLogPath(name, s.Mode) }

// Start launches skaffold and blocks until it signals readiness:
//   - run mode: blocks until the process exits (success = ready, failure = error).
//   - dev/debug mode: blocks until the first successful deploy is detected via
//     the Skaffold HTTP event API, then returns while the process keeps running.
func (s *SkaffoldStep) Start(ctx context.Context, instanceName string) error {
	mode := s.Mode
	if mode == "" {
		mode = "dev"
	}

	logPath := SkaffoldLogPath(instanceName, mode)
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
	args := []string{"run", "--filename", absPath}
	for _, p := range s.Profiles {
		args = append(args, "--profile", p)
	}
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
func (s *SkaffoldStep) startWatchMode(ctx context.Context, lf *os.File, absPath, mode string) error {
	port, err := step.RandomPort()
	if err != nil {
		lf.Close()
		return fmt.Errorf("finding free port: %w", err)
	}

	args := []string{mode, "--filename", absPath, "--rpc-http-port", strconv.Itoa(port)}
	for _, p := range s.Profiles {
		args = append(args, "--profile", p)
	}
	cmd := exec.CommandContext(ctx, "skaffold", args...)
	cmd.Dir = filepath.Dir(absPath)
	cmd.Stdout = lf
	cmd.Stderr = lf
	fmt.Fprintf(lf, "[tui] running: %s\n", strings.Join(cmd.Args, " "))

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
	send := s.sender()
	exitErr := make(chan error, 1)
	go func() {
		defer lf.Close()
		err := cmd.Wait()
		exitErr <- err // fill before cancelling the watcher
		cancelWatch()  // unblock waitForSkaffoldDeploy
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

// Stop is a no-op: skaffold is terminated when the instance context is cancelled.
func (s *SkaffoldStep) Stop(_ context.Context, _ string) error { return nil }

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

// SkaffoldTemplate returns a StepTemplate for skaffold.
//
// generate is called with the full wizard values and should return the path to
// a skaffold.yaml and an optional list of skaffold profiles to activate.
// Returning an empty path skips the step; returning an error aborts the wizard.
// The wizard values include the "mode" field contributed by this template, as
// well as any fields from other templates in the pipeline (e.g. an "env" field
// from a companion step, or "components" from a custom selection step).
//
// If you need component selection, add a separate Hidden step with a
// FieldKindSystemSelect field (see the "env" step pattern in sample/main.go).
func SkaffoldTemplate(generate func(v tui.WizardValues) (path string, profiles []string, err error)) tui.StepTemplate {
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
				Name: "status",
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
			if generate == nil {
				return nil, nil
			}
			mode := v.String("mode")
			if mode == "" {
				mode = "dev"
			}
			path, profiles, err := generate(v)
			if err != nil {
				return nil, err
			}
			if path == "" {
				return nil, nil
			}
			return &SkaffoldStep{Path: path, Mode: mode, Profiles: profiles}, nil
		},
	}
}

// SkaffoldBuildTemplate returns a StepTemplate for skaffold build.
//
// generate is called with the full wizard values and should return the path to
// a skaffold.yaml and an optional list of skaffold profiles to activate.
// Returning an empty path skips the step; returning an error aborts the wizard.
// The wizard values include the "mode" field contributed by this template, as
// well as any fields from other templates in the pipeline (e.g. an "env" field
// from a companion step, or "components" from a custom selection step).
//
// If you need component selection, add a separate Hidden step with a
// FieldKindSystemSelect field (see the "env" step pattern in sample/main.go).
func SkaffoldBuildTemplate(generate func(v tui.WizardValues) (path string, profiles []string, err error)) tui.StepTemplate {
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
			if generate == nil {
				return nil, nil
			}
			path, profiles, err := generate(v)
			if err != nil {
				return nil, err
			}
			if path == "" {
				return nil, nil
			}
			return &SkaffoldStep{Path: path, Mode: "build", Profiles: profiles}, nil
		},
	}
}

// SkaffoldConfig holds the generated skaffold file path and profiles.
// It is used to share configuration between SkaffoldFileGeneratorTemplate
// and multiple skaffold step templates (build, dev, run, debug).
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
		Panel:  tui.PanelTopLeft,
		Label:  "Skaffold Config",
		Hidden: true,
		Build: func(v tui.WizardValues) (step.Step, error) {
			if generate == nil {
				return nil, fmt.Errorf("skaffold generator: generate function is nil")
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
				Name: "status",
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
			return &SkaffoldStep{Path: cfg.Path, Mode: mode, Profiles: cfg.Profiles}, nil
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
			return &SkaffoldStep{Path: cfg.Path, Mode: "build", Profiles: cfg.Profiles}, nil
		},
	}
}
