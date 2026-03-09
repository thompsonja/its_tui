package tui

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"tui/config"
	"tui/step"
)

// ── Type aliases — package code uses these names without a config/step prefix ─

type (
	InstanceState  = config.InstanceState
	ComponentsFile = config.ComponentsFile
	System         = config.System
	Component      = config.Component

	MFECommand = step.MFECommand
	Step       = step.Step

	MinikubeStep = step.MinikubeStep
	KubectlStep  = step.KubectlStep
	SkaffoldStep = step.SkaffoldStep
	MFEStep      = step.MFEStep
)

// ── Function aliases ──────────────────────────────────────────────────────────

var (
	LoadComponents       = config.LoadComponents
	LoadState            = config.LoadState
	SaveState            = config.SaveState
	SaveInstanceState    = config.SaveInstanceState
	MarkActive           = config.MarkActive
	MarkInactive         = config.MarkInactive
	SaveMFEPGID          = config.SaveMFEPGID
	SaveTheme            = config.SaveTheme
	AppendCommandHistory = config.AppendCommandHistory
	DefaultStatePath     = config.DefaultStatePath
)

// ── Public API ────────────────────────────────────────────────────────────────

const defaultInstanceName = "Integration Test Suite"

// PanelID identifies one of the three content panels (not the Commands panel).
type PanelID int

const (
	PanelTopLeft     PanelID = iota // default: Minikube / kubectl
	PanelTopRight                   // default: Skaffold
	PanelBottomRight                // default: MFE
)

// StepDef wires a Step to a panel and describes its execution dependencies.
type StepDef struct {
	Step  Step    // the process to run
	Panel PanelID // which content panel receives this step's output

	// Label is shown in the commands panel step tracker.
	// If empty, the step's ID is used (capitalized).
	Label string

	// WaitFor is the ID of a step that must complete before this one starts.
	// An empty string means "start immediately".
	WaitFor string

	// AutoActivate, when true, switches the panel view to this step when it is
	// activated (i.e. when its WaitFor dependency completes).
	AutoActivate bool

	// Hidden, when true, suppresses this step from the commands panel tracker.
	Hidden bool

	// OnReady is called (in a goroutine) when the step's Start returns nil.
	OnReady func()
}

// effectiveLabel returns the display label for the step.
func (d StepDef) effectiveLabel() string {
	if d.Label != "" {
		return d.Label
	}
	id := d.Step.ID()
	if len(id) == 0 {
		return "step"
	}
	return strings.ToUpper(id[:1]) + id[1:]
}

// ── Wizard field types ────────────────────────────────────────────────────────

// FieldKind identifies the interaction style for a wizard field.
type FieldKind int

const (
	FieldKindSelect       FieldKind = iota // horizontal left/right selector (e.g. CPU, RAM)
	FieldKindSingleSelect                  // searchable single-choice picker (e.g. MFE)
	FieldKindMultiSelect                   // searchable multi-choice picker
	FieldKindSystemSelect                  // hierarchical system/component picker
)

// FieldSpec describes one user-configurable wizard field.
type FieldSpec struct {
	ID      string    // unique identifier; used as key in WizardValues
	Label   string    // display label
	Kind    FieldKind
	Options []string // choices for Select / SingleSelect / MultiSelect
	Systems []System // hierarchy for SystemSelect
	Default int      // for Select: index of the default option
}

// WizardValues holds the collected user selections from the wizard.
type WizardValues struct {
	str  map[string]string
	strs map[string][]string
}

// String returns the string value for the field with the given ID.
func (v WizardValues) String(id string) string { return v.str[id] }

// Strings returns the slice value for the field with the given ID.
func (v WizardValues) Strings(id string) []string { return v.strs[id] }

// ── Step template ─────────────────────────────────────────────────────────────

// StepTemplate wires a Step's pipeline placement, wizard fields, and factory
// together. Callers register templates via Config.Steps.
type StepTemplate struct {
	// ID is the canonical step identifier. It must match the value returned by
	// the Step built by Build. Used for validation and WaitFor resolution.
	ID string

	// Fields are the wizard configuration fields for this step.
	Fields []FieldSpec

	// Panel is the content panel this step's output is routed to.
	Panel PanelID

	// Label is shown in the commands panel step tracker.
	Label string

	// LabelFunc, if set, overrides Label using the final wizard values.
	LabelFunc func(WizardValues) string

	// WaitFor is the ID of a step that must be ready before this one starts.
	WaitFor string

	// AutoActivate switches the panel view to this step when activated.
	AutoActivate bool

	// Hidden suppresses this step from the commands panel tracker.
	Hidden bool

	// OnReady is called (in a goroutine) when the step's Start returns nil.
	// The statePath argument is the path to the TUI state file.
	OnReady func(statePath string)

	// Build constructs the Step from the collected wizard values.
	// Returning (nil, nil) skips this step entirely.
	Build func(WizardValues) (Step, error)

	// StopFunc, if set, is called during the stop command to clean up this
	// step's resources. Steps are stopped in reverse template order.
	StopFunc func(ctx context.Context, instanceName string)

	// StopLabel is shown in the commands panel while StopFunc runs.
	// Defaults to "stopping <Label>" if empty.
	StopLabel string
}

// ── Config ────────────────────────────────────────────────────────────────────

// Config holds the configuration provided by the caller when starting the TUI.
type Config struct {
	// InstanceName is the display name for the managed instance.
	// Defaults to "Integration Test Suite" if empty.
	InstanceName string

	// Steps is the ordered list of step templates that define the pipeline.
	Steps []StepTemplate
}

// ── Runtime globals ───────────────────────────────────────────────────────────

// prog is the global program handle used for sending messages from goroutines.
var prog *tea.Program

// instanceCtx / cancelInstance govern all background goroutines tied to the
// current instance. Calling cancelInstance() kills watchers and running
// processes; a fresh context is created when the instance is switched.
var (
	instanceCtx    context.Context
	cancelInstance context.CancelFunc
)

// validateTemplates checks the template list for structural problems that would
// cause silent failures at runtime. It is called at the top of Run.
func validateTemplates(steps []StepTemplate) error {
	knownIDs := make(map[string]bool, len(steps))
	for _, t := range steps {
		if t.Build == nil {
			label := t.Label
			if label == "" {
				label = "(unlabeled)"
			}
			return fmt.Errorf("template %q has nil Build function", label)
		}
		if t.Panel < PanelTopLeft || t.Panel > PanelBottomRight {
			return fmt.Errorf("template %q has invalid Panel %d", t.Label, int(t.Panel))
		}
		if t.ID != "" {
			if knownIDs[t.ID] {
				return fmt.Errorf("duplicate step ID %q", t.ID)
			}
			knownIDs[t.ID] = true
		}
	}
	// Validate WaitFor references only when every template has an ID registered.
	if len(steps) > 0 && len(knownIDs) == len(steps) {
		for _, t := range steps {
			if t.WaitFor != "" && !knownIDs[t.WaitFor] {
				return fmt.Errorf("template %q WaitFor=%q: no template with that ID", t.Label, t.WaitFor)
			}
		}
	}
	return nil
}

// Run starts the TUI with the given configuration. It blocks until the user
// exits and returns any error from the bubbletea runtime.
func Run(cfg Config) error {
	if err := validateTemplates(cfg.Steps); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	statePath := DefaultStatePath()
	state, _ := LoadState(statePath)

	m := newModel(cfg)
	m.statePath = statePath

	if state.Theme != "" {
		for _, t := range presets {
			if t.Name == state.Theme {
				currentTheme = t
				break
			}
		}
	}

	if len(state.CommandHistory) > 0 {
		m.cmdHistory = append(m.cmdHistory, state.CommandHistory...)
	}

	instanceCtx, cancelInstance = context.WithCancel(context.Background())

	// Session restore: set up the model before handing it to tea.NewProgram,
	// since NewProgram takes m by value and any mutations after that are lost.
	var restoreDefs []StepDef
	var restoreName string
	if state.Instance != nil && state.Instance.StartedAt != "" {
		restoreName = m.instanceName()
		m.instance.Name = restoreName
		m.fullscreenProgress = 0
		m.fullscreenTarget = 0
		restoreDefs = m.buildPipelineFromState(restoreName, state.Instance)
		m.registerPipeline(restoreDefs)
	}

	p := tea.NewProgram(m, tea.WithAltScreen())
	prog = p
	step.SetSender(func(msg any) { p.Send(msg) })

	// Start background watchers now that prog/Send are wired up.
	for _, def := range restoreDefs {
		go watchStep(instanceCtx, def, restoreName)
		go resumeStep(instanceCtx, def, restoreName)
	}

	if _, err := p.Run(); err != nil {
		return err
	}
	return nil
}
