package tui

import "context"

// MinikubeTemplate returns a StepTemplate for starting a minikube cluster.
// It contributes CPU and RAM selector fields to the wizard, and provides a
// StopFunc that runs minikube delete during the stop command.
func MinikubeTemplate() StepTemplate {
	return StepTemplate{
		ID:        "minikube",
		Panel:     PanelTopLeft,
		Label:     "Minikube",
		StopLabel: "deleting cluster",
		StopFunc: func(ctx context.Context, name string) {
			_ = (&MinikubeStep{}).Stop(ctx, name)
		},
		Fields: []FieldSpec{
			{ID: "cpu", Label: "CPU", Kind: FieldKindSelect, Options: []string{"2", "4", "8", "16"}, Default: 1},
			{ID: "ram", Label: "RAM", Kind: FieldKindSelect, Options: []string{"2g", "4g", "8g", "16g"}, Default: 1},
		},
		Build: func(v WizardValues) (Step, error) {
			cpu := v.String("cpu")
			if cpu == "" {
				cpu = "4"
			}
			ram := v.String("ram")
			if ram == "" {
				ram = "4g"
			}
			return &MinikubeStep{CPU: cpu, RAM: ram}, nil
		},
	}
}

// KubectlTemplate returns a StepTemplate for the kubectl pod watcher.
// It has no wizard fields: it starts automatically after minikube is ready,
// auto-activates its panel, and calls MarkActive when ready.
func KubectlTemplate() StepTemplate {
	return StepTemplate{
		ID:           "kubectl",
		Panel:        PanelTopLeft,
		Label:        "kubectl",
		WaitFor:      "minikube",
		AutoActivate: true,
		Hidden:       true,
		OnReady:      func(sp string) { _ = MarkActive(sp) },
		Build:        func(v WizardValues) (Step, error) { return &KubectlStep{}, nil },
	}
}

// SkaffoldTemplate returns a StepTemplate for skaffold.
//
// generate is called with the selected component names and mode ("dev", "run",
// or "debug") and should return the path to a skaffold.yaml. Returning an
// empty path skips the step; returning an error aborts the wizard.
//
// systems provides the hierarchical system/component data shown in the wizard.
func SkaffoldTemplate(generate func(components []string, mode string) (string, error), systems []System) StepTemplate {
	return StepTemplate{
		ID:    "skaffold",
		Panel: PanelTopRight,
		LabelFunc: func(v WizardValues) string {
			mode := v.String("mode")
			if mode == "" {
				mode = "dev"
			}
			return "Skaffold (" + mode + ")"
		},
		WaitFor: "minikube",
		Fields: []FieldSpec{
			{ID: "components", Label: "Components", Kind: FieldKindSystemSelect, Systems: systems},
			{ID: "mode", Label: "Mode", Kind: FieldKindSelect, Options: []string{"dev", "run", "debug"}, Default: 0},
		},
		Build: func(v WizardValues) (Step, error) {
			if generate == nil {
				return nil, nil
			}
			mode := v.String("mode")
			if mode == "" {
				mode = "dev"
			}
			path, err := generate(v.Strings("components"), mode)
			if err != nil {
				return nil, err
			}
			if path == "" {
				return nil, nil
			}
			return &SkaffoldStep{Path: path, Mode: mode}, nil
		},
	}
}

// MFETemplate returns a StepTemplate for a micro-frontend runner.
//
// mfes is the list of available MFE names shown in the single-select picker.
// run is called with the selected name; if nil, defaults to "npm start" in the
// MFE name directory.
func MFETemplate(mfes []string, run func(name string) MFECommand) StepTemplate {
	return StepTemplate{
		ID:    "mfe",
		Panel: PanelBottomRight,
		Label: "MFE",
		Fields: []FieldSpec{
			{ID: "mfe", Label: "MFE", Kind: FieldKindSingleSelect, Options: mfes},
		},
		Build: func(v WizardValues) (Step, error) {
			mfe := v.String("mfe")
			if mfe == "" {
				return nil, nil
			}
			var cmd MFECommand
			if run != nil {
				cmd = run(mfe)
			} else {
				cmd = MFECommand{Cmd: "npm", Args: []string{"start"}, Dir: mfe}
			}
			if cmd.Cmd == "" {
				return nil, nil
			}
			return &MFEStep{Cmd: cmd}, nil
		},
	}
}
