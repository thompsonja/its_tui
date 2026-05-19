package tui

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/thompsonja/its_tui/config"
	"github.com/thompsonja/its_tui/step"
)

// HeadlessOptions configures headless/CI execution.
type HeadlessOptions struct {
	// Values provides the wizard field values that would normally come from
	// interactive selection. Construct with NewWizardValues().
	Values WizardValues

	// Formatter controls CI output formatting. If nil, auto-detects:
	// GitHubActionsFormatter when $GITHUB_ACTIONS is set, PlainFormatter otherwise.
	Formatter CIFormatter

	// RunTests is a list of test labels to execute after all steps complete.
	// Empty means skip tests. Use "*" to run all configured tests.
	RunTests []string

	// FailFast cancels all remaining steps when any step fails
	// (in addition to the normal dependency cascade).
	FailFast bool

	// Stdout and Stderr override the default output writers.
	Stdout io.Writer
	Stderr io.Writer
}

func headlessStdout(opts HeadlessOptions) io.Writer {
	if opts.Stdout != nil {
		return opts.Stdout
	}
	return os.Stdout
}

// RunHeadless executes the step pipeline and optional tests without a terminal UI.
// It returns a non-nil error if any step or test fails.
func RunHeadless(cfg Config, opts HeadlessOptions) error {
	if err := validateTemplates(cfg.Steps); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}
	if err := validateTests(cfg.Tests); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	if err := config.SetLogDir(cfg.LogDir); err != nil {
		return fmt.Errorf("invalid log directory: %w", err)
	}
	if err := config.EnsureLogDir(); err != nil {
		return fmt.Errorf("failed to create log directory: %w", err)
	}

	formatter := opts.Formatter
	if formatter == nil {
		w := headlessStdout(opts)
		if os.Getenv("GITHUB_ACTIONS") == "true" {
			formatter = NewGitHubActionsFormatter(w)
		} else {
			formatter = NewPlainFormatter(w)
		}
	}

	statePath := DefaultStatePath()
	defs, _, err := buildDefs(cfg.Steps, opts.Values, statePath)
	if err != nil {
		return fmt.Errorf("build steps: %w", err)
	}
	if len(defs) == 0 {
		return fmt.Errorf("no steps to execute")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		cancel()
	}()

	msgs := make(chan any, 256)
	send := func(msg any) {
		select {
		case msgs <- msg:
		case <-ctx.Done():
		}
	}
	step.SetSender(send)

	for _, def := range defs {
		if s, ok := def.Step.(step.Sender); ok {
			s.SetSender(send)
		}
	}

	instanceName := cfg.InstanceName
	if instanceName == "" {
		instanceName = "headless"
	}

	stepStates := make(map[string]StepState)
	for _, def := range defs {
		stepStates[def.Step.ID()] = StepState{
			ID:     def.Step.ID(),
			Status: config.StepStatusPending,
		}
	}
	if err := SaveInstanceState(statePath, InstanceState{
		StartedAt:    time.Now().UTC().Format(time.RFC3339),
		StringValues: opts.Values.str,
		SliceValues:  opts.Values.strs,
		StepStates:   stepStates,
	}); err != nil {
		return fmt.Errorf("save state: %w", err)
	}

	for _, def := range defs {
		if lp := def.Step.LogPath(instanceName); lp != "" {
			_ = os.Truncate(lp, 0)
		}
	}

	headlessExecuteSteps(ctx, defs, instanceName, statePath, send, formatter)

	stepErr := runHeadlessEventLoop(ctx, cancel, defs, msgs, formatter, statePath, opts.FailFast)

	formatter.Summary(defs, instanceName)

	if stepErr == nil && len(opts.RunTests) > 0 {
		stepErr = runHeadlessTests(ctx, cfg, opts, formatter)
	}

	_ = MarkInactive(statePath)

	return stepErr
}

func headlessExecuteSteps(ctx context.Context, defs []StepDef, instanceName, statePath string, send func(any), formatter CIFormatter) {
	sortedDefs := topoSortSteps(defs)

	ready := make(map[string]chan struct{}, len(defs))
	for _, def := range defs {
		ready[def.Step.ID()] = make(chan struct{})
	}

	for _, def := range sortedDefs {
		def := def
		id := def.Step.ID()

		if lp := def.Step.LogPath(instanceName); lp != "" {
			go step.WatchStep(ctx, def.Step, instanceName)
		}

		go func() {
			if !waitForDeps(ctx, id, def.meta.waitFor, ready, send) {
				return
			}
			send(stepActivateMsg{id: id})

			if err := UpdateStepState(statePath, id, config.StepStatusRunning, nil); err != nil {
				debugLog("headless: step %q: UpdateStepState running: %v", id, err)
			}

			if err := def.Step.Start(ctx, instanceName); err != nil {
				_ = UpdateStepState(statePath, id, config.StepStatusFailed, err)
				close(ready[id])
				notifyDependentsOfFailure(defs, id, send)
				if !def.meta.hidden {
					send(stepDoneMsg{id: id, ok: false, label: def.effectiveLabel() + " failed: " + err.Error()})
				}
				return
			}

			_ = UpdateStepState(statePath, id, config.StepStatusCompleted, nil)
			close(ready[id])
			if def.meta.onReady != nil {
				go def.meta.onReady()
			}
			if !def.meta.hidden {
				send(stepDoneMsg{id: id, ok: true, label: def.effectiveLabel() + " completed"})
			}
		}()
	}
}

func runHeadlessEventLoop(ctx context.Context, cancel context.CancelFunc, defs []StepDef, msgs chan any, formatter CIFormatter, statePath string, failFast bool) error {
	startTime := time.Now()
	stepStartTimes := make(map[string]time.Time)
	completedCount := 0
	totalCount := 0
	var firstErr error

	defsByID := make(map[string]StepDef)
	for _, def := range defs {
		defsByID[def.Step.ID()] = def
		if !def.meta.hidden {
			totalCount++
		}
	}

	if totalCount == 0 {
		return nil
	}

	for completedCount < totalCount {
		select {
		case <-ctx.Done():
			if firstErr != nil {
				return firstErr
			}
			return ctx.Err()
		case msg := <-msgs:
			switch msg := msg.(type) {
			case step.LineMsg:
				formatter.StepOutput(msg.ID, msg.Line)
			case step.SetMsg:
				for _, line := range msg.Content {
					formatter.StepOutput(msg.ID, line)
				}
			case step.CommandMsg:
				formatter.StepOutput("cmd", msg.Text)
			case stepActivateMsg:
				stepStartTimes[msg.id] = time.Now()
				def := defsByID[msg.id]
				formatter.StepStarted(msg.id, def.effectiveLabel())
			case stepDoneMsg:
				def := defsByID[msg.id]
				startedAt, ok := stepStartTimes[msg.id]
				if !ok {
					startedAt = startTime
				}
				dur := time.Since(startedAt)
				if msg.ok {
					formatter.StepCompleted(msg.id, def.effectiveLabel(), dur)
				} else {
					formatter.StepFailed(msg.id, def.effectiveLabel(), msg.label, dur)
					if firstErr == nil {
						firstErr = fmt.Errorf("step %s failed: %s", msg.id, msg.label)
					}
					if failFast {
						cancel()
					}
				}
				if !def.meta.hidden {
					completedCount++
				}
			case stepDepFailedMsg:
				def := defsByID[msg.id]
				reason := fmt.Sprintf("dependency %s failed", msg.failedDep)
				formatter.StepSkipped(msg.id, def.effectiveLabel(), reason)
				_ = UpdateStepState(statePath, msg.id, config.StepStatusSkipped, fmt.Errorf("%s", reason))
				if !def.meta.hidden {
					completedCount++
				}
			case stepDepReadyMsg:
				// informational
			}
		}
	}

	if firstErr == nil {
		formatter.AllStepsCompleted(time.Since(startTime))
		_ = MarkReady(statePath)
	}
	return firstErr
}

func runHeadlessTests(ctx context.Context, cfg Config, opts HeadlessOptions, formatter CIFormatter) error {
	var testsToRun []TestTemplate

	if len(opts.RunTests) == 1 && opts.RunTests[0] == "*" {
		testsToRun = cfg.Tests
	} else {
		labelMap := make(map[string]TestTemplate)
		for _, t := range cfg.Tests {
			labelMap[t.Label] = t
		}
		for _, label := range opts.RunTests {
			if label == "" {
				continue
			}
			t, ok := labelMap[label]
			if !ok {
				return fmt.Errorf("unknown test label: %q", label)
			}
			testsToRun = append(testsToRun, t)
		}
	}

	if len(testsToRun) == 0 {
		return nil
	}

	var firstErr error
	for _, tmpl := range testsToRun {
		tc, err := tmpl.Build(opts.Values)
		if err != nil {
			return fmt.Errorf("test %q build failed: %w", tmpl.Label, err)
		}
		if tc.Cmd == "" {
			continue
		}

		formatter.TestStarted(tmpl.Label)
		start := time.Now()

		cmd := exec.CommandContext(ctx, tc.Cmd, tc.Args...)
		cmd.Dir = tc.Dir
		if len(tc.Env) > 0 {
			cmd.Env = os.Environ()
			for k, v := range tc.Env {
				cmd.Env = append(cmd.Env, k+"="+v)
			}
		}

		passed := true
		step.StreamCmd(ctx, cmd, func(line string) {
			if strings.HasPrefix(line, "[exited:") {
				passed = false
			}
			formatter.TestOutput(line)
		})

		if ctx.Err() != nil {
			formatter.TestCompleted(tmpl.Label, false, time.Since(start))
			return ctx.Err()
		}

		dur := time.Since(start)
		formatter.TestCompleted(tmpl.Label, passed, dur)
		if !passed && firstErr == nil {
			firstErr = fmt.Errorf("test %q failed", tmpl.Label)
		}
	}
	return firstErr
}
