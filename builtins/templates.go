package builtins

import (
	"fmt"

	"github.com/thompsonja/its_tui/config"
	"github.com/thompsonja/its_tui/step"
	"github.com/thompsonja/its_tui/tui"
)

// MinikubeTemplate returns a StepTemplate for starting a minikube cluster.
// It contributes CPU and RAM selector fields to the wizard.
// Additional args can be passed to minikube start command via the args parameter.
func MinikubeTemplate(args ...string) tui.StepTemplate {
	return tui.StepTemplate{
		ID:    "minikube",
		Panel: tui.PanelTopLeft,
		Label: "Minikube",
		Fields: []tui.FieldSpec{
			{ID: "cpu", Label: "CPU", Kind: tui.FieldKindSelect, OptionsFunc: tui.StaticOptions("2", "4", "8", "16"), Default: 1},
			{ID: "ram", Label: "RAM", Kind: tui.FieldKindSelect, OptionsFunc: tui.StaticOptions("2g", "4g", "8g", "16g"), Default: 1},
		},
		Build: func(v tui.WizardValues) (step.Step, error) {
			cpu := v.String("cpu")
			if cpu == "" {
				cpu = "4"
			}
			ram := v.String("ram")
			if ram == "" {
				ram = "4g"
			}
			return &MinikubeStep{CPU: cpu, RAM: ram, Args: args}, nil
		},
	}
}

// KubectlTemplate returns a StepTemplate for the kubectl pod watcher.
// It has no wizard fields: it starts automatically after minikube is ready
// and auto-activates its panel.
func KubectlTemplate() tui.StepTemplate {
	return tui.StepTemplate{
		ID:           "kubectl",
		Panel:        tui.PanelTopLeft,
		Label:        "kubectl",
		WaitFor:      []string{"minikube"},
		AutoActivate: true,
		Hidden:       true,
		Build: func(v tui.WizardValues) (step.Step, error) {
			return &KubectlStep{StatePath: config.DefaultStatePath()}, nil
		},
	}
}

// SkaffoldTemplate returns a StepTemplate for skaffold.
//
// generate is called with the full wizard values and should return the path to
// a skaffold.yaml and an optional list of skaffold profiles to activate.
// Returning an empty path skips the step; returning an error aborts the wizard.
// The wizard values include the "mode" field contributed by this template, as
// well as any fields from other templates in the pipeline (e.g. an "env" field
// from a companion step, or "components" from a custom selection step).
//
// If you need component selection, add a separate Hidden step with a
// FieldKindSystemSelect field (see the "env" step pattern in sample/main.go).
func SkaffoldTemplate(generate func(v tui.WizardValues) (path string, profiles []string, err error)) tui.StepTemplate {
	return tui.StepTemplate{
		ID:    "skaffold",
		Panel: tui.PanelTopRight,
		LabelFunc: func(v tui.WizardValues) string {
			mode := v.String("mode")
			if mode == "" {
				mode = "dev"
			}
			return "Skaffold (" + mode + ")"
		},
		WaitFor: []string{"minikube"},
		Fields: []tui.FieldSpec{
			{ID: "mode", Label: "Mode", Kind: tui.FieldKindSelect, OptionsFunc: tui.StaticOptions("dev", "run", "debug"), Default: 0},
		},
		Commands: []tui.CommandSpec{
			{
				Name: "status",
				Help: "show skaffold deployment status",
				Handler: func(args []string, instanceName string, values tui.WizardValues) error {
					if instanceName == "" {
						tui.PrintCommand("  no instance running - use: start")
						return nil
					}
					mode := values.String("mode")
					if mode == "" {
						mode = "dev"
					}
					tui.PrintCommand(fmt.Sprintf("  skaffold running in %s mode", mode))
					tui.PrintCommand(fmt.Sprintf("  instance: %s", instanceName))
					return nil
				},
			},
		},
		Build: func(v tui.WizardValues) (step.Step, error) {
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

// SkaffoldBuildTemplate returns a StepTemplate for skaffold build.
//
// generate is called with the full wizard values and should return the path to
// a skaffold.yaml and an optional list of skaffold profiles to activate.
// Returning an empty path skips the step; returning an error aborts the wizard.
// The wizard values include the "mode" field contributed by this template, as
// well as any fields from other templates in the pipeline (e.g. an "env" field
// from a companion step, or "components" from a custom selection step).
//
// If you need component selection, add a separate Hidden step with a
// FieldKindSystemSelect field (see the "env" step pattern in sample/main.go).
func SkaffoldBuildTemplate(generate func(v tui.WizardValues) (path string, profiles []string, err error)) tui.StepTemplate {
	return tui.StepTemplate{
		ID:    "skaffold_build",
		Panel: tui.PanelTopRight,
		LabelFunc: func(v tui.WizardValues) string {
			return "Skaffold (build)"
		},
		WaitFor: []string{"minikube"},
		Commands: []tui.CommandSpec{
			{
				Name: "status",
				Help: "show skaffold build status",
				Handler: func(args []string, instanceName string, values tui.WizardValues) error {
					if instanceName == "" {
						tui.PrintCommand("  no instance running - use: start")
						return nil
					}
					tui.PrintCommand("  skaffold running in build mode")
					tui.PrintCommand(fmt.Sprintf("  instance: %s", instanceName))
					return nil
				},
			},
		},
		Build: func(v tui.WizardValues) (step.Step, error) {
			if generate == nil {
				return nil, nil
			}
			path, profiles, err := generate(v)
			if err != nil {
				return nil, err
			}
			if path == "" {
				return nil, nil
			}
			return &SkaffoldStep{Path: path, Mode: "build", Profiles: profiles}, nil
		},
	}
}

// MFETemplate returns a StepTemplate for a micro-frontend runner.
//
// mfes is the list of available MFE names shown in the single-select picker.
// run is called with the selected MFE name and the full wizard values (so port
// fields or other selections can be read); if nil, defaults to "npm start" in
// the MFE name directory.
func MFETemplate(mfes []string, run func(name string, v tui.WizardValues) MFECommand) tui.StepTemplate {
	return tui.StepTemplate{
		ID:    "mfe",
		Panel: tui.PanelBottomRight,
		Label: "MFE",
		Fields: []tui.FieldSpec{
			{ID: "mfe", Label: "MFE", Kind: tui.FieldKindSingleSelect, OptionsFunc: tui.StaticOptions(mfes...)},
		},
		Build: func(v tui.WizardValues) (step.Step, error) {
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
