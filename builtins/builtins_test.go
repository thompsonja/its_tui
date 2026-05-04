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

func TestSkaffoldPipeline(t *testing.T) {
	generateCalls := 0
	pipeline := NewSkaffoldPipeline(func(v tui.WizardValues) (string, []string, error) {
		generateCalls++
		return "/generated/skaffold.yaml", []string{"dev"}, nil
	})

	templates := pipeline.Templates()
	if len(templates) != 3 {
		t.Fatalf("expected 3 templates, got %d", len(templates))
	}

	// Run the generator's Build to populate the shared config.
	values := tui.NewWizardValues(nil, nil)
	genStep, err := templates[0].Build(values)
	if err != nil {
		t.Fatalf("generator Build failed: %v", err)
	}
	if genStep != nil {
		t.Errorf("generator Build should return nil step, got %T", genStep)
	}
	if generateCalls != 1 {
		t.Errorf("expected generate to be called once, got %d", generateCalls)
	}

	// Build and dev templates should pick up the generated config.
	buildStep, err := templates[1].Build(values)
	if err != nil {
		t.Fatalf("build template Build failed: %v", err)
	}
	if buildStep == nil {
		t.Fatal("expected non-nil build step")
	}
	if buildStep.(*SkaffoldStep).Mode != "build" {
		t.Errorf("expected build mode, got %q", buildStep.(*SkaffoldStep).Mode)
	}

	devStep, err := templates[2].Build(values)
	if err != nil {
		t.Fatalf("dev template Build failed: %v", err)
	}
	if devStep == nil {
		t.Fatal("expected non-nil dev step")
	}
	if devStep.(*SkaffoldStep).Mode != "dev" {
		t.Errorf("expected dev mode, got %q", devStep.(*SkaffoldStep).Mode)
	}

	// DevTemplate should wait for both minikube and skaffold_build.
	devTemplate := templates[2]
	wantDeps := map[string]bool{"minikube": false, "skaffold_build": false}
	for _, dep := range devTemplate.WaitFor {
		wantDeps[dep] = true
	}
	for dep, found := range wantDeps {
		if !found {
			t.Errorf("DevTemplate WaitFor missing %q", dep)
		}
	}
}

func TestSkaffoldPipeline_DevOnlyNoBuild(t *testing.T) {
	pipeline := NewSkaffoldPipeline(func(v tui.WizardValues) (string, []string, error) {
		return "/gen/skaffold.yaml", nil, nil
	})

	// Populate config via generator.
	values := tui.NewWizardValues(nil, nil)
	if _, err := pipeline.GeneratorTemplate().Build(values); err != nil {
		t.Fatalf("generator failed: %v", err)
	}

	// DevTemplate should still build without error even without BuildTemplate registered.
	devStep, err := pipeline.DevTemplate().Build(values)
	if err != nil {
		t.Fatalf("dev template Build failed: %v", err)
	}
	if devStep == nil {
		t.Fatal("expected non-nil dev step")
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
