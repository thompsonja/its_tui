package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGetLogDir_InitialDefault(t *testing.T) {
	// GetLogDir should return ~/.tui/logs by default before any SetLogDir call
	got := GetLogDir()
	expected := defaultLogDir()
	if got != expected {
		t.Errorf("expected initial default %s, got %s", expected, got)
	}
	// Verify it ends with .tui/logs
	if !strings.HasSuffix(got, filepath.Join(".tui", "logs")) {
		t.Errorf("expected path to end with .tui/logs, got %s", got)
	}
}

func TestSetLogDir_DefaultsToTuiLogs(t *testing.T) {
	if err := SetLogDir(""); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := GetLogDir()
	expected := defaultLogDir()
	if got != expected {
		t.Errorf("expected %s, got %s", expected, got)
	}
	// Verify it ends with .tui/logs
	if !strings.HasSuffix(got, filepath.Join(".tui", "logs")) {
		t.Errorf("expected path to end with .tui/logs, got %s", got)
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
	expected := defaultLogDir()
	if got != expected {
		t.Errorf("expected %s for '.', got %s", expected, got)
	}
}

func TestEnsureLogDir_CreatesDirectory(t *testing.T) {
	// Use a temporary directory for testing
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "test-logs")

	if err := SetLogDir(logPath); err != nil {
		t.Fatalf("unexpected error setting log dir: %v", err)
	}
	defer SetLogDir("") // restore default

	// Directory should not exist yet
	if _, err := os.Stat(logPath); err == nil {
		t.Fatal("directory should not exist before EnsureLogDir")
	}

	// Create the directory
	if err := EnsureLogDir(); err != nil {
		t.Fatalf("EnsureLogDir failed: %v", err)
	}

	// Directory should now exist
	info, err := os.Stat(logPath)
	if err != nil {
		t.Fatalf("directory should exist after EnsureLogDir: %v", err)
	}
	if !info.IsDir() {
		t.Error("expected a directory")
	}

	// Calling again should be idempotent
	if err := EnsureLogDir(); err != nil {
		t.Fatalf("EnsureLogDir should be idempotent: %v", err)
	}
}
