package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"tui"
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
			// env step: contributes the "env" selector field to the wizard.
			// It does not start a process of its own — the selected value is
			// read by the skaffold generate callback below and forwarded as a
			// --profile flag to skaffold.
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
				},
				Build: func(v tui.WizardValues) (tui.Step, error) {
					return nil, nil
				},
			},
			tui.SkaffoldTemplate(
				func(v tui.WizardValues) (string, []string, error) {
					// Map the selected environment to a skaffold profile.
					// The skaffold.yaml defines matching "dev" and "test" profiles
					// that set APP_ENV on the deployed container accordingly.
					env := v.String("env")
					var profiles []string
					if env != "" {
						profiles = []string{env}
					}
					return filepath.Join(sampleDir(), "skaffold.yaml"), profiles, nil
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
				func(name string) tui.MFECommand {
					return tui.MFECommand{
						Cmd:  "node",
						Args: []string{"index.js"},
						Dir:  filepath.Join(sampleDir(), "mfe"),
					}
				},
			),
		},
	}

	if err := tui.Run(cfg); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
