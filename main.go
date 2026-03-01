package main

import (
	"context"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
)

// prog is the global program handle used for sending messages from goroutines.
var prog *tea.Program

// instanceCtx / cancelInstance govern all background goroutines tied to the
// current instance. Calling cancelInstance() kills watchers and running
// processes; a fresh context is created when the instance is switched.
var (
	instanceCtx    context.Context
	cancelInstance context.CancelFunc
)

func main() {
	// Load config (missing file is not fatal).
	cfgPath := DefaultConfigPath()
	configs, err := LoadConfigs(cfgPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "config:", err)
	}

	// Load state so we can restore the last-used instance.
	statePath := DefaultStatePath()
	state, _ := LoadState(statePath)

	m := newModel()
	m.configs = configs
	m.statePath = statePath

	// Restore previously selected instance if one was saved.
	if state.CurrentInstance != "" {
		m.instance.Name = state.CurrentInstance
		// Instance already known — start with the full grid visible.
		m.fullscreenProgress = 0
		m.fullscreenTarget = 0
	}

	// Restore persisted command history for up/down navigation.
	if len(state.CommandHistory) > 0 {
		m.cmdHistory = append(m.cmdHistory, state.CommandHistory...)
	}

	// Restore saved theme.
	if state.Theme != "" {
		for _, t := range presets {
			if t.Name == state.Theme {
				currentTheme = t
				break
			}
		}
	}

	// Create the initial instance context before starting watchers.
	instanceCtx, cancelInstance = context.WithCancel(context.Background())

	p := tea.NewProgram(
		m,
		tea.WithAltScreen(),
	)
	prog = p

	go watchKubectl(instanceCtx, m.instance.Name)
	go watchMinikubeLog(instanceCtx, minikubeLogPath(m.instance.Name), m.instance.Name)
	go watchSkaffoldLog(instanceCtx, skaffoldLogPath(m.instance.Name), m.instance.Name)

	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
