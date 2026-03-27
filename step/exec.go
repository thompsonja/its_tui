package step

import (
	"bufio"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"syscall"
)

// KillProcessGroup sends SIGTERM to the process group identified by pgid.
// Safe to call with pgid <= 0 (no-op).
func KillProcessGroup(pgid int) {
	if pgid <= 0 {
		return
	}
	_ = syscall.Kill(-pgid, syscall.SIGTERM)
}

// StreamToPanel builds a command from name+args and streams each line as a LineMsg
// with the given step ID.
func StreamToPanel(ctx context.Context, id string, name string, args ...string) {
	cmd := exec.CommandContext(ctx, name, args...)
	streamCmd(ctx, cmd, func(line string) {
		Send(LineMsg{ID: id, Line: line})
	})
}

// StreamCmd drains stdout and stderr from cmd, calling emit for each line.
// Suppresses the exit message when the context was cancelled.
func StreamCmd(ctx context.Context, cmd *exec.Cmd, emit func(string)) {
	streamCmd(ctx, cmd, emit)
}

func streamCmd(ctx context.Context, cmd *exec.Cmd, emit func(string)) {
	stepDebugLog("streamCmd: starting command: %v", cmd.Args)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stepDebugLog("streamCmd: stdout pipe error: %v", err)
		emit(fmt.Sprintf("stdout pipe error: %v", err))
		return
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		stepDebugLog("streamCmd: stderr pipe error: %v", err)
		emit(fmt.Sprintf("stderr pipe error: %v", err))
		return
	}
	stepDebugLog("streamCmd: starting process")
	if err := cmd.Start(); err != nil {
		stepDebugLog("streamCmd: start error: %v", err)
		emit(fmt.Sprintf("start error: %v", err))
		return
	}
	stepDebugLog("streamCmd: process started, setting up scanners")

	go func() {
		s := bufio.NewScanner(stdout)
		for s.Scan() {
			emit(s.Text())
		}
	}()

	s := bufio.NewScanner(stderr)
	for s.Scan() {
		emit(s.Text())
	}

	stepDebugLog("streamCmd: waiting for process to exit")
	if err := cmd.Wait(); err != nil {
		if ctx.Err() != nil {
			stepDebugLog("streamCmd: process killed by context cancellation")
			return // killed by context cancellation — don't report
		}
		stepDebugLog("streamCmd: process exited with error: %v", err)
		emit(fmt.Sprintf("[exited: %v]", err))
	} else {
		stepDebugLog("streamCmd: process exited cleanly")
		emit("[process exited cleanly]")
	}
}

// SplitLines splits s into lines using a scanner (handles \r\n too).
func SplitLines(s string) []string {
	var lines []string
	sc := bufio.NewScanner(strings.NewReader(s))
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	return lines
}
