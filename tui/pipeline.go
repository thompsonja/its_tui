package tui

import (
	"context"
	"fmt"

	"github.com/thompsonja/its_tui/config"
)

// wizardValuesFromState reconstructs a WizardValues from a saved InstanceState.
// The returned values have IsRestore()==true so Build() implementations can
// skip re-running generation that would produce identical output.
func wizardValuesFromState(inst *InstanceState) WizardValues {
	str := inst.StringValues
	strs := inst.SliceValues
	if str == nil {
		str = map[string]string{}
	}
	if strs == nil {
		strs = map[string][]string{}
	}
	return WizardValues{str: str, strs: strs, isRestore: true}
}

// switchToInstance cancels the current instance context, clears panel content,
// and sets the new instance name.
func (m *model) switchToInstance(name string) {
	cancelInstance()
	instanceCtx, cancelInstance = context.WithCancel(context.Background())
	m.app.inst.name = name
	m.app.debugPorts = nil
	for i := range m.app.panels {
		for j := range m.app.panels[i].bufs {
			m.app.panels[i].bufs[j] = nil
		}
		m.app.panels[i].activeIdx = 0
	}
	for i := range m.vs.panelVPs {
		m.vs.panelVPs[i].SetContent("")
	}
}

// buildPipelineFromState reconstructs the step graph from a saved InstanceState.
func (m *model) buildPipelineFromState(instanceName string, inst *InstanceState) []StepDef {
	values := wizardValuesFromState(inst)
	defs, _ := m.buildDefsFromTemplates(values)
	return defs
}

// ResumeAction indicates what to do with a step on restart.
type ResumeAction string

const (
	ResumeActionSkip    ResumeAction = "skip"
	ResumeActionRetry   ResumeAction = "retry"
	ResumeActionRestart ResumeAction = "restart"
	ResumeActionStart   ResumeAction = "start"
)

func determineResumeAction(stepID string, savedState map[string]StepState) ResumeAction {
	ss, exists := savedState[stepID]
	if !exists {
		return ResumeActionStart
	}
	switch ss.Status {
	case config.StepStatusCompleted:
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
func (m *model) buildDefsFromTemplates(values WizardValues) ([]StepDef, error) {
	debugLog("buildDefsFromTemplates: starting with %d templates", len(m.app.cfg.Steps))
	sp := m.app.statePath
	var defs []StepDef

	m.app.customCmds = make(map[string]CommandSpec)

	for i, tmpl := range m.app.cfg.Steps {
		templateLabel := tmpl.Label
		if templateLabel == "" {
			templateLabel = tmpl.ID
		}
		if templateLabel == "" {
			templateLabel = fmt.Sprintf("template[%d]", i)
		}
		debugLog("buildDefsFromTemplates: building template %d: %q", i, templateLabel)
		s, err := tmpl.Build(values)
		debugLog("buildDefsFromTemplates: template %d (%q) Build() returned", i, templateLabel)
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
		if tmpl.ID != "" && s.ID() != tmpl.ID {
			return nil, fmt.Errorf("step %q: Step.ID() returned %q but template ID is %q",
				label, s.ID(), tmpl.ID)
		}

		for _, cmd := range tmpl.Commands {
			if cmd.Name == "" {
				return nil, fmt.Errorf("step %q: command has empty Name", label)
			}
			if cmd.Handler == nil {
				return nil, fmt.Errorf("step %q: command %q has nil Handler", label, cmd.Name)
			}
			if _, exists := m.app.customCmds[cmd.Name]; exists {
				return nil, fmt.Errorf("command name conflict: %q defined by multiple steps", cmd.Name)
			}
			builtins := []string{"help", "start", "stop", "restart", "logs", "status", "test", "theme"}
			for _, b := range builtins {
				if b == cmd.Name {
					return nil, fmt.Errorf("step %q: command %q conflicts with built-in command", label, cmd.Name)
				}
			}
			m.app.customCmds[cmd.Name] = cmd
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

	seenIDs := make(map[string]string)
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
func topoSortSteps(defs []StepDef) []StepDef {
	if len(defs) == 0 {
		return defs
	}

	graph := make(map[string][]string)
	inDegree := make(map[string]int)
	idToIdx := make(map[string]int)
	allIDs := make(map[string]bool)

	for _, def := range defs {
		allIDs[def.Step.ID()] = true
	}

	for i, def := range defs {
		id := def.Step.ID()
		idToIdx[id] = i
		if _, ok := inDegree[id]; !ok {
			inDegree[id] = 0
		}
		for _, dep := range def.meta.waitFor {
			if allIDs[dep] {
				graph[dep] = append(graph[dep], id)
				inDegree[id]++
			}
		}
	}

	var queue []string
	for _, def := range defs {
		id := def.Step.ID()
		if inDegree[id] == 0 {
			queue = append(queue, id)
		}
	}

	var sorted []string
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		sorted = append(sorted, id)
		for _, dependent := range graph[id] {
			inDegree[dependent]--
			if inDegree[dependent] == 0 {
				queue = append(queue, dependent)
			}
		}
	}

	if len(sorted) != len(defs) {
		return defs
	}

	result := make([]StepDef, len(defs))
	for i, id := range sorted {
		result[i] = defs[idToIdx[id]]
	}
	return result
}
