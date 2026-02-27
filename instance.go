package main

// Instance describes a named development environment that coordinates
// a minikube cluster, a skaffold pipeline, and an npm frontend process.
//
// TODO: wire up Start/Stop/Status once process orchestration is implemented.
type Instance struct {
	Name string

	// Extend with per-process state as needed, for example:
	//
	//   Minikube ProcessState
	//   Skaffold ProcessState
	//   Frontend ProcessState
}

// ProcessState holds the runtime status of one managed subprocess.
//
// type ProcessState struct {
// 	Running bool
// 	PID     int
// 	Err     error
// }

// StatusLine returns the string rendered into the single-line top bar.
// Keep it short — it has to fit in one terminal row.
func (inst Instance) StatusLine() string {
	if inst.Name == "" {
		return "no instance selected"
	}

	// TODO: append live process indicators once state is tracked, e.g.:
	//   return fmt.Sprintf("%s   minikube ●  skaffold ●  frontend ●", inst.Name)
	return inst.Name
}
