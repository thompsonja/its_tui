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

// streamCmd drains stdout and stderr from cmd, calling emit for each line.
// Suppresses the exit message when the context was cancelled.
func streamCmd(ctx context.Context, cmd *exec.Cmd, emit func(string)) {
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		emit(fmt.Sprintf("stdout pipe error: %v", err))
		return
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		emit(fmt.Sprintf("stderr pipe error: %v", err))
		return
	}
	if err := cmd.Start(); err != nil {
		emit(fmt.Sprintf("start error: %v", err))
		return
	}

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

	if err := cmd.Wait(); err != nil {
		if ctx.Err() != nil {
			return // killed by context cancellation — don't report
		}
		emit(fmt.Sprintf("[exited: %v]", err))
	} else {
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
