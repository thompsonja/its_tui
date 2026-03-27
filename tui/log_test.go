package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDebugLog(t *testing.T) {
	// Clean up before test
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("Failed to get home directory: %v", err)
	}

	logPath := filepath.Join(home, ".tui", "debug.log")
	_ = os.Remove(logPath)

	// Initialize logging
	if err := initDebugLog(); err != nil {
		t.Fatalf("Failed to initialize debug log: %v", err)
	}

	// Write some test messages
	debugLog("Test message 1")
	debugLog("Test message with args: %s %d", "hello", 42)

	// Close the log
	closeDebugLog()

	// Read the log file and verify content
	content, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("Failed to read debug log: %v", err)
	}

	logContent := string(content)
	if !strings.Contains(logContent, "TUI session started") {
		t.Errorf("Log should contain session started message")
	}
	if !strings.Contains(logContent, "Test message 1") {
		t.Errorf("Log should contain test message 1")
	}
	if !strings.Contains(logContent, "Test message with args: hello 42") {
		t.Errorf("Log should contain formatted test message")
	}
	if !strings.Contains(logContent, "TUI session ended") {
		t.Errorf("Log should contain session ended message")
	}
}
