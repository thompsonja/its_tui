package tui

import "github.com/charmbracelet/bubbles/viewport"

func (m model) View() string {
	return m.vs.render(m.app)
}

// isDebugPortName returns true when portName indicates a debug-protocol port.
func isDebugPortName(portName string) bool {
	switch portName {
	case "dlv", "jvm", "ptvsd", "debugpy", "node", "nodejs":
		return true
	}
	return false
}

// appendToVP appends line to buf, syncs content to vp, and scrolls to bottom.
func appendToVP(buf *[]string, vp *viewport.Model, line string) {
	*buf = appendLine(*buf, line)
	vp.SetContent(wrapContent(*buf, vp.Width))
	vp.GotoBottom()
}
