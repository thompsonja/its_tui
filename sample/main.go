package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/thompsonja/its_tui/builtins"
	"github.com/thompsonja/its_tui/step"
	"github.com/thompsonja/its_tui/tui"
)

// slowStep is a synthetic step that runs for a fixed duration, logging
// progress every 5 seconds. Useful for testing progress bar behaviour.
type slowStep struct {
	id       string
	duration time.Duration
}

func (s *slowStep) ID() string                      { return s.id }
func (s *slowStep) LogPath(_ string) string          { return "" } // direct-send

func (s *slowStep) Start(ctx context.Context, _ string) error {
	step.Send(step.LineMsg{ID: s.id, Line: fmt.Sprintf("starting, will run for %v", s.duration)})

	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		started := time.Now()
		for {
			select {
			case <-ctx.Done():
				return
			case t := <-ticker.C:
				elapsed := t.Sub(started).Round(time.Second)
				step.Send(step.LineMsg{ID: s.id, Line: fmt.Sprintf("running %v / %v", elapsed, s.duration)})
			}
		}
	}()

	// Block until the configured duration elapses, keeping the step "running"
	// in the tracker so the progress bar animates for the full window.
	select {
	case <-ctx.Done():
		return nil
	case <-time.After(s.duration):
		return nil
	}
}

// slowStepTemplate returns a StepTemplate for a slow step with the given id
// and panel. A wizard field lets the user choose the duration (10s/30s/60s/90s).
// If waitFor is non-empty the step waits for those IDs before starting.
func slowStepTemplate(id, label string, panel tui.PanelID, waitFor []string) tui.StepTemplate {
	return tui.StepTemplate{
		ID:      id,
		Label:   label,
		Panel:   panel,
		WaitFor: waitFor,
		Fields: []tui.FieldSpec{
			{
				ID:          id + "_dur",
				Label:       label + " duration",
				Kind:        tui.FieldKindSelect,
				OptionsFunc: tui.StaticOptions("10s", "30s", "60s", "90s"),
				Default:     1, // 30s
			},
		},
		Build: func(v tui.WizardValues) (tui.Step, error) {
			raw := v.String(id + "_dur")
			secs, _ := strconv.Atoi(raw[:len(raw)-1])
			if secs <= 0 {
				secs = 30
			}
			return &slowStep{
				id:       id,
				duration: time.Duration(secs) * time.Second,
			}, nil
		},
	}
}

// sampleDir returns the directory containing this source file, so that
// relative paths like "skaffold.yaml" resolve correctly regardless of where
// the binary is invoked from.
func sampleDir() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "sample"
	}
	return filepath.Dir(file)
}

func main() {
	headless := flag.Bool("headless", false, "run in headless/CI mode")
	env := flag.String("env", "dev", "environment (dev, test)")
	apiPort := flag.String("api-port", "9001", "API port (9001, 9002, 9003)")
	runTests := flag.String("test", "", "test labels to run (comma-separated, or * for all)")
	failFast := flag.Bool("fail-fast", false, "cancel all steps on first failure")
	flag.Parse()

	// env step: contributes the "env" and "api_port" selector fields to
	// the wizard. It does not start a process of its own — the selected
	// values are read by the skaffold generate callback below.
	envStep := tui.StepTemplate{
		ID:     "env",
		Panel:  tui.PanelTopLeft,
		Label:  "Environment",
		Hidden: true,
		Fields: []tui.FieldSpec{
			{
				ID:          "env",
				Label:       "Environment",
				Kind:        tui.FieldKindSelect,
				OptionsFunc: tui.StaticOptions("dev", "test"),
				Default:     0,
			},
			{
				ID:          "api_port",
				Label:       "API Port",
				Kind:        tui.FieldKindSelect,
				OptionsFunc: tui.StaticOptions("9001", "9002", "9003"),
				Default:     0,
			},
		},
		Build: func(v tui.WizardValues) (tui.Step, error) {
			return nil, nil
		},
	}

	// components step: contributes the "components" field using the
	// systems/components hierarchy. Like the env step, it doesn't start
	// a process - the selected components are read by the skaffold
	// generate callback below.
	componentsStep := tui.StepTemplate{
		ID:     "components",
		Panel:  tui.PanelTopLeft,
		Label:  "Components",
		Hidden: true,
		Fields: []tui.FieldSpec{
			{
				ID:    "components",
				Label: "Components",
				Kind:  tui.FieldKindSystemSelect,
				SystemsFunc: func(v tui.WizardValues) []tui.System {
					return []tui.System{
						{
							Name: "checkout",
							Components: []tui.Component{
								{Name: "checkout-backend"},
								{Name: "checkout-bff"},
							},
						},
						{
							Name: "user",
							Components: []tui.Component{
								{Name: "user-service"},
								{Name: "user-bff"},
							},
						},
						{
							Name: "product",
							Components: []tui.Component{
								{Name: "product-service"},
								{Name: "product-bff"},
							},
						},
						{
							Name: "order",
							Components: []tui.Component{
								{Name: "order-service"},
								{Name: "order-bff"},
							},
						},
						{
							Name: "analytics",
							Components: []tui.Component{
								{Name: "analytics-backend"},
								{Name: "analytics-bff"},
							},
						},
					}
				},
			},
		},
		Build: func(v tui.WizardValues) (tui.Step, error) {
			return nil, nil
		},
	}

	// Skaffold pipeline: generates the skaffold.yaml once and shares it
	// between the build and dev steps. DevTemplate automatically waits
	// for BuildTemplate when both are registered.
	skaffoldPipeline := builtins.NewSkaffoldPipeline(
		func(v tui.WizardValues) (string, []string, error) {
			env := v.String("env")
			port := v.String("api_port")
			if port == "" {
				port = "9001"
			}
			return generateSkaffoldYAML(sampleDir(), env, port)
		},
	)

	mfeStep := builtins.MFETemplate(
		[]string{
			"checkout-mfe",
			"user-mfe",
			"product-mfe",
			"analytics-mfe",
		},
		// RunMFE maps every MFE name to the sample mfe/ directory.
		// The MFE calls GET /hello on the port-forwarded service and
		// displays the message returned by the Go server.
		func(name string, v tui.WizardValues) builtins.MFECommand {
			port := v.String("api_port")
			if port == "" {
				port = "9001"
			}
			return builtins.MFECommand{
				Cmd:  "node",
				Args: []string{"index.js"},
				Dir:  filepath.Join(sampleDir(), "mfe"),
				Env:  map[string]string{"API_BASE": "http://localhost:" + port},
			}
		},
	)

	// Two slow steps for testing progress bar behaviour:
	//   slow_a — starts immediately alongside other steps
	//   slow_b — waits for slow_a to finish, so it starts late in the timeline
	// Set slow_a to 30s and slow_b to 60s+ to observe the estimate extension
	// and shimmer starting from the correct bar position (not from t=0).
	slowA := slowStepTemplate("slow_a", "Slow A", tui.PanelTopLeft, nil)
	slowB := slowStepTemplate("slow_b", "Slow B", tui.PanelTopRight, []string{"slow_a"})

	steps := []tui.StepTemplate{
		builtins.MinikubeTemplate(),
		builtins.KubectlTemplate(),
		envStep,
		componentsStep,
		slowA,
		slowB,
	}
	steps = append(steps, skaffoldPipeline.Templates()...)
	steps = append(steps, mfeStep)

	cfg := tui.Config{
		Steps: steps,
		Tests: []tui.TestTemplate{
			{
				Label: "API",
				Build: func(v tui.WizardValues) (tui.TestCommand, error) {
					port := v.String("api_port")
					if port == "" {
						port = "9001"
					}
					return tui.TestCommand{
						Cmd:  "go",
						Args: []string{"test", "-v", "./..."},
						Dir:  sampleDir(),
						Env:  map[string]string{"API_BASE": "http://localhost:" + port},
					}, nil
				},
			},
		},
	}

	if *headless {
		var tests []string
		if *runTests != "" {
			tests = strings.Split(*runTests, ",")
		}
		err := tui.RunHeadless(cfg, tui.HeadlessOptions{
			Values: tui.NewWizardValues(
				map[string]string{"env": *env, "api_port": *apiPort},
				nil,
			),
			RunTests: tests,
			FailFast: *failFast,
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	if err := tui.Run(cfg); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
