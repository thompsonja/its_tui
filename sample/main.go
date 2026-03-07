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
		Systems: []tui.System{
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

		MFEs: []string{
			"checkout-mfe",
			"user-mfe",
			"product-mfe",
			"analytics-mfe",
		},

		// GenerateSkaffold is called with the list of selected components and
		// should return the path to a skaffold.yaml. Here we return the static
		// sample file; a real implementation would generate one on the fly.
		GenerateSkaffold: func(components []string) (string, error) {
			return filepath.Join(sampleDir(), "skaffold.yaml"), nil
		},

		// RunMFE maps every MFE name to the sample mfe/ directory.
		// A real implementation would map each name to its own repo/directory.
		RunMFE: func(mfe string) tui.MFECommand {
			return tui.MFECommand{
				Cmd:  "npm",
				Args: []string{"start"},
				Dir:  filepath.Join(sampleDir(), "mfe"),
			}
		},
	}

	if err := tui.Run(cfg); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
