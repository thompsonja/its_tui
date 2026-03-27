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

// SkaffoldConfig holds the generated skaffold file path and profiles.
// It is used to share configuration between SkaffoldFileGeneratorTemplate
// and multiple skaffold step templates (build, dev, run, debug).
type SkaffoldConfig struct {
	Path     string
	Profiles []string
}

// SkaffoldFileGeneratorTemplate generates a skaffold.yaml file once during Build()
// and stores the result in the provided SkaffoldConfig pointer.
//
// This is typically used as a hidden step that runs before other skaffold steps,
// allowing multiple steps (e.g., build + dev) to reference the same generated file.
//
// generate is called with the full wizard values and should return the path to
// the generated skaffold.yaml and an optional list of skaffold profiles to activate.
// Returning an empty path or error will be reported during wizard validation.
//
// Example:
//
//	cfg := &builtins.SkaffoldConfig{}
//	templates := []tui.StepTemplate{
//	    builtins.SkaffoldFileGeneratorTemplate(cfg, func(v tui.WizardValues) (string, []string, error) {
//	        return generateSkaffoldYAML(v.String("env"), v.String("port"))
//	    }),
//	    builtins.SkaffoldBuildTemplateFrom(cfg),
//	    builtins.SkaffoldTemplateFrom(cfg),
//	}
func SkaffoldFileGeneratorTemplate(cfg *SkaffoldConfig, generate func(v tui.WizardValues) (path string, profiles []string, err error)) tui.StepTemplate {
	return tui.StepTemplate{
		ID:     "skaffold_generator",
		Panel:  tui.PanelTopLeft,
		Label:  "Skaffold Config",
		Hidden: true,
		Build: func(v tui.WizardValues) (step.Step, error) {
			if generate == nil {
				return nil, fmt.Errorf("skaffold generator: generate function is nil")
			}
			path, profiles, err := generate(v)
			if err != nil {
				return nil, fmt.Errorf("skaffold generator: %w", err)
			}
			if path == "" {
				return nil, fmt.Errorf("skaffold generator: generate returned empty path")
			}
			cfg.Path = path
			cfg.Profiles = profiles
			return nil, nil // no step to run, just populate the config
		},
	}
}

// SkaffoldTemplateFrom returns a StepTemplate for skaffold that reads its
// configuration from a shared SkaffoldConfig (populated by SkaffoldFileGeneratorTemplate).
//
// This allows multiple skaffold steps to use the same generated configuration file.
// The step will use the path and profiles from cfg, and add a "mode" field to select
// between "dev", "run", or "debug".
func SkaffoldTemplateFrom(cfg *SkaffoldConfig) tui.StepTemplate {
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
			if cfg.Path == "" {
				return nil, fmt.Errorf("skaffold config path is empty - ensure SkaffoldFileGeneratorTemplate ran first")
			}
			mode := v.String("mode")
			if mode == "" {
				mode = "dev"
			}
			return &SkaffoldStep{Path: cfg.Path, Mode: mode, Profiles: cfg.Profiles}, nil
		},
	}
}

// SkaffoldBuildTemplateFrom returns a StepTemplate for skaffold build that reads its
// configuration from a shared SkaffoldConfig (populated by SkaffoldFileGeneratorTemplate).
//
// This allows the build step to share the same configuration file as other skaffold steps.
func SkaffoldBuildTemplateFrom(cfg *SkaffoldConfig) tui.StepTemplate {
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
			if cfg.Path == "" {
				return nil, fmt.Errorf("skaffold config path is empty - ensure SkaffoldFileGeneratorTemplate ran first")
			}
			return &SkaffoldStep{Path: cfg.Path, Mode: "build", Profiles: cfg.Profiles}, nil
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
