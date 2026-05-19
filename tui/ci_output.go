package tui

import (
	"fmt"
	"io"
	"sync"
	"time"
)

// CIFormatter controls how step progress and results are written to stdout
// during headless/CI execution.
type CIFormatter interface {
	StepStarted(stepID, label string)
	StepOutput(stepID, line string)
	StepCompleted(stepID, label string, duration time.Duration)
	StepFailed(stepID, label string, errMsg string, duration time.Duration)
	StepSkipped(stepID, label string, reason string)
	TestStarted(label string)
	TestOutput(line string)
	TestCompleted(label string, passed bool, duration time.Duration)
	AllStepsCompleted(totalDuration time.Duration)
	Summary(defs []StepDef, instanceName string)
}

// GitHubActionsFormatter outputs using GitHub Actions workflow commands.
type GitHubActionsFormatter struct {
	w  io.Writer
	mu sync.Mutex
}

// NewGitHubActionsFormatter creates a formatter for GitHub Actions output.
func NewGitHubActionsFormatter(w io.Writer) *GitHubActionsFormatter {
	return &GitHubActionsFormatter{w: w}
}

func (f *GitHubActionsFormatter) StepStarted(stepID, label string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fmt.Fprintf(f.w, "::group::[%s] %s\n", stepID, label)
}

func (f *GitHubActionsFormatter) StepOutput(stepID, line string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fmt.Fprintf(f.w, "[%s] %s\n", stepID, line)
}

func (f *GitHubActionsFormatter) StepCompleted(stepID, label string, dur time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fmt.Fprintf(f.w, "::endgroup::\n")
	fmt.Fprintf(f.w, "::notice::[%s] %s completed in %s\n", stepID, label, dur.Round(time.Millisecond))
}

func (f *GitHubActionsFormatter) StepFailed(stepID, label string, errMsg string, dur time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fmt.Fprintf(f.w, "::endgroup::\n")
	fmt.Fprintf(f.w, "::error::[%s] %s failed after %s: %s\n", stepID, label, dur.Round(time.Millisecond), errMsg)
}

func (f *GitHubActionsFormatter) StepSkipped(stepID, label string, reason string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fmt.Fprintf(f.w, "::warning::[%s] %s skipped: %s\n", stepID, label, reason)
}

func (f *GitHubActionsFormatter) TestStarted(label string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fmt.Fprintf(f.w, "::group::Test: %s\n", label)
}

func (f *GitHubActionsFormatter) TestOutput(line string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fmt.Fprintln(f.w, line)
}

func (f *GitHubActionsFormatter) TestCompleted(label string, passed bool, dur time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fmt.Fprintf(f.w, "::endgroup::\n")
	if passed {
		fmt.Fprintf(f.w, "::notice::Test %s passed in %s\n", label, dur.Round(time.Millisecond))
	} else {
		fmt.Fprintf(f.w, "::error::Test %s failed after %s\n", label, dur.Round(time.Millisecond))
	}
}

func (f *GitHubActionsFormatter) AllStepsCompleted(totalDur time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fmt.Fprintf(f.w, "::notice::All steps completed in %s\n", totalDur.Round(time.Millisecond))
}

func (f *GitHubActionsFormatter) Summary(defs []StepDef, instanceName string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fmt.Fprintf(f.w, "::group::Log files\n")
	for _, def := range defs {
		if lp := def.Step.LogPath(instanceName); lp != "" {
			fmt.Fprintf(f.w, "  %s: %s\n", def.Step.ID(), lp)
		}
	}
	fmt.Fprintf(f.w, "::endgroup::\n")
}

// PlainFormatter outputs plain text with step ID prefixes.
type PlainFormatter struct {
	w  io.Writer
	mu sync.Mutex
}

// NewPlainFormatter creates a plain text formatter.
func NewPlainFormatter(w io.Writer) *PlainFormatter {
	return &PlainFormatter{w: w}
}

func (f *PlainFormatter) StepStarted(stepID, label string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fmt.Fprintf(f.w, "[%s] Starting: %s\n", stepID, label)
}

func (f *PlainFormatter) StepOutput(stepID, line string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fmt.Fprintf(f.w, "[%s] > %s\n", stepID, line)
}

func (f *PlainFormatter) StepCompleted(stepID, label string, dur time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fmt.Fprintf(f.w, "[%s] Completed: %s (%s)\n", stepID, label, dur.Round(time.Millisecond))
}

func (f *PlainFormatter) StepFailed(stepID, label string, errMsg string, dur time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fmt.Fprintf(f.w, "[%s] FAILED: %s (%s): %s\n", stepID, label, dur.Round(time.Millisecond), errMsg)
}

func (f *PlainFormatter) StepSkipped(stepID, label string, reason string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fmt.Fprintf(f.w, "[%s] Skipped: %s (%s)\n", stepID, label, reason)
}

func (f *PlainFormatter) TestStarted(label string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fmt.Fprintf(f.w, "--- TEST: %s ---\n", label)
}

func (f *PlainFormatter) TestOutput(line string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fmt.Fprintln(f.w, line)
}

func (f *PlainFormatter) TestCompleted(label string, passed bool, dur time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if passed {
		fmt.Fprintf(f.w, "--- PASS: %s (%s) ---\n", label, dur.Round(time.Millisecond))
	} else {
		fmt.Fprintf(f.w, "--- FAIL: %s (%s) ---\n", label, dur.Round(time.Millisecond))
	}
}

func (f *PlainFormatter) AllStepsCompleted(totalDur time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fmt.Fprintf(f.w, "All steps completed in %s\n", totalDur.Round(time.Millisecond))
}

func (f *PlainFormatter) Summary(defs []StepDef, instanceName string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fmt.Fprintln(f.w, "Log files:")
	for _, def := range defs {
		if lp := def.Step.LogPath(instanceName); lp != "" {
			fmt.Fprintf(f.w, "  %s: %s\n", def.Step.ID(), lp)
		}
	}
}
