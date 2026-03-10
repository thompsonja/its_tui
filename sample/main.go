package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

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
	cfg := tui.Config{
		Steps: []tui.StepTemplate{
			tui.MinikubeTemplate(),
			tui.KubectlTemplate(),
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
						ID:      "env",
						Label:   "Environment",
						Kind:    tui.FieldKindSelect,
						Options: []string{"dev", "test"},
						Default: 0,
					},
					{
						ID:      "api_port",
						Label:   "API Port",
						Kind:    tui.FieldKindSelect,
						Options: []string{"9001", "9002", "9003"},
						Default: 0,
					},
				},
				Build: func(v tui.WizardValues) (tui.Step, error) {
					return nil, nil
				},
			},
			tui.SkaffoldTemplate(
				func(v tui.WizardValues) (string, []string, error) {
					// Generate a skaffold.yaml with the selected port and env profile.
					env := v.String("env")
					port := v.String("api_port")
					if port == "" {
						port = "9001"
					}
					return generateSkaffoldYAML(sampleDir(), env, port)
				},
				[]tui.System{
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
				},
			),
			tui.MFETemplate(
				[]string{
					"checkout-mfe",
					"user-mfe",
					"product-mfe",
					"analytics-mfe",
				},
				// RunMFE maps every MFE name to the sample mfe/ directory.
				// The MFE calls GET /hello on the port-forwarded service and
				// displays the message returned by the Go server.
				func(name string, v tui.WizardValues) tui.MFECommand {
					port := v.String("api_port")
					if port == "" {
						port = "9001"
					}
					return tui.MFECommand{
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
