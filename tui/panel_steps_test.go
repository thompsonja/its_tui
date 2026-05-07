package tui

import (
	"strings"
	"testing"
)

// newStepsModel returns a minimal model suitable for panel_steps tests.
func newStepsModel() *model {
	return &model{}
}

// ── startPendingStep ──────────────────────────────────────────────────────────

func TestStartPendingStep_LineContainsLabel(t *testing.T) {
	m := newStepsModel()
	m.startPendingStep("skaffold", "Skaffold", []string{"minikube"})
	if !strings.Contains(m.app.commandsBuf[0], "Skaffold") {
		t.Fatalf("expected Skaffold in line, got %q", m.app.commandsBuf[0])
	}
}

func TestStartPendingStep_LineHasPendingIcon(t *testing.T) {
	m := newStepsModel()
	m.startPendingStep("skaffold", "Skaffold", []string{"minikube"})
	if !strings.Contains(m.app.commandsBuf[0], "○") {
		t.Fatalf("expected ○ icon in pending line, got %q", m.app.commandsBuf[0])
	}
}

func TestStartPendingStep_LineHasEmptyBar(t *testing.T) {
	m := newStepsModel()
	m.startPendingStep("skaffold", "Skaffold", []string{"minikube"})
	if !strings.Contains(m.app.commandsBuf[0], barEmpty) {
		t.Fatalf("expected empty bar chars in pending line, got %q", m.app.commandsBuf[0])
	}
}

func TestStartPendingStep_LineDoesNotShowDeps(t *testing.T) {
	m := newStepsModel()
	m.startPendingStep("app", "App", []string{"infra", "config"})
	line := m.app.commandsBuf[0]
	if strings.Contains(line, "infra") || strings.Contains(line, "config") {
		t.Fatalf("dep names should not appear in bar line, got %q", line)
	}
}

func TestStartPendingStep_StoresPendingDeps(t *testing.T) {
	m := newStepsModel()
	m.startPendingStep("app", "App", []string{"infra", "config"})
	s := m.vs.steps["app"]
	if len(s.pendingDeps) != 2 {
		t.Fatalf("expected 2 pendingDeps, got %d", len(s.pendingDeps))
	}
}

func TestStartPendingStep_MarkedPending(t *testing.T) {
	m := newStepsModel()
	m.startPendingStep("app", "App", []string{"infra"})
	if !m.vs.steps["app"].pending {
		t.Fatal("expected step to be marked pending")
	}
}

func TestStartPendingStep_DoesNotMutateDepsSlice(t *testing.T) {
	m := newStepsModel()
	deps := []string{"a", "b"}
	m.startPendingStep("app", "App", deps)
	deps[0] = "mutated"
	if m.vs.steps["app"].pendingDeps[0] == "mutated" {
		t.Fatal("startPendingStep should copy the deps slice, not reference it")
	}
}

// ── depReady ──────────────────────────────────────────────────────────────────

func TestDepReady_RemovesCompletedDep(t *testing.T) {
	m := newStepsModel()
	m.startPendingStep("app", "App", []string{"infra", "config"})
	m.depReady("app", "infra")
	if strings.Contains(m.app.commandsBuf[0], "infra") {
		t.Fatalf("expected infra removed from line, got %q", m.app.commandsBuf[0])
	}
}

func TestDepReady_PreservesRemainingDep(t *testing.T) {
	m := newStepsModel()
	m.startPendingStep("app", "App", []string{"infra", "config"})
	m.depReady("app", "infra")
	s := m.vs.steps["app"]
	if len(s.pendingDeps) != 1 || s.pendingDeps[0] != "config" {
		t.Fatalf("expected pendingDeps=[config], got %v", s.pendingDeps)
	}
}

func TestDepReady_LineUnchangedWhileDepsRemain(t *testing.T) {
	m := newStepsModel()
	m.startPendingStep("app", "App", []string{"infra", "config"})
	before := m.app.commandsBuf[0]
	m.depReady("app", "infra")
	if m.app.commandsBuf[0] != before {
		t.Fatalf("expected line unchanged while deps remain\nbefore: %q\nafter:  %q", before, m.app.commandsBuf[0])
	}
}

func TestDepReady_ClearsWaitingSuffixWhenLastDepDone(t *testing.T) {
	m := newStepsModel()
	m.startPendingStep("app", "App", []string{"infra"})
	m.depReady("app", "infra")
	if strings.Contains(m.app.commandsBuf[0], "waiting") {
		t.Fatalf("expected no waiting suffix after last dep, got %q", m.app.commandsBuf[0])
	}
}

func TestDepReady_PreservesLabelAfterAllDone(t *testing.T) {
	m := newStepsModel()
	m.startPendingStep("app", "My App", []string{"infra"})
	m.depReady("app", "infra")
	if !strings.Contains(m.app.commandsBuf[0], "My App") {
		t.Fatalf("expected base label preserved, got %q", m.app.commandsBuf[0])
	}
}

func TestDepReady_SequentialCompletions(t *testing.T) {
	m := newStepsModel()
	m.startPendingStep("app", "App", []string{"a", "b", "c"})
	m.depReady("app", "a")
	s := m.vs.steps["app"]
	if len(s.pendingDeps) != 2 {
		t.Fatalf("after removing a, expected 2 pendingDeps, got %v", s.pendingDeps)
	}
	m.depReady("app", "b")
	if len(s.pendingDeps) != 1 || s.pendingDeps[0] != "c" {
		t.Fatalf("after removing b, expected pendingDeps=[c], got %v", s.pendingDeps)
	}
	m.depReady("app", "c")
	if len(s.pendingDeps) != 0 {
		t.Fatalf("after all deps done, expected empty pendingDeps, got %v", s.pendingDeps)
	}
}

func TestDepReady_UpdatesPendingDepsSlice(t *testing.T) {
	m := newStepsModel()
	m.startPendingStep("app", "App", []string{"a", "b"})
	m.depReady("app", "a")
	s := m.vs.steps["app"]
	if len(s.pendingDeps) != 1 || s.pendingDeps[0] != "b" {
		t.Fatalf("expected pendingDeps=[b], got %v", s.pendingDeps)
	}
}

func TestDepReady_UnknownStepIsNoop(t *testing.T) {
	m := newStepsModel()
	// Must not panic when the step ID doesn't exist.
	m.depReady("nonexistent", "dep")
}

func TestDepReady_UnknownDepLeavesLineUnchanged(t *testing.T) {
	m := newStepsModel()
	m.startPendingStep("app", "App", []string{"infra"})
	before := m.app.commandsBuf[0]
	m.depReady("app", "no-such-dep")
	if m.app.commandsBuf[0] != before {
		t.Fatalf("expected line unchanged for unknown dep\nbefore: %q\nafter:  %q", before, m.app.commandsBuf[0])
	}
}

func TestDepReady_MultipleStepsIndependent(t *testing.T) {
	m := newStepsModel()
	m.startPendingStep("app", "App", []string{"infra"})
	m.startPendingStep("worker", "Worker", []string{"infra", "config"})
	m.depReady("app", "infra")
	// worker's pendingDeps should be unaffected.
	worker := m.vs.steps["worker"]
	if len(worker.pendingDeps) != 2 {
		t.Fatalf("worker pendingDeps should be unchanged (2), got %v", worker.pendingDeps)
	}
}
