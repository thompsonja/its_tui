package tui

import (
	"context"
	"fmt"
	"os"

	"github.com/thompsonja/its_tui/config"
)

// wizardValuesFromState reconstructs a WizardValues from a saved InstanceState.
func wizardValuesFromState(inst *InstanceState) WizardValues {
	str := inst.StringValues
	strs := inst.SliceValues
	if str == nil {
		str = map[string]string{}
	}
	if strs == nil {
		strs = map[string][]string{}
	}
	return WizardValues{str: str, strs: strs}
}

// switchToInstance cancels the current instance context, clears panel content,
// and sets the new instance name. Callers are responsible for calling
// registerPipeline and starting watchers / executeStart as needed.
func (m *model) switchToInstance(name string) {
	cancelInstance()
	instanceCtx, cancelInstance = context.WithCancel(context.Background())
	m.instanceName = name
	m.debugPorts = nil
	// Clear panel buffers (defs are reset by a subsequent registerPipeline call).
	for i := range m.panels {
		for j := range m.panels[i].bufs {
			m.panels[i].bufs[j] = nil
		}
		m.panels[i].activeIdx = 0
	}
	for i := range m.panelVPs {
		m.panelVPs[i].SetContent("")
	}
}

// buildPipelineFromState reconstructs the step graph from a saved InstanceState,
// used when restoring a previously-running instance after a restart.
func (m *model) buildPipelineFromState(instanceName string, inst *InstanceState) []StepDef {
	values := wizardValuesFromState(inst)
	defs, _ := m.buildDefsFromTemplates(values)
	return defs
}

// ResumeAction indicates what to do with a step on restart.
type ResumeAction string

const (
	ResumeActionSkip    ResumeAction = "skip"    // completed, don't restart
	ResumeActionRetry   ResumeAction = "retry"   // failed, try again
	ResumeActionRestart ResumeAction = "restart" // running when quit
	ResumeActionStart   ResumeAction = "start"   // pending/new
)

// determineResumeAction decides what to do with a step based on its saved state.
func determineResumeAction(stepID string, savedState map[string]StepState) ResumeAction {
	ss, exists := savedState[stepID]
	if !exists {
		fmt.Fprintf(os.Stderr, "[DEBUG determineResumeAction] step %s: not found in saved state\n", stepID)
		return ResumeActionStart
	}

	fmt.Fprintf(os.Stderr, "[DEBUG determineResumeAction] step %s: status=%s\n", stepID, ss.Status)

	switch ss.Status {
	case config.StepStatusCompleted:
		fmt.Fprintf(os.Stderr, "[DEBUG determineResumeAction] step %s: returning ResumeActionSkip\n", stepID)
		return ResumeActionSkip
	case config.StepStatusFailed:
		return ResumeActionRetry
	case config.StepStatusRunning:
		return ResumeActionRestart
	default:
		return ResumeActionStart
	}
}

// buildDefsFromTemplates builds a StepDef slice from all templates using values.
// On error it returns the error; callers that want best-effort (session restore)
// can ignore it.
func (m *model) buildDefsFromTemplates(values WizardValues) ([]StepDef, error) {
	sp := m.statePath
	var defs []StepDef

	// Clear and rebuild command registry
	m.customCommands = make(map[string]CommandSpec)

	for _, tmpl := range m.cfg.Steps {
		s, err := tmpl.Build(values)
		if err != nil {
			label := tmpl.Label
			if label == "" {
				label = "step"
			}
			return nil, fmt.Errorf("%s: %w", label, err)
		}
		if s == nil {
			continue
		}
		label := tmpl.Label
		if tmpl.LabelFunc != nil {
			label = tmpl.LabelFunc(values)
		}
		// Validate that Step.ID() matches template ID if template ID is set.
		if tmpl.ID != "" && s.ID() != tmpl.ID {
			return nil, fmt.Errorf("step %q: Step.ID() returned %q but template ID is %q",
				label, s.ID(), tmpl.ID)
		}

		// Register commands from this template
		for _, cmd := range tmpl.Commands {
			// Validate command
			if cmd.Name == "" {
				return nil, fmt.Errorf("step %q: command has empty Name", label)
			}
			if cmd.Handler == nil {
				return nil, fmt.Errorf("step %q: command %q has nil Handler", label, cmd.Name)
			}

			// Check for conflicts with other step commands
			if _, exists := m.customCommands[cmd.Name]; exists {
				return nil, fmt.Errorf("command name conflict: %q defined by multiple steps", cmd.Name)
			}

			// Check against built-in commands
			builtins := []string{"help", "start", "stop", "restart", "logs", "test", "theme"}
			for _, b := range builtins {
				if b == cmd.Name {
					return nil, fmt.Errorf("step %q: command %q conflicts with built-in command", label, cmd.Name)
				}
			}

			// Register command
			m.customCommands[cmd.Name] = cmd
		}

		var onReady func()
		if tmpl.OnReady != nil {
			fn := tmpl.OnReady
			onReady = func() { fn(sp) }
		}
		defs = append(defs, StepDef{
			Step: s,
			meta: stepMetadata{
				panel:        tmpl.Panel,
				label:        label,
				waitFor:      tmpl.WaitFor,
				autoActivate: tmpl.AutoActivate,
				hidden:       tmpl.Hidden,
				onReady:      onReady,
			},
		})
	}

	// Validate that all Step.ID() values are unique
	seenIDs := make(map[string]string) // maps ID -> label
	for _, def := range defs {
		id := def.Step.ID()
		if prevLabel, exists := seenIDs[id]; exists {
			return nil, fmt.Errorf("duplicate step ID %q (used by both %q and %q)",
				id, prevLabel, def.meta.label)
		}
		seenIDs[id] = def.meta.label
	}

	return defs, nil
}

// topoSortSteps returns a topologically sorted copy of defs.
// Steps with no dependencies come first, followed by steps that depend on them.
// Steps at the same dependency level maintain their original relative order.
// If there's a cycle, returns the original order.
func topoSortSteps(defs []StepDef) []StepDef {
	if len(defs) == 0 {
		return defs
	}

	// Build dependency graph and index mapping
	graph := make(map[string][]string) // id -> list of ids that depend on it
	inDegree := make(map[string]int)   // id -> count of unresolved dependencies
	idToIdx := make(map[string]int)    // id -> original index in defs
	allIDs := make(map[string]bool)    // set of all step IDs

	// First pass: collect all IDs
	for _, def := range defs {
		allIDs[def.Step.ID()] = true
	}

	// Second pass: build graph
	for i, def := range defs {
		id := def.Step.ID()
		idToIdx[id] = i
		if _, ok := inDegree[id]; !ok {
			inDegree[id] = 0
		}
		for _, dep := range def.meta.waitFor {
			// Only count dependencies that exist in this step set
			if allIDs[dep] {
				graph[dep] = append(graph[dep], id)
				inDegree[id]++
			}
		}
	}

	// Find all nodes with no dependencies, preserving original order
	var queue []string
	for _, def := range defs {
		id := def.Step.ID()
		if inDegree[id] == 0 {
			queue = append(queue, id)
		}
	}

	// Kahn's algorithm for topological sort
	var sorted []string
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		sorted = append(sorted, id)

		// Reduce in-degree for all dependents
		for _, dependent := range graph[id] {
			inDegree[dependent]--
			if inDegree[dependent] == 0 {
				queue = append(queue, dependent)
			}
		}
	}

	// If cycle detected (not all nodes processed), return original order
	if len(sorted) != len(defs) {
		return defs
	}

	// Build sorted defs slice
	result := make([]StepDef, len(defs))
	for i, id := range sorted {
		result[i] = defs[idToIdx[id]]
	}

	return result
}
