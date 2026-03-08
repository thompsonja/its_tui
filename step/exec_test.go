package step

import "testing"

func TestSplitLines_Basic(t *testing.T) {
	lines := SplitLines("a\nb\nc")
	if len(lines) != 3 || lines[0] != "a" || lines[1] != "b" || lines[2] != "c" {
		t.Fatalf("unexpected: %v", lines)
	}
}

func TestSplitLines_Empty(t *testing.T) {
	lines := SplitLines("")
	if len(lines) != 0 {
		t.Fatalf("expected empty, got %v", lines)
	}
}

func TestSplitLines_CRLF(t *testing.T) {
	lines := SplitLines("a\r\nb\r\nc")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %v", lines)
	}
	for _, l := range lines {
		for _, r := range l {
			if r == '\r' || r == '\n' {
				t.Fatalf("line contains CR/LF: %q", l)
			}
		}
	}
}

func TestSplitLines_TrailingNewline(t *testing.T) {
	// A trailing newline should not produce a phantom empty line.
	lines := SplitLines("a\nb\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %v", lines)
	}
}

func TestSplitLines_SingleLine(t *testing.T) {
	lines := SplitLines("hello")
	if len(lines) != 1 || lines[0] != "hello" {
		t.Fatalf("unexpected: %v", lines)
	}
}
