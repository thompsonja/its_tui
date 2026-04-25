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
func (m *model) executeStartFromWizard() {
	debugLog("executeStartFromWizard: called")
	wiz := m.vs.wizard
	if wiz == nil {
		debugLog("executeStartFromWizard: wizard not initialized")
		m.printLine("  internal error: wizard not initialized")
		return
	}
	sp := m.app.statePath
	name := m.app.configuredName()

	debugLog("executeStartFromWizard: instance name=%q, statePath=%q", name, sp)
	m.printLine("$ start")
	debugLog("executeStartFromWizard: building wizard values")
	values := wiz.buildValues()
	debugLog("executeStartFromWizard: wizard values built, building defs from templates")
	defs, err := m.buildDefsFromTemplates(values)
	if err != nil {
		debugLog("executeStartFromWizard: buildDefsFromTemplates failed: %v", err)
		m.printLine(fmt.Sprintf("  error: %v", err))
		return
	}
	debugLog("executeStartFromWizard: built %d step definitions", len(defs))
	for _, def := range defs {
		if s, ok := def.Step.(step.Sender); ok {
			s.SetSender(func(msg any) { prog.Send(msg) })
		}
	}

	var existingStepStates map[string]StepState
	if state, err := LoadState(sp); err == nil && state.Instance != nil && len(state.Instance.StepStates) > 0 {
		existingStepStates = state.Instance.StepStates
		debugLog("executeStartFromWizard: found %d existing step states", len(existingStepStates))
		for sid, ss := range existingStepStates {
			debugLog("executeStartFromWizard: existing state: step=%q status=%s", sid, ss.Status)
		}
	} else {
		debugLog("executeStartFromWizard: no existing step states — fresh start")
	}

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
	// Preserve step_data written by Build() calls above (e.g. generator cache).
	// SaveInstanceState replaces the entire InstanceState, so we must carry it forward.
	var stepData map[string]map[string]string
	if state, err := LoadState(sp); err == nil && state.Instance != nil {
		stepData = state.Instance.StepData
	}
	if err := SaveInstanceState(sp, InstanceState{
		StartedAt:    time.Now().UTC().Format(time.RFC3339),
		StringValues: values.str,
		SliceValues:  values.strs,
		StepStates:   stepStates,
		StepData:     stepData,
	}); err != nil {
		prog.Send(commandLineMsg(fmt.Sprintf("  warning: failed to save state: %v", err)))
	}

	m.switchToInstance(name)
	m.app.activeDefs = defs
	m.app.registerPipeline(defs)
	m.vs.fullscreenTarget = 0

	if existingStepStates != nil {
		debugLog("executeStartFromWizard: calling executeStartWithResume (existing states present)")
		m.executeStartWithResume(defs, existingStepStates)
	} else {
		debugLog("executeStartFromWizard: calling executeStart (fresh start)")
		for _, def := range defs {
			if lp := def.Step.LogPath(name); lp != "" {
				if err := os.Truncate(lp, 0); err != nil {
					debugLog("executeStartFromWizard: truncate log %q: %v", lp, err)
				}
			}
		}
		m.executeStart(defs)
	}

	for _, def := range defs {
		if def.meta.panel == PanelNone {
			continue
		}
		id := def.Step.ID()
		if e, ok := m.app.stepCtxs[id]; ok {
			go step.WatchStep(e.ctx, def.Step, name)
		}
	}
}

// waitForDeps blocks until all deps are ready or ctx is cancelled.
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

// notifyDependentsOfFailure sends stepDepFailedMsg to every step waiting on failedID.
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
func (m *model) executeStart(defs []StepDef) {
	debugLog("executeStart: launching %d steps", len(defs))
	ctx := instanceCtx

	m.vs.steps = map[string]*commandStep{}
	m.app.stepCtxs = make(map[string]stepEntry)
	name := m.app.instanceName
	sp := m.app.statePath
	debugLog("executeStart: instance name=%q", name)

	m.app.completedSteps = 0
	m.app.totalSteps = 0
	for _, def := range defs {
		if !def.meta.hidden {
			m.app.totalSteps++
		}
	}

	ready := make(map[string]chan struct{}, len(defs))
	for _, def := range defs {
		id := def.Step.ID()
		ready[id] = make(chan struct{})
		stepCtx, stepCancel := context.WithCancel(ctx)
		m.app.stepCtxs[id] = stepEntry{ctx: stepCtx, cancel: stepCancel}
	}

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
		stepCtx := m.app.stepCtxs[id].ctx
		go func() {
			debugLog("executeStart: step %q: waiting for dependencies: %v", id, def.meta.waitFor)
			if !waitForDeps(ctx, id, def.meta.waitFor, ready) {
				debugLog("executeStart: step %q: dependency wait cancelled", id)
				return
			}
			debugLog("executeStart: step %q: dependencies ready, starting", id)
			if len(def.meta.waitFor) > 0 {
				prog.Send(stepActivateMsg{id: id})
			}

			if err := UpdateStepState(sp, id, config.StepStatusRunning, nil); err != nil {
				debugLog("executeStart: step %q: UpdateStepState running: %v", id, err)
			}

			debugLog("executeStart: step %q: calling Start()", id)
			if err := def.Step.Start(stepCtx, name); err != nil {
				debugLog("executeStart: step %q: Start() failed: %v", id, err)
				if stateErr := UpdateStepState(sp, id, config.StepStatusFailed, err); stateErr != nil {
					debugLog("executeStart: step %q: UpdateStepState failed: %v", id, stateErr)
				}
				close(ready[id])
				notifyDependentsOfFailure(defs, id)
				if !def.meta.hidden {
					prog.Send(stepDoneMsg{id: id, ok: false, label: def.effectiveLabel() + " failed: " + err.Error()})
				}
				return
			}

			debugLog("executeStart: step %q: Start() completed successfully", id)
			if err := UpdateStepState(sp, id, config.StepStatusCompleted, nil); err != nil {
				debugLog("executeStart: step %q: UpdateStepState completed: %v", id, err)
			}
			close(ready[id])
			if def.meta.onReady != nil {
				go def.meta.onReady()
			}
			if !def.meta.hidden {
				prog.Send(stepDoneMsg{id: id, ok: true, label: def.effectiveLabel() + " running"})
			}
		}()
	}
	debugLog("executeStart: all step goroutines launched")
}

// executeStartWithResume launches steps with resume logic based on saved state.
func (m *model) executeStartWithResume(defs []StepDef, savedStates map[string]StepState) {
	ctx := instanceCtx

	m.vs.steps = map[string]*commandStep{}
	m.app.stepCtxs = make(map[string]stepEntry)
	name := m.app.instanceName
	sp := m.app.statePath

	m.app.completedSteps = 0
	m.app.totalSteps = 0
	for _, def := range defs {
		if !def.meta.hidden {
			m.app.totalSteps++
		}
	}

	ready := make(map[string]chan struct{}, len(defs))
	for _, def := range defs {
		id := def.Step.ID()
		ready[id] = make(chan struct{})
		stepCtx, stepCancel := context.WithCancel(ctx)
		m.app.stepCtxs[id] = stepEntry{ctx: stepCtx, cancel: stepCancel}
	}

	resumeActions := make(map[string]ResumeAction)
	for _, def := range defs {
		resumeActions[def.Step.ID()] = determineResumeAction(def.Step.ID(), savedStates)
	}

	sortedDefs := topoSortSteps(defs)
	for _, def := range sortedDefs {
		if def.meta.hidden {
			continue
		}
		id := def.Step.ID()
		label := def.effectiveLabel()
		action := resumeActions[id]
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
		stepCtx := m.app.stepCtxs[id].ctx
		debugLog("executeStartWithResume: step %q action=%s", id, action)

		go func() {
			if !waitForDeps(ctx, id, def.meta.waitFor, ready) {
				debugLog("executeStartWithResume: step %q: dependency wait cancelled", id)
				return
			}
			if len(def.meta.waitFor) > 0 {
				prog.Send(stepActivateMsg{id: id})
			}

			var err error
			wasAlreadyCompleted := false
			debugLog("executeStartWithResume: step %q: executing action=%s", id, action)
			if action == ResumeActionSkip {
				err = step.ResumeStep(stepCtx, def.Step, name, true)
				if err != nil {
					debugLog("executeStartWithResume: step %q: ResumeStep returned err=%v — calling Start() directly", id, err)
					if stateErr := UpdateStepState(sp, id, config.StepStatusRunning, nil); stateErr != nil {
						debugLog("executeStartWithResume: step %q: UpdateStepState running: %v", id, stateErr)
					}
					err = def.Step.Start(stepCtx, name)
					debugLog("executeStartWithResume: step %q: Start() (after ResumeStep err) returned: %v", id, err)
				} else {
					debugLog("executeStartWithResume: step %q: ResumeStep succeeded — reattached", id)
					wasAlreadyCompleted = true
				}
			} else {
				if action == ResumeActionRestart {
					if lp := def.Step.LogPath(name); lp != "" {
						if truncErr := os.Truncate(lp, 0); truncErr != nil {
							debugLog("executeStartWithResume: step %q: truncate log %q: %v", id, lp, truncErr)
						}
					}
				}
				if action == ResumeActionRetry {
					if stateErr := UpdateStepState(sp, id, config.StepStatusPending, nil); stateErr != nil {
						debugLog("executeStartWithResume: step %q: UpdateStepState pending: %v", id, stateErr)
					}
				}
				if stateErr := UpdateStepState(sp, id, config.StepStatusRunning, nil); stateErr != nil {
					debugLog("executeStartWithResume: step %q: UpdateStepState running: %v", id, stateErr)
				}
				debugLog("executeStartWithResume: step %q: calling Start() directly (action=%s)", id, action)
				err = def.Step.Start(stepCtx, name)
				debugLog("executeStartWithResume: step %q: Start() returned: %v", id, err)
			}

			if err != nil {
				if stateErr := UpdateStepState(sp, id, config.StepStatusFailed, err); stateErr != nil {
					debugLog("executeStartWithResume: step %q: UpdateStepState failed: %v", id, stateErr)
				}
				close(ready[id])
				notifyDependentsOfFailure(defs, id)
				if !def.meta.hidden {
					prog.Send(stepDoneMsg{id: id, ok: false, label: def.effectiveLabel() + " failed: " + err.Error()})
				}
				return
			}

			if !wasAlreadyCompleted {
				if stateErr := UpdateStepState(sp, id, config.StepStatusCompleted, nil); stateErr != nil {
					debugLog("executeStartWithResume: step %q: UpdateStepState completed: %v", id, stateErr)
				}
			}
			close(ready[id])
			if def.meta.onReady != nil {
				go def.meta.onReady()
			}
			if !def.meta.hidden {
				prog.Send(stepDoneMsg{id: id, ok: true, label: def.effectiveLabel() + " running"})
			}
		}()
	}
}
