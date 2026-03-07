package tui

// Instance describes a named development environment that coordinates
// a minikube cluster, a skaffold pipeline, and an npm frontend process.
type Instance struct {
	Name string
}

// StatusLine returns the string rendered into the single-line top bar.
// Keep it short — it has to fit in one terminal row.
func (inst Instance) StatusLine() string {
	if inst.Name == "" {
		return "no instance selected"
	}
	return inst.Name
}
