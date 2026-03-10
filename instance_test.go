package tui

import (
	"strings"
	"testing"
)

func TestRenderTopBar_NoInstance(t *testing.T) {
	m := newModel(Config{})
	m.width = 40
	out := m.renderTopBar()
	if !strings.Contains(out, "no instance running") {
		t.Fatalf("expected 'no instance running', got: %q", out)
	}
}

func TestRenderTopBar_WithInstance(t *testing.T) {
	m := newModel(Config{})
	m.width = 40
	m.instanceName = "hello-world"
	out := m.renderTopBar()
	if !strings.Contains(out, "hello-world") {
		t.Fatalf("expected 'hello-world', got: %q", out)
	}
}

func TestRenderTopBar_CustomStatusLine(t *testing.T) {
	m := newModel(Config{
		StatusLine: func(name string) string { return "custom:" + name },
	})
	m.width = 40
	m.instanceName = "env"
	out := m.renderTopBar()
	if !strings.Contains(out, "custom:env") {
		t.Fatalf("expected 'custom:env', got: %q", out)
	}
}
