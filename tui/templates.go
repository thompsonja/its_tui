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
			{ID: "cpu", Label: "CPU", Kind: FieldKindSelect, OptionsFunc: StaticOptions("2", "4", "8", "16"), Default: 1},
			{ID: "ram", Label: "RAM", Kind: FieldKindSelect, OptionsFunc: StaticOptions("2g", "4g", "8g", "16g"), Default: 1},
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
		WaitFor:      []string{"minikube"},
		AutoActivate: true,
		Hidden:       true,
		OnReady:      func(sp string) { _ = MarkActive(sp) },
		Build:        func(v WizardValues) (Step, error) { return &KubectlStep{}, nil },
	}
}

// SkaffoldTemplate returns a StepTemplate for skaffold.
//
// generate is called with the full wizard values and should return the path to
// a skaffold.yaml and an optional list of skaffold profiles to activate.
// Returning an empty path skips the step; returning an error aborts the wizard.
// The wizard values include the "components" ([]string) and "mode" fields
// contributed by this template, as well as any fields from other templates in
// the pipeline (e.g. an "env" field from a companion step).
//
// systemsfunc provides the hierarchical system/component data shown in the wizard.
// It is called at wizard-open and after every field change, enabling dynamic
// updates based on other field selections.
func SkaffoldTemplate(generate func(v WizardValues) (path string, profiles []string, err error), systemsfunc func(WizardValues) []System) StepTemplate {
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
		WaitFor: []string{"minikube"},
		Fields: []FieldSpec{
			{ID: "components", Label: "Components", Kind: FieldKindSystemSelect, SystemsFunc: systemsfunc},
			{ID: "mode", Label: "Mode", Kind: FieldKindSelect, OptionsFunc: StaticOptions("dev", "run", "debug"), Default: 0},
		},
		Build: func(v WizardValues) (Step, error) {
			if generate == nil {
				return nil, nil
			}
			mode := v.String("mode")
			if mode == "" {
				mode = "dev"
			}
			path, profiles, err := generate(v)
			if err != nil {
				return nil, err
			}
			if path == "" {
				return nil, nil
			}
			return &SkaffoldStep{Path: path, Mode: mode, Profiles: profiles}, nil
		},
	}
}

// MFETemplate returns a StepTemplate for a micro-frontend runner.
//
// mfes is the list of available MFE names shown in the single-select picker.
// run is called with the selected MFE name and the full wizard values (so port
// fields or other selections can be read); if nil, defaults to "npm start" in
// the MFE name directory.
func MFETemplate(mfes []string, run func(name string, v WizardValues) MFECommand) StepTemplate {
	return StepTemplate{
		ID:    "mfe",
		Panel: PanelBottomRight,
		Label: "MFE",
		Fields: []FieldSpec{
			{ID: "mfe", Label: "MFE", Kind: FieldKindSingleSelect, OptionsFunc: StaticOptions(mfes...)},
		},
		Build: func(v WizardValues) (Step, error) {
			mfe := v.String("mfe")
			if mfe == "" {
				return nil, nil
			}
			var cmd MFECommand
			if run != nil {
				cmd = run(mfe, v)
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
