package tui

import (
	"context"
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/thompsonja/its_tui/config"
	"github.com/thompsonja/its_tui/step"
)

// ── Type aliases — package code uses these names without a config/step prefix ─

type (
	InstanceState  = config.InstanceState
	DebugPort      = config.DebugPort
	ComponentsFile = config.ComponentsFile
	System         = config.System
	Component      = config.Component
	StepStatus     = config.StepStatus
	StepState      = config.StepState

	Step = step.Step
)

// ── Function aliases ──────────────────────────────────────────────────────────

var (
	LoadComponents       = config.LoadComponents
	LoadState            = config.LoadState
	SaveState            = config.SaveState
	SaveInstanceState    = config.SaveInstanceState
	MarkActive           = config.MarkActive
	MarkInactive         = config.MarkInactive
	MarkReady            = config.MarkReady
	SaveMFEPGID          = config.SaveMFEPGID
	SaveDebugPorts       = config.SaveDebugPorts
	SavePorts            = config.SavePorts
	SaveTheme            = config.SaveTheme
	AppendCommandHistory = config.AppendCommandHistory
	UpdateStepState      = config.UpdateStepState
	SaveStepData         = config.SaveStepData
	LoadStepData         = config.LoadStepData
	GetStepData          = config.GetStepData
	SavePanelTabs        = config.SavePanelTabs
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

	PanelNone PanelID = -1 // no panel assignment; step runs but produces no visible output
)

// stepMetadata holds execution configuration for a step.
type stepMetadata struct {
	panel        PanelID
	label        string
	waitFor      []string
	autoActivate bool
	hidden       bool
	onReady      func()
}

// StepDef wires a Step to a panel and describes its execution dependencies.
type StepDef struct {
	Step Step         // the process to run
	meta stepMetadata // execution metadata
}

// effectiveLabel returns the display label for the step.
func (d StepDef) effectiveLabel() string {
	if d.meta.label != "" {
		return d.meta.label
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
	FieldKindText                          // free-text input field
)

// FieldSpec describes one user-configurable wizard field.
type FieldSpec struct {
	ID          string // unique identifier; used as key in WizardValues
	Label       string // display label
	Kind        FieldKind
	OptionsFunc func(WizardValues) []string // provides choices for Select / SingleSelect / MultiSelect
	SystemsFunc func(WizardValues) []System // provides hierarchy for SystemSelect
	Default     int                         // for Select: index of the default option
}

// StaticOptions returns an OptionsFunc that always returns the given options.
// Use for fields whose options never change based on other selections.
func StaticOptions(opts ...string) func(WizardValues) []string {
	return func(WizardValues) []string { return opts }
}

// StaticSystems returns a SystemsFunc that always returns the given systems.
// Use for system hierarchies that never change based on other selections.
func StaticSystems(systems ...System) func(WizardValues) []System {
	return func(WizardValues) []System { return systems }
}

// WizardValues holds the collected user selections from the wizard.
type WizardValues struct {
	str       map[string]string
	strs      map[string][]string
	isRestore bool // true when called from the session-restore path, not the wizard
}

// String returns the string value for the field with the given ID.
func (v WizardValues) String(id string) string { return v.str[id] }

// Strings returns the slice value for the field with the given ID.
func (v WizardValues) Strings(id string) []string { return v.strs[id] }

// IsRestore reports whether Build() is being called during a session restore
// (TUI restart with existing state) rather than from a fresh wizard submission.
// Generator templates can use this to skip re-running expensive generation
// that would produce the same output as the previous run.
func (v WizardValues) IsRestore() bool { return v.isRestore }

// NewWizardValues constructs a WizardValues from explicit maps.
// Useful in tests and in Build functions that delegate to sub-builders.
// Nil maps are treated as empty.
func NewWizardValues(str map[string]string, strs map[string][]string) WizardValues {
	if str == nil {
		str = map[string]string{}
	}
	if strs == nil {
		strs = map[string][]string{}
	}
	return WizardValues{str: str, strs: strs}
}

// ── Step template ─────────────────────────────────────────────────────────────

// CommandSpec describes a custom command contributed by a step template.
type CommandSpec struct {
	// Name is the command name as typed by users (e.g., "rebuild", "status").
	Name string

	// Help is a brief description shown in the help overlay.
	Help string

	// Handler is called when the command is invoked.
	// - args: command-line arguments after the command name
	// - instanceName: current running instance name (empty if no instance)
	// - values: wizard values from the current/last instance
	Handler func(args []string, instanceName string, values WizardValues) error
}

// StepTemplate wires a Step's pipeline placement, wizard fields, and factory
// together. Callers register templates via Config.Steps.
type StepTemplate struct {
	// ID is the canonical step identifier. It must match the value returned by
	// the Step built by Build. Used for validation and WaitFor resolution.
	ID string

	// Fields are the wizard configuration fields for this step.
	Fields []FieldSpec

	// Panel is the content panel this step's output is routed to.
	// Use PanelNone for steps that run but produce no visible output.
	Panel PanelID

	// Label is shown in the commands panel step tracker.
	Label string

	// LabelFunc, if set, overrides Label using the final wizard values.
	LabelFunc func(WizardValues) string

	// WaitFor is a list of IDs of steps that must be ready before this one starts.
	WaitFor []string

	// AutoActivate switches the panel view to this step when activated.
	AutoActivate bool

	// Hidden suppresses this step from the commands panel tracker.
	Hidden bool

	// OnReady is called (in a goroutine) when the step's Start returns nil.
	// The statePath argument is the path to the TUI state file.
	OnReady func(statePath string)

	// Commands are optional custom commands contributed by this step.
	// Available as soon as the step is configured in the pipeline.
	Commands []CommandSpec

	// Build constructs the Step from the collected wizard values.
	// Returning (nil, nil) skips this step entirely.
	Build func(WizardValues) (Step, error)
}

// ── Test templates ────────────────────────────────────────────────────────────

// TestCommand describes a test process to run against the running instance.
type TestCommand struct {
	Cmd  string
	Args []string
	Dir  string
	Env  map[string]string // merged with os.Environ
}

// TestTemplate describes one runnable test suite.
// Tests appear as a virtual "Tests" tab on the BottomRight panel and are
// triggered via the `test [label]` REPL command.
type TestTemplate struct {
	// Label identifies the test suite in the REPL and the tab title.
	Label string

	// Build constructs the TestCommand from the current wizard values.
	Build func(WizardValues) (TestCommand, error)
}

// ── Config ────────────────────────────────────────────────────────────────────

// Config holds the configuration provided by the caller when starting the TUI.
type Config struct {
	// InstanceName is the display name for the managed instance.
	// Defaults to "Integration Test Suite" if empty.
	InstanceName string

	// Steps is the ordered list of step templates that define the pipeline.
	Steps []StepTemplate

	// Tests is the optional list of test suites runnable via the `test` command.
	// When non-empty a virtual "Tests" tab appears on the BottomRight panel.
	Tests []TestTemplate

	// StatusLine, if set, is called each frame to produce the top bar text.
	// The argument is the currently running instance name (empty when stopped).
	// Defaults to showing the instance name, or "no instance running" when empty.
	StatusLine func(instanceName string) string

	// LogDir is the root directory for log files.
	// Defaults to "/tmp" if empty.
	LogDir string

	// WorkspaceDir is the project root used to locate the .vscode folder for
	// automatic launch.json management. Defaults to the current working
	// directory when empty. Launch configs are only written/removed when a
	// .vscode directory already exists at this path.
	WorkspaceDir string
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

// cancelTest stops any in-progress test run. Initialised to a no-op so it is
// always safe to call without a nil check.
var cancelTest context.CancelFunc = func() {}

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
		if t.Panel != PanelNone && (t.Panel < PanelTopLeft || t.Panel > PanelBottomRight) {
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
			for _, dep := range t.WaitFor {
				if !knownIDs[dep] {
					return fmt.Errorf("template %q WaitFor=%q: no template with that ID", t.Label, dep)
				}
			}
		}
	}
	// Validate that FieldSpec.Kind is consistent with the required func fields.
	// FieldKindSelect/SingleSelect/MultiSelect require OptionsFunc.
	// FieldKindSystemSelect requires SystemsFunc.
	for _, t := range steps {
		for _, f := range t.Fields {
			switch f.Kind {
			case FieldKindSelect, FieldKindSingleSelect, FieldKindMultiSelect:
				if f.OptionsFunc == nil {
					return fmt.Errorf("template %q field %q: Kind %v requires OptionsFunc to be non-nil", t.Label, f.ID, f.Kind)
				}
			case FieldKindSystemSelect:
				if f.SystemsFunc == nil {
					return fmt.Errorf("template %q field %q: FieldKindSystemSelect requires SystemsFunc to be non-nil", t.Label, f.ID)
				}
			}
		}
	}
	return nil
}

func validateTests(tests []TestTemplate) error {
	for _, t := range tests {
		if t.Build == nil {
			label := t.Label
			if label == "" {
				label = "(unlabeled)"
			}
			return fmt.Errorf("test template %q has nil Build function", label)
		}
	}
	return nil
}

// PrintCommand sends a line to the commands panel. This is exposed for use by
// builtin step templates and custom command handlers.
func PrintCommand(line string) {
	if prog != nil {
		prog.Send(commandLineMsg(line))
	}
}

// Run starts the TUI with the given configuration. It blocks until the user
// exits and returns any error from the bubbletea runtime.
func Run(cfg Config) error {
	// Initialize debug logging first
	if err := initDebugLog(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to initialize debug log: %v\n", err)
	}
	defer closeDebugLog()

	debugLog("Run: starting with config: InstanceName=%q, Steps=%d, Tests=%d",
		cfg.InstanceName, len(cfg.Steps), len(cfg.Tests))

	if err := validateTemplates(cfg.Steps); err != nil {
		debugLog("Run: template validation failed: %v", err)
		return fmt.Errorf("invalid configuration: %w", err)
	}
	if err := validateTests(cfg.Tests); err != nil {
		debugLog("Run: test validation failed: %v", err)
		return fmt.Errorf("invalid configuration: %w", err)
	}

	// Configure log directory (defaults to ~/.tui/logs if not specified)
	if err := config.SetLogDir(cfg.LogDir); err != nil {
		debugLog("Run: failed to set log directory: %v", err)
		return fmt.Errorf("invalid log directory: %w", err)
	}

	// Ensure log directory exists
	if err := config.EnsureLogDir(); err != nil {
		debugLog("Run: failed to create log directory: %v", err)
		return fmt.Errorf("failed to create log directory: %w", err)
	}
	debugLog("Run: log directory set to: %s", config.GetLogDir())

	statePath := DefaultStatePath()
	debugLog("Run: loading state from: %s", statePath)
	state, _ := LoadState(statePath)

	debugLog("Run: creating model")
	m := newModel(cfg)
	m.app.statePath = statePath

	if state.Theme != "" {
		for _, t := range presets {
			if t.Name == state.Theme {
				currentTheme = t
				break
			}
		}
	}

	if len(state.CommandHistory) > 0 {
		m.app.cmdHistory = append(m.app.cmdHistory, state.CommandHistory...)
	}

	instanceCtx, cancelInstance = context.WithCancel(context.Background())

	// Session restore: set up the model before handing it to tea.NewProgram,
	// since NewProgram takes m by value and any mutations after that are lost.
	var restoreDefs []StepDef
	var restoreName string
	if state.Instance != nil && state.Instance.StartedAt != "" {
		restoreName = m.app.configuredName()
		m.app.instanceName = restoreName
		m.vs.fullscreenProgress = 0
		m.vs.fullscreenTarget = 0
		restoreDefs = m.buildPipelineFromState(restoreName, state.Instance)
		m.app.activeDefs = restoreDefs
		m.app.registerPipeline(restoreDefs)

		// Restore active tab indices for each panel
		for i := 0; i < 3; i++ {
			if state.Instance.PanelTabs[i] < len(m.app.panels[i].defs) {
				m.app.panels[i].activeIdx = state.Instance.PanelTabs[i]
			}
		}

		for _, dp := range state.Instance.ForwardedPorts {
			m.app.fwdPorts = append(m.app.fwdPorts, step.DebugPortMsg{
				LocalPort:    dp.LocalPort,
				RemotePort:   dp.RemotePort,
				ResourceName: dp.ResourceName,
				PortName:     dp.PortName,
				Address:      dp.Address,
			})
		}
		for _, dp := range state.Instance.DebugPorts {
			m.app.debugPorts = append(m.app.debugPorts, step.DebugPortMsg{
				LocalPort:    dp.LocalPort,
				RemotePort:   dp.RemotePort,
				ResourceName: dp.ResourceName,
				PortName:     dp.PortName,
				Address:      dp.Address,
			})
		}
	}

	debugLog("Run: creating bubbletea program")
	p := tea.NewProgram(m, tea.WithAltScreen())
	prog = p
	sender := func(msg any) { p.Send(msg) }
	step.SetSender(sender)
	debugLog("Run: program created, sender configured")

	// Inject sender into restored steps.
	for _, def := range restoreDefs {
		if s, ok := def.Step.(step.Sender); ok {
			s.SetSender(sender)
		}
	}

	// Restore the instance using executeStartWithResume to properly handle step states.
	// This checks saved state for each step and skips/retries/restarts as appropriate.
	if len(restoreDefs) > 0 && state.Instance != nil && state.Instance.StepStates != nil {
		debugLog("Run: restoring session with %d steps (startup path)", len(restoreDefs))
		for sid, ss := range state.Instance.StepStates {
			debugLog("Run: restore state: step=%q status=%s", sid, ss.Status)
		}
		m.executeStartWithResume(restoreDefs, state.Instance.StepStates)

		// Start watchers for all steps with visible panels.
		for _, def := range restoreDefs {
			if def.meta.panel == PanelNone {
				continue
			}
			id := def.Step.ID()
			if e, ok := m.app.stepCtxs[id]; ok {
				go step.WatchStep(e.ctx, def.Step, restoreName)
			}
		}

		// Automatically show status on resume
		go func() {
			p.Send(autoStatusMsg{})
		}()
	}

	debugLog("Run: starting bubbletea program")
	if _, err := p.Run(); err != nil {
		debugLog("Run: program exited with error: %v", err)
		return err
	}
	debugLog("Run: program exited normally")
	return nil
}
