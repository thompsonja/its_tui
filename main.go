package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
)

// prog is the global program handle used for sending messages from goroutines.
var prog *tea.Program

func main() {
	m := newModel()
	p := tea.NewProgram(
		m,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(), // enables mousewheel scrolling in viewports
	)
	prog = p

	// Start long-running background watchers.
	go watchKubectl(m.instance.Name)

	go watchSkaffoldLog("/tmp/skaffold.log", m.instance.Name)

	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
