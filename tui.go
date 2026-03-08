package tui

import (
	"context"
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

// Config holds the configuration provided by the caller when starting the TUI.
type Config struct {
	// InstanceName is the display name for the single managed instance.
	// Defaults to "Integration Test Suite" if empty.
	InstanceName string

	// Systems is the list of systems and their components (backends + BFFs).
	Systems []System

	// GenerateSkaffold, if non-nil, is called with the selected component names
	// and should return the path to a skaffold.yaml to run.
	GenerateSkaffold func(components []string) (string, error)

	// MFEs is the list of available micro-frontends shown in the MFE picker.
	MFEs []string

	// RunMFE, if non-nil, is called with the selected MFE name to determine
	// how to run it.
	RunMFE func(mfe string) MFECommand
}

// mfeCommand returns the MFECommand for the given MFE name.
func (cfg Config) mfeCommand(mfe string) MFECommand {
	if mfe == "" {
		return MFECommand{}
	}
	if cfg.RunMFE != nil {
		return cfg.RunMFE(mfe)
	}
	return MFECommand{Cmd: "npm", Args: []string{"start"}, Dir: mfe}
}

// prog is the global program handle used for sending messages from goroutines.
var prog *tea.Program

// instanceCtx / cancelInstance govern all background goroutines tied to the
// current instance. Calling cancelInstance() kills watchers and running
// processes; a fresh context is created when the instance is switched.
var (
	instanceCtx    context.Context
	cancelInstance context.CancelFunc
)

// Run starts the TUI with the given configuration. It blocks until the user
// exits and returns any error from the bubbletea runtime.
func Run(cfg Config) error {
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
