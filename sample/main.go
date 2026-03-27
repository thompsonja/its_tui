package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/thompsonja/its_tui/builtins"
	"github.com/thompsonja/its_tui/tui"
)

// sampleDir returns the directory containing this source file, so that
// relative paths like "skaffold.yaml" resolve correctly regardless of where
// the binary is invoked from.
func sampleDir() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "sample"
	}
	return filepath.Dir(file)
}

func main() {
	// Shared skaffold config: the generator populates this, and both the
	// build and dev steps read from it.
	skaffoldCfg := &builtins.SkaffoldConfig{}

	cfg := tui.Config{
		Steps: []tui.StepTemplate{
			builtins.MinikubeTemplate(),
			builtins.KubectlTemplate(),
			// env step: contributes the "env" and "api_port" selector fields to
			// the wizard. It does not start a process of its own — the selected
			// values are read by the skaffold generate callback below.
			{
				ID:     "env",
				Panel:  tui.PanelTopLeft,
				Label:  "Environment",
				Hidden: true,
				Fields: []tui.FieldSpec{
					{
						ID:          "env",
						Label:       "Environment",
						Kind:        tui.FieldKindSelect,
						OptionsFunc: tui.StaticOptions("dev", "test"),
						Default:     0,
					},
					{
						ID:          "api_port",
						Label:       "API Port",
						Kind:        tui.FieldKindSelect,
						OptionsFunc: tui.StaticOptions("9001", "9002", "9003"),
						Default:     0,
					},
				},
				Build: func(v tui.WizardValues) (tui.Step, error) {
					return nil, nil
				},
			},
			// components step: contributes the "components" field using the
			// systems/components hierarchy. Like the env step, it doesn't start
			// a process - the selected components are read by the skaffold
			// generate callback below.
			{
				ID:     "components",
				Panel:  tui.PanelTopLeft,
				Label:  "Components",
				Hidden: true,
				Fields: []tui.FieldSpec{
					{
						ID:    "components",
						Label: "Components",
						Kind:  tui.FieldKindSystemSelect,
						SystemsFunc: func(v tui.WizardValues) []tui.System {
							return []tui.System{
								{
									Name: "checkout",
									Components: []tui.Component{
										{Name: "checkout-backend"},
										{Name: "checkout-bff"},
									},
								},
								{
									Name: "user",
									Components: []tui.Component{
										{Name: "user-service"},
										{Name: "user-bff"},
									},
								},
								{
									Name: "product",
									Components: []tui.Component{
										{Name: "product-service"},
										{Name: "product-bff"},
									},
								},
								{
									Name: "order",
									Components: []tui.Component{
										{Name: "order-service"},
										{Name: "order-bff"},
									},
								},
								{
									Name: "analytics",
									Components: []tui.Component{
										{Name: "analytics-backend"},
										{Name: "analytics-bff"},
									},
								},
							}
						},
					},
				},
				Build: func(v tui.WizardValues) (tui.Step, error) {
					return nil, nil
				},
			},
			// Skaffold file generator: generates the skaffold.yaml once and
			// populates skaffoldCfg with the path and profiles. This allows
			// both the build and dev steps below to use the same file without
			// generating it twice.
			builtins.SkaffoldFileGeneratorTemplate(skaffoldCfg,
				func(v tui.WizardValues) (string, []string, error) {
					// Generate a skaffold.yaml with the selected port and env profile.
					// The "components" field from the step above is available in v but
					// not used in this simple example.
					env := v.String("env")
					port := v.String("api_port")
					if port == "" {
						port = "9001"
					}
					return generateSkaffoldYAML(sampleDir(), env, port)
				},
			),
			// Skaffold build step: runs `skaffold build` using the generated config
			builtins.SkaffoldBuildTemplateFrom(skaffoldCfg),
			// Skaffold dev step: runs `skaffold dev/run/debug` using the generated config
			builtins.SkaffoldTemplateFrom(skaffoldCfg),
			builtins.MFETemplate(
				[]string{
					"checkout-mfe",
					"user-mfe",
					"product-mfe",
					"analytics-mfe",
				},
				// RunMFE maps every MFE name to the sample mfe/ directory.
				// The MFE calls GET /hello on the port-forwarded service and
				// displays the message returned by the Go server.
				func(name string, v tui.WizardValues) builtins.MFECommand {
					port := v.String("api_port")
					if port == "" {
						port = "9001"
					}
					return builtins.MFECommand{
						Cmd:  "node",
						Args: []string{"index.js"},
						Dir:  filepath.Join(sampleDir(), "mfe"),
						Env:  map[string]string{"API_BASE": "http://localhost:" + port},
					}
				},
			),
		},
		Tests: []tui.TestTemplate{
			{
				Label: "API",
				Build: func(v tui.WizardValues) (tui.TestCommand, error) {
					port := v.String("api_port")
					if port == "" {
						port = "9001"
					}
					return tui.TestCommand{
						Cmd:  "go",
						Args: []string{"test", "-v", "./..."},
						Dir:  sampleDir(),
						Env:  map[string]string{"API_BASE": "http://localhost:" + port},
					}, nil
				},
			},
		},
	}

	if err := tui.Run(cfg); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
