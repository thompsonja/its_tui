package tui

import (
	"context"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
)

// MFECommand describes how to run a micro-frontend.
type MFECommand struct {
	Cmd  string   // executable name
	Args []string // arguments
	Dir  string   // working directory
}

// Config holds the configuration provided by the caller when starting the TUI.
type Config struct {
	// Systems is the list of systems and their components (backends + BFFs).
	// These are shown in the component picker when building a custom instance.
	Systems []System

	// GenerateSkaffold, if non-nil, is called with the selected component names
	// and should return the path to a skaffold.yaml to run.
	// If nil, skaffold will not be started from the custom wizard.
	GenerateSkaffold func(components []string) (string, error)

	// MFEs is the list of available micro-frontends shown in the MFE picker.
	MFEs []string

	// RunMFE, if non-nil, is called with the selected MFE name to determine
	// how to run it. If nil, defaults to {Cmd: "npm", Args: ["start"], Dir: mfe}.
	RunMFE func(mfe string) MFECommand
}

// mfeCommand returns the MFECommand for the given MFE name, using the
// configured callback or the npm default when RunMFE is nil.
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
	cfgPath := DefaultConfigPath()
	configs, err := LoadConfigs(cfgPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "config:", err)
	}

	statePath := DefaultStatePath()
	state, _ := LoadState(statePath)

	m := newModel(cfg)
	m.configs = configs
	m.statePath = statePath

	if state.CurrentInstance != "" {
		m.instance.Name = state.CurrentInstance
		m.fullscreenProgress = 0
		m.fullscreenTarget = 0
	}

	if len(state.CommandHistory) > 0 {
		m.cmdHistory = append(m.cmdHistory, state.CommandHistory...)
	}

	if state.Theme != "" {
		for _, t := range presets {
			if t.Name == state.Theme {
				currentTheme = t
				break
			}
		}
	}

	instanceCtx, cancelInstance = context.WithCancel(context.Background())

	p := tea.NewProgram(m, tea.WithAltScreen())
	prog = p

	go watchKubectl(instanceCtx, m.instance.Name)
	go watchStep(instanceCtx, &MinikubeStep{}, m.instance.Name)
	go watchStep(instanceCtx, &SkaffoldStep{}, m.instance.Name)
	go watchStep(instanceCtx, &MFEStep{}, m.instance.Name)

	if _, err := p.Run(); err != nil {
		return err
	}
	return nil
}
