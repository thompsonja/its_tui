package step

// LineMsg appends one log line to the named step's panel buffer.
type LineMsg struct{ ID, Line string }

// SetMsg replaces the named step's panel buffer with new content.
type SetMsg struct {
	ID      string
	Content []string
}

// CommandMsg appends a line to the commands panel.
type CommandMsg struct{ Text string }

// PIDMsg carries the MFE process group ID for state persistence.
type PIDMsg struct{ PID int }

// DebugPortMsg is sent when skaffold debug establishes a port-forward for a
// container debug adapter. PortName identifies the runtime: "dlv" (Go),
// "jvm" (Java), "ptvsd"/"debugpy" (Python), "node" (Node.js).
type DebugPortMsg struct {
	LocalPort    int
	RemotePort   int
	ResourceName string
	PortName     string
	Address      string
}
