package tui

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
)

var (
	debugLogger *log.Logger
	debugFile   *os.File
	logMutex    sync.Mutex
)

// initDebugLog initializes the debug log file.
// Logs are written to ~/.tui/debug.log
func initDebugLog() error {
	logMutex.Lock()
	defer logMutex.Unlock()

	if debugLogger != nil {
		return nil // already initialized
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	tuiDir := filepath.Join(home, ".tui")
	if err := os.MkdirAll(tuiDir, 0755); err != nil {
		return fmt.Errorf("failed to create .tui directory: %w", err)
	}

	logPath := filepath.Join(tuiDir, "debug.log")
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("failed to open debug log: %w", err)
	}

	debugFile = f
	debugLogger = log.New(f, "", log.LstdFlags|log.Lmicroseconds)
	debugLogger.Printf("=== TUI session started ===")
	return nil
}

// closeDebugLog closes the debug log file.
func closeDebugLog() {
	logMutex.Lock()
	defer logMutex.Unlock()

	if debugLogger != nil {
		debugLogger.Printf("=== TUI session ended ===")
	}
	if debugFile != nil {
		debugFile.Close()
		debugFile = nil
	}
	debugLogger = nil
}

// debugLog writes a message to the debug log if initialized.
func debugLog(format string, args ...interface{}) {
	logMutex.Lock()
	defer logMutex.Unlock()

	if debugLogger != nil {
		debugLogger.Printf(format, args...)
	}
}
