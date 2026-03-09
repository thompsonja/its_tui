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
			tui.SkaffoldTemplate(
				func(components []string, mode string) (string, error) {
					// A real implementation would generate a skaffold.yaml from
					// the selected components and mode. Here we return the static
					// sample file for demonstration purposes.
					return filepath.Join(sampleDir(), "skaffold.yaml"), nil
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
				// A real implementation would map each name to its own repo.
				func(name string) tui.MFECommand {
					return tui.MFECommand{
						Cmd:  "npm",
						Args: []string{"start"},
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
