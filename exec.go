package tui

import (
	"bufio"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"
)

// killProcessGroup sends SIGTERM to the entire process group identified by pgid.
// Safe to call with pgid <= 0 (no-op).
func killProcessGroup(pgid int) {
	if pgid <= 0 {
		return
	}
	_ = syscall.Kill(-pgid, syscall.SIGTERM)
}

// streamToPanel builds a command from name+args and streams it via streamCmd.
func streamToPanel(ctx context.Context, factory func(string) tea.Msg, name string, args ...string) {
	streamCmd(ctx, exec.CommandContext(ctx, name, args...), factory)
}

// streamCmd drains stdout and stderr from cmd, sending each line through factory.
// A nil return from factory means "skip this line".
// Suppresses the exit message when the context was cancelled (e.g. instance switch).
func streamCmd(ctx context.Context, cmd *exec.Cmd, factory func(string) tea.Msg) {
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		if msg := factory(fmt.Sprintf("stdout pipe error: %v", err)); msg != nil {
			prog.Send(msg)
		}
		return
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		if msg := factory(fmt.Sprintf("stderr pipe error: %v", err)); msg != nil {
			prog.Send(msg)
		}
		return
	}
	if err := cmd.Start(); err != nil {
		if msg := factory(fmt.Sprintf("start error: %v", err)); msg != nil {
			prog.Send(msg)
		}
		return
	}

	// Drain stdout in a separate goroutine; drain stderr here.
	go func() {
		s := bufio.NewScanner(stdout)
		for s.Scan() {
			if msg := factory(s.Text()); msg != nil {
				prog.Send(msg)
			}
		}
	}()

	s := bufio.NewScanner(stderr)
	for s.Scan() {
		if msg := factory(s.Text()); msg != nil {
			prog.Send(msg)
		}
	}

	if err := cmd.Wait(); err != nil {
		if ctx.Err() != nil {
			return // killed by context cancellation — don't report
		}
		if msg := factory(fmt.Sprintf("[exited: %v]", err)); msg != nil {
			prog.Send(msg)
		}
	} else {
		if msg := factory("[process exited cleanly]"); msg != nil {
			prog.Send(msg)
		}
	}
}

// splitLines splits s into lines using a scanner (handles \r\n too).
func splitLines(s string) []string {
	var lines []string
	sc := bufio.NewScanner(strings.NewReader(s))
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	return lines
}
