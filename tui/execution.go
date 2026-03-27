package tui

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/thompsonja/its_tui/config"
	"github.com/thompsonja/its_tui/step"
)

// executeStartFromWizard handles the start command initiated from the wizard.
// It builds the pipeline from wizard selections and starts execution.
func (m *model) executeStartFromWizard() {
	wiz := m.wizard
	if wiz == nil {
		m.printLine("  internal error: wizard not initialized")
		return
	}
	sp := m.statePath
	name := m.configuredName()

	m.printLine("$ start")
	values := wiz.buildValues()
	defs, err := m.buildDefsFromTemplates(values)
	if err != nil {
		prog.Send(commandLineMsg("error: " + err.Error()))
		return
	}
	// Inject message sender into steps that support it, so they can be
	// tested without the global Send and send messages via prog.
	for _, def := range defs {
		if s, ok := def.Step.(step.Sender); ok {
			s.SetSender(func(msg any) { prog.Send(msg) })
		}
	}

	// Check if there's an existing instance state with step states (resume scenario)
	var existingStepStates map[string]StepState
	if state, err := LoadState(sp); err == nil && state.Instance != nil && len(state.Instance.StepStates) > 0 {
		existingStepStates = state.Instance.StepStates
	}

	// Persist wizard selections and set StartedAt immediately.
	// If resuming, keep existing step states; otherwise initialize all as pending.
	var stepStates map[string]StepState
	if existingStepStates != nil {
		stepStates = existingStepStates
	} else {
		stepStates = make(map[string]StepState)
		for _, def := range defs {
			stepStates[def.Step.ID()] = StepState{
				ID:     def.Step.ID(),
				Status: config.StepStatusPending,
			}
		}
	}
	// SaveInstanceState must run synchronously before executeStart/executeStartWithResume
	// spawn their goroutines. Those goroutines call UpdateStepState, and both functions
	// do a read-modify-write on the state file. If SaveInstanceState runs asynchronously
	// (after UpdateStepState(completed) has already fired), it overwrites the whole
	// Instance with the initial "pending" states — leaving the file wrong on TUI exit
	// and causing completed steps to be restarted on the next session.
	if err := SaveInstanceState(sp, InstanceState{
		StartedAt:    time.Now().UTC().Format(time.RFC3339),
		StringValues: values.str,
		SliceValues:  values.strs,
		StepStates:   stepStates,
	}); err != nil {
		prog.Send(commandLineMsg(fmt.Sprintf("  warning: failed to save state: %v", err)))
	}

	m.switchToInstance(name)
	m.activeDefs = defs // Store for use by stop command
	m.registerPipeline(defs)
	m.fullscreenTarget = 0

	// Choose execution path based on whether we're resuming
	if existingStepStates != nil {
		// Resume: use saved step states to skip/retry/restart
		m.executeStartWithResume(defs, existingStepStates)
	} else {
		// Fresh start: truncate logs and start all steps
		for _, def := range defs {
			if lp := def.Step.LogPath(name); lp != "" {
				_ = os.Truncate(lp, 0)
			}
		}
		m.executeStart(defs)
	}

	// Start watchers using the per-step contexts created by executeStart/executeStartWithResume.
	// Skip steps with PanelNone (no output destination).
	for _, def := range defs {
		if def.meta.panel == PanelNone {
			continue
		}
		id := def.Step.ID()
		if e, ok := m.stepCtxs[id]; ok {
			go step.WatchStep(e.ctx, def.Step, name)
		}
	}
}

// waitForDeps blocks until all deps in waitFor are ready or ctx is cancelled.
// For each dep that becomes ready it sends a stepDepReadyMsg for stepID.
// Returns true when all deps are satisfied, false if ctx was cancelled first.
func waitForDeps(ctx context.Context, stepID string, waitFor []string, ready map[string]chan struct{}) bool {
	if len(waitFor) == 0 {
		return true
	}
	remaining := make(chan struct{}, len(waitFor))
	for _, dep := range waitFor {
		dep := dep
		go func() {
			if ch, ok := ready[dep]; ok {
				select {
				case <-ch:
					prog.Send(stepDepReadyMsg{id: stepID, dep: dep})
					remaining <- struct{}{}
				case <-ctx.Done():
				}
			} else {
				remaining <- struct{}{}
			}
		}()
	}
	for range waitFor {
		select {
		case <-remaining:
		case <-ctx.Done():
			return false
		}
	}
	return true
}

// notifyDependentsOfFailure sends a stepDepFailedMsg to every step that lists
// failedID in its WaitFor.
func notifyDependentsOfFailure(defs []StepDef, failedID string) {
	for _, otherDef := range defs {
		for _, dep := range otherDef.meta.waitFor {
			if dep == failedID {
				prog.Send(stepDepFailedMsg{id: otherDef.Step.ID(), failedDep: failedID})
			}
		}
	}
}

// executeStart launches all step processes with dependency ordering.
// Steps with WaitFor set block until their dependency signals ready.
func (m *model) executeStart(defs []StepDef) {
	// Capture instanceCtx by value now to prevent a race if the global is
	// reassigned (stop/switch) while dep-waiting goroutines are still running.
	ctx := instanceCtx

	m.steps = map[string]*commandStep{}
	m.stepCtxs = make(map[string]stepEntry)
	name := m.instanceName

	// Initialize startup tracking
	m.completedSteps = 0
	m.totalSteps = 0
	for _, def := range defs {
		if !def.meta.hidden {
			m.totalSteps++
		}
	}

	// Create per-step contexts and build a ready channel for each step.
	ready := make(map[string]chan struct{}, len(defs))
	for _, def := range defs {
		id := def.Step.ID()
		ready[id] = make(chan struct{})
		stepCtx, stepCancel := context.WithCancel(ctx)
		m.stepCtxs[id] = stepEntry{ctx: stepCtx, cancel: stepCancel}
	}

	// Register visible steps in the commands panel tracker in topological order.
	sortedDefs := topoSortSteps(defs)
	for _, def := range sortedDefs {
		if def.meta.hidden {
			continue
		}
		label := def.effectiveLabel()
		if len(def.meta.waitFor) == 0 {
			m.startStep(def.Step.ID(), label)
		} else {
			m.startPendingStep(def.Step.ID(), label, def.meta.waitFor)
		}
	}

	for _, def := range defs {
		def := def
		id := def.Step.ID()
		stepCtx := m.stepCtxs[id].ctx
		go func() {
			if !waitForDeps(ctx, id, def.meta.waitFor, ready) {
				return
			}
			if len(def.meta.waitFor) > 0 {
				// Activate this step (triggers spinner + AutoActivate if set).
				prog.Send(stepActivateMsg{id: id})
			}

			// Mark step as running
			sp := m.statePath
			_ = UpdateStepState(sp, id, config.StepStatusRunning, nil)

			// Start the step.
			if err := def.Step.Start(stepCtx, name); err != nil {
				_ = UpdateStepState(sp, id, config.StepStatusFailed, err)
				close(ready[id])
				notifyDependentsOfFailure(defs, id)
				if !def.meta.hidden {
					prog.Send(stepDoneMsg{
						id:    id,
						ok:    false,
						label: def.effectiveLabel() + " failed: " + err.Error(),
					})
				}
				return
			}

			// Mark step as completed
			_ = UpdateStepState(sp, id, config.StepStatusCompleted, nil)
			close(ready[id])
			if def.meta.onReady != nil {
				go def.meta.onReady()
			}
			if !def.meta.hidden {
				prog.Send(stepDoneMsg{
					id:    id,
					ok:    true,
					label: def.effectiveLabel() + " running",
				})
			}
		}()
	}
}

// executeStartWithResume launches step processes with resume logic based on saved state.
// Steps that completed successfully are skipped, failed steps are retried, and steps
// that were running when the instance quit are restarted.
func (m *model) executeStartWithResume(defs []StepDef, savedStates map[string]StepState) {
	// Capture instanceCtx by value now to prevent a race if the global is
	// reassigned (stop/switch) while dep-waiting goroutines are still running.
	ctx := instanceCtx

	m.steps = map[string]*commandStep{}
	m.stepCtxs = make(map[string]stepEntry)
	name := m.instanceName
	sp := m.statePath

	// Initialize startup tracking
	m.completedSteps = 0
	m.totalSteps = 0
	for _, def := range defs {
		if !def.meta.hidden {
			m.totalSteps++
		}
	}

	// Create per-step contexts and build a ready channel for each step.
	ready := make(map[string]chan struct{}, len(defs))
	for _, def := range defs {
		id := def.Step.ID()
		ready[id] = make(chan struct{})
		stepCtx, stepCancel := context.WithCancel(ctx)
		m.stepCtxs[id] = stepEntry{ctx: stepCtx, cancel: stepCancel}
	}

	// Determine resume action for each step
	resumeActions := make(map[string]ResumeAction)
	for _, def := range defs {
		resumeActions[def.Step.ID()] = determineResumeAction(def.Step.ID(), savedStates)
	}

	// Register visible steps in the commands panel tracker in topological order.
	sortedDefs := topoSortSteps(defs)
	for _, def := range sortedDefs {
		if def.meta.hidden {
			continue
		}
		id := def.Step.ID()
		label := def.effectiveLabel()
		action := resumeActions[id]

		// Add suffix to label based on resume action
		switch action {
		case ResumeActionSkip:
			label = label + " (restored)"
		case ResumeActionRetry:
			label = label + " (retrying)"
		case ResumeActionRestart:
			label = label + " (restarting)"
		}

		if len(def.meta.waitFor) == 0 {
			m.startStep(id, label)
		} else {
			m.startPendingStep(id, label, def.meta.waitFor)
		}
	}

	for _, def := range defs {
		def := def
		id := def.Step.ID()
		action := resumeActions[id]
		stepCtx := m.stepCtxs[id].ctx

		go func() {
			if !waitForDeps(ctx, id, def.meta.waitFor, ready) {
				return
			}
			if len(def.meta.waitFor) > 0 {
				// Activate this step (triggers spinner + AutoActivate if set).
				prog.Send(stepActivateMsg{id: id})
			}

			// Handle based on resume action
			var err error
			wasAlreadyCompleted := false
			if action == ResumeActionSkip {
				// Step completed - try Resume() or skip if not implemented
				// Don't change state - it should stay Completed unless Resume() fails
				err = step.ResumeStep(stepCtx, def.Step, name, true)
				if err != nil {
					// Resume failed, need to restart - mark as running and call Start()
					_ = UpdateStepState(sp, id, config.StepStatusRunning, nil)
					err = def.Step.Start(stepCtx, name)
				} else {
					// Resume succeeded - step stays in Completed state
					wasAlreadyCompleted = true
				}
			} else {
				// Step needs restart (retry/restart/start)

				// Truncate log if restarting a previously-running step
				if action == ResumeActionRestart {
					if lp := def.Step.LogPath(name); lp != "" {
						_ = os.Truncate(lp, 0)
					}
				}

				// Clear error for retry
				if action == ResumeActionRetry {
					_ = UpdateStepState(sp, id, config.StepStatusPending, nil)
				}

				// Mark step as running and start/restart it
				_ = UpdateStepState(sp, id, config.StepStatusRunning, nil)
				err = def.Step.Start(stepCtx, name)
			}

			// Handle error from Start() or ResumeStep()
			if err != nil {
				_ = UpdateStepState(sp, id, config.StepStatusFailed, err)
				close(ready[id])
				notifyDependentsOfFailure(defs, id)
				if !def.meta.hidden {
					prog.Send(stepDoneMsg{
						id:    id,
						ok:    false,
						label: def.effectiveLabel() + " failed: " + err.Error(),
					})
				}
				return
			}

			// Mark step as completed (skip if already completed from successful resume)
			if !wasAlreadyCompleted {
				_ = UpdateStepState(sp, id, config.StepStatusCompleted, nil)
			}
			close(ready[id])
			if def.meta.onReady != nil {
				go def.meta.onReady()
			}
			if !def.meta.hidden {
				prog.Send(stepDoneMsg{
					id:    id,
					ok:    true,
					label: def.effectiveLabel() + " running",
				})
			}
		}()
	}
}
