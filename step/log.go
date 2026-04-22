package step

import (
	"log"
	"os"
	"path/filepath"
	"sync"
)

var (
	stepDebugLogger *log.Logger
	stepDebugFile   *os.File
	stepLogMutex    sync.Mutex
	stepLogInit     sync.Once
)

// initStepDebugLog initializes the step debug log file.
// Logs are written to ~/.tui/debug.log (same file as TUI logs)
func initStepDebugLog() {
	stepLogInit.Do(func() {
		home, err := os.UserHomeDir()
		if err != nil {
			return
		}

		tuiDir := filepath.Join(home, ".tui")
		if err := os.MkdirAll(tuiDir, 0755); err != nil {
			return
		}

		logPath := filepath.Join(tuiDir, "debug.log")
		f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			return
		}

		stepDebugFile = f
		stepDebugLogger = log.New(f, "[step] ", log.LstdFlags|log.Lmicroseconds)
	})
}

// stepDebugLog writes a message to the step debug log.
func stepDebugLog(format string, args ...interface{}) {
	initStepDebugLog()
	stepLogMutex.Lock()
	defer stepLogMutex.Unlock()

	if stepDebugLogger != nil {
		stepDebugLogger.Printf(format, args...)
	}
}

// DebugLog writes a message to the shared debug log. Exported for use by
// packages that implement steps (e.g. builtins) where the unexported
// stepDebugLog is not accessible.
func DebugLog(format string, args ...interface{}) {
	stepDebugLog(format, args...)
}
