package builtins

import (
	"path/filepath"
	"testing"

	"github.com/thompsonja/its_tui/config"
)

func TestSkaffoldLogPath_UsesLogDir(t *testing.T) {
	if err := config.SetLogDir("/custom/logs"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer config.SetLogDir("") // restore default

	path := SkaffoldLogPath("test-instance")
	expected := filepath.Join("/custom/logs", "skaffold_test-instance.log")
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

	if SkaffoldLogPath("") != "" {
		t.Error("expected empty path for empty instance name")
	}
	if MinikubeLogPath("") != "" {
		t.Error("expected empty path for empty instance name")
	}
	if MfeLogPath("") != "" {
		t.Error("expected empty path for empty instance name")
	}
}
