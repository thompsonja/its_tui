package config

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestSetLogDir_DefaultsToTmp(t *testing.T) {
	if err := SetLogDir(""); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := GetLogDir()
	if got != "/tmp" {
		t.Errorf("expected /tmp, got %s", got)
	}
}

func TestSetLogDir_CustomPath(t *testing.T) {
	if err := SetLogDir("/var/log/myapp"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer SetLogDir("") // restore default

	got := GetLogDir()
	if got != "/var/log/myapp" {
		t.Errorf("expected /var/log/myapp, got %s", got)
	}
}

func TestSetLogDir_RelativePath(t *testing.T) {
	if err := SetLogDir("./logs"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer SetLogDir("") // restore default

	got := GetLogDir()
	if got != "logs" {
		t.Errorf("expected 'logs', got %s", got)
	}
}

func TestSetLogDir_PathTraversal(t *testing.T) {
	err := SetLogDir("/tmp/../etc/passwd")
	if err == nil {
		t.Fatal("expected error for path traversal, got nil")
	}
	if !strings.Contains(err.Error(), "invalid traversal") {
		t.Errorf("expected 'invalid traversal' error, got: %v", err)
	}
}

func TestSetLogDir_PathTraversalInMiddle(t *testing.T) {
	err := SetLogDir("/var/../../../etc")
	if err == nil {
		t.Fatal("expected error for path traversal, got nil")
	}
}

func TestSetLogDir_CleansDot(t *testing.T) {
	if err := SetLogDir("."); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer SetLogDir("") // restore default

	got := GetLogDir()
	if got != "/tmp" {
		t.Errorf("expected /tmp for '.', got %s", got)
	}
}

func TestSkaffoldLogPath_UsesLogDir(t *testing.T) {
	if err := SetLogDir("/custom/logs"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer SetLogDir("") // restore default

	path := SkaffoldLogPath("test-instance")
	expected := filepath.Join("/custom/logs", "skaffold_test-instance.log")
	if path != expected {
		t.Errorf("expected %s, got %s", expected, path)
	}
}

func TestMinikubeLogPath_UsesLogDir(t *testing.T) {
	if err := SetLogDir("/custom/logs"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer SetLogDir("") // restore default

	path := MinikubeLogPath("test-instance")
	expected := filepath.Join("/custom/logs", "minikube_test-instance.log")
	if path != expected {
		t.Errorf("expected %s, got %s", expected, path)
	}
}

func TestMfeLogPath_UsesLogDir(t *testing.T) {
	if err := SetLogDir("/custom/logs"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer SetLogDir("") // restore default

	path := MfeLogPath("test-instance")
	expected := filepath.Join("/custom/logs", "mfe_test-instance.log")
	if path != expected {
		t.Errorf("expected %s, got %s", expected, path)
	}
}

func TestLogPath_EmptyInstanceName(t *testing.T) {
	if err := SetLogDir("/custom/logs"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer SetLogDir("") // restore default

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
