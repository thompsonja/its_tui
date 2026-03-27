package builtins

import (
	"path/filepath"
	"testing"

	"github.com/thompsonja/its_tui/config"
	"github.com/thompsonja/its_tui/tui"
)

func TestSkaffoldLogPath_UsesLogDir(t *testing.T) {
	if err := config.SetLogDir("/custom/logs"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer config.SetLogDir("") // restore default

	path := SkaffoldLogPath("test-instance", "debug")
	expected := filepath.Join("/custom/logs", "skaffold_test-instance_debug.log")
	if path != expected {
		t.Errorf("expected %s, got %s", expected, path)
	}
}

func TestMinikubeLogPath_UsesLogDir(t *testing.T) {
	if err := config.SetLogDir("/custom/logs"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer config.SetLogDir("") // restore default

	path := MinikubeLogPath("test-instance")
	expected := filepath.Join("/custom/logs", "minikube_test-instance.log")
	if path != expected {
		t.Errorf("expected %s, got %s", expected, path)
	}
}

func TestMfeLogPath_UsesLogDir(t *testing.T) {
	if err := config.SetLogDir("/custom/logs"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer config.SetLogDir("") // restore default

	path := MfeLogPath("test-instance")
	expected := filepath.Join("/custom/logs", "mfe_test-instance.log")
	if path != expected {
		t.Errorf("expected %s, got %s", expected, path)
	}
}

func TestLogPath_EmptyInstanceName(t *testing.T) {
	if err := config.SetLogDir("/custom/logs"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer config.SetLogDir("") // restore default

	if SkaffoldLogPath("", "debug") != "" {
		t.Error("expected empty path for empty instance name")
	}
	if MinikubeLogPath("") != "" {
		t.Error("expected empty path for empty instance name")
	}
	if MfeLogPath("") != "" {
		t.Error("expected empty path for empty instance name")
	}
}

func TestSkaffoldFileGeneratorTemplate(t *testing.T) {
	cfg := &SkaffoldConfig{}

	// Create a generator template
	generatorTemplate := SkaffoldFileGeneratorTemplate(cfg, func(v tui.WizardValues) (string, []string, error) {
		return "/path/to/skaffold.yaml", []string{"dev", "test"}, nil
	})

	// Call Build to populate the config
	values := tui.NewWizardValues(nil, nil)
	step, err := generatorTemplate.Build(values)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if step != nil {
		t.Errorf("expected nil step, got %v", step)
	}

	// Verify the config was populated
	if cfg.Path != "/path/to/skaffold.yaml" {
		t.Errorf("expected path '/path/to/skaffold.yaml', got '%s'", cfg.Path)
	}
	if len(cfg.Profiles) != 2 || cfg.Profiles[0] != "dev" || cfg.Profiles[1] != "test" {
		t.Errorf("expected profiles [dev test], got %v", cfg.Profiles)
	}
}

func TestSkaffoldTemplateFrom_SharedConfig(t *testing.T) {
	// Simulate the generator populating a shared config
	cfg := &SkaffoldConfig{
		Path:     "/path/to/skaffold.yaml",
		Profiles: []string{"dev"},
	}

	// Create build and dev templates that share the config
	buildTemplate := SkaffoldBuildTemplateFrom(cfg)
	devTemplate := SkaffoldTemplateFrom(cfg)

	// Build the build step
	values := tui.NewWizardValues(nil, nil)
	buildStep, err := buildTemplate.Build(values)
	if err != nil {
		t.Fatalf("unexpected error building build step: %v", err)
	}
	if buildStep == nil {
		t.Fatal("expected non-nil build step")
	}

	// Build the dev step
	devStep, err := devTemplate.Build(values)
	if err != nil {
		t.Fatalf("unexpected error building dev step: %v", err)
	}
	if devStep == nil {
		t.Fatal("expected non-nil dev step")
	}

	// Verify both steps use the same path and profiles
	buildSkaffoldStep, ok := buildStep.(*SkaffoldStep)
	if !ok {
		t.Fatalf("expected SkaffoldStep, got %T", buildStep)
	}
	if buildSkaffoldStep.Path != "/path/to/skaffold.yaml" {
		t.Errorf("expected build step path '/path/to/skaffold.yaml', got '%s'", buildSkaffoldStep.Path)
	}
	if buildSkaffoldStep.Mode != "build" {
		t.Errorf("expected build step mode 'build', got '%s'", buildSkaffoldStep.Mode)
	}

	devSkaffoldStep, ok := devStep.(*SkaffoldStep)
	if !ok {
		t.Fatalf("expected SkaffoldStep, got %T", devStep)
	}
	if devSkaffoldStep.Path != "/path/to/skaffold.yaml" {
		t.Errorf("expected dev step path '/path/to/skaffold.yaml', got '%s'", devSkaffoldStep.Path)
	}
	if devSkaffoldStep.Mode != "dev" {
		t.Errorf("expected dev step mode 'dev', got '%s'", devSkaffoldStep.Mode)
	}

	// Verify both share the same profiles
	if len(buildSkaffoldStep.Profiles) != 1 || buildSkaffoldStep.Profiles[0] != "dev" {
		t.Errorf("expected build step profiles [dev], got %v", buildSkaffoldStep.Profiles)
	}
	if len(devSkaffoldStep.Profiles) != 1 || devSkaffoldStep.Profiles[0] != "dev" {
		t.Errorf("expected dev step profiles [dev], got %v", devSkaffoldStep.Profiles)
	}
}

func TestSkaffoldTemplateFrom_EmptyConfig(t *testing.T) {
	// Create templates with an empty config (generator not run yet)
	cfg := &SkaffoldConfig{}

	buildTemplate := SkaffoldBuildTemplateFrom(cfg)
	devTemplate := SkaffoldTemplateFrom(cfg)

	values := tui.NewWizardValues(nil, nil)

	// Both should return errors since the config is not populated
	_, err := buildTemplate.Build(values)
	if err == nil {
		t.Error("expected error when config path is empty")
	}

	_, err = devTemplate.Build(values)
	if err == nil {
		t.Error("expected error when config path is empty")
	}
}
