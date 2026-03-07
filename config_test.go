package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ── LoadConfigs ───────────────────────────────────────────────────────────────

func TestLoadConfigs_MissingFile(t *testing.T) {
	cfg, err := LoadConfigs("/nonexistent/path/config.yaml")
	if err != nil {
		t.Fatalf("missing file should not error, got: %v", err)
	}
	if len(cfg) != 0 {
		t.Fatalf("expected empty configs, got %v", cfg)
	}
}

func TestLoadConfigs_ValidYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	yaml := `
hello-world:
  minikube:
    cpu: 4
    ram: 4g
  skaffold:
    path: sample/skaffold.yaml
  mfe:
    path: sample/mfe/package.json
`
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfigs(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	ic, ok := cfg["hello-world"]
	if !ok {
		t.Fatal("expected key 'hello-world'")
	}
	if ic.Minikube.CPU != 4 {
		t.Fatalf("expected cpu=4, got %d", ic.Minikube.CPU)
	}
	if ic.Minikube.RAM != "4g" {
		t.Fatalf("expected ram=4g, got %q", ic.Minikube.RAM)
	}
}

func TestLoadConfigs_EmptyYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfigs(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg) != 0 {
		t.Fatalf("expected empty configs, got %v", cfg)
	}
}

func TestLoadConfigs_InvalidInstanceName(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	yaml := "\"bad name!\": {}\n"
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadConfigs(path)
	if err == nil {
		t.Fatal("expected error for invalid instance name")
	}
}

func TestLoadConfigs_MultipleInstances(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	yaml := "alpha: {}\nbeta: {}\n"
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfigs(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg) != 2 {
		t.Fatalf("expected 2 instances, got %d", len(cfg))
	}
}

// ── LoadState / SaveState ─────────────────────────────────────────────────────

func TestLoadState_MissingFile(t *testing.T) {
	s, err := LoadState("/nonexistent/state.json")
	if err != nil {
		t.Fatalf("missing file should not error, got: %v", err)
	}
	if s.Instances == nil {
		t.Fatal("Instances map should be initialized")
	}
}

func TestSaveAndLoadState_RoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	s := State{
		Instances:       map[string]ActiveInstance{"foo": {StartedAt: "2026-01-01T00:00:00Z"}},
		CurrentInstance: "foo",
		Theme:           "dark",
	}
	if err := SaveState(path, s); err != nil {
		t.Fatalf("SaveState: %v", err)
	}
	got, err := LoadState(path)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if got.CurrentInstance != "foo" {
		t.Fatalf("expected CurrentInstance=foo, got %q", got.CurrentInstance)
	}
	if got.Theme != "dark" {
		t.Fatalf("expected Theme=dark, got %q", got.Theme)
	}
	if _, ok := got.Instances["foo"]; !ok {
		t.Fatal("expected instance foo to be present")
	}
}

func TestSaveState_CreatesParentDirs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a", "b", "state.json")
	if err := SaveState(path, State{Instances: map[string]ActiveInstance{}}); err != nil {
		t.Fatalf("SaveState should create parent dirs: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("state file should exist: %v", err)
	}
}

// ── MarkActive / MarkInactive ─────────────────────────────────────────────────

func TestMarkActive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := MarkActive(path, "myinstance"); err != nil {
		t.Fatalf("MarkActive: %v", err)
	}
	s, _ := LoadState(path)
	if _, ok := s.Instances["myinstance"]; !ok {
		t.Fatal("expected myinstance to be active")
	}
	if s.Instances["myinstance"].StartedAt == "" {
		t.Fatal("expected StartedAt to be set")
	}
}

func TestMarkInactive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	_ = MarkActive(path, "myinstance")
	if err := MarkInactive(path, "myinstance"); err != nil {
		t.Fatalf("MarkInactive: %v", err)
	}
	s, _ := LoadState(path)
	if _, ok := s.Instances["myinstance"]; ok {
		t.Fatal("expected myinstance to be gone")
	}
}

func TestMarkInactive_NonexistentInstance(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := MarkInactive(path, "ghost"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ── AppendCommandHistory ──────────────────────────────────────────────────────

func TestAppendCommandHistory_Basic(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := AppendCommandHistory(path, "start"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	s, _ := LoadState(path)
	if len(s.CommandHistory) != 1 || s.CommandHistory[0] != "start" {
		t.Fatalf("unexpected history: %v", s.CommandHistory)
	}
}

func TestAppendCommandHistory_CapsAtMax(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	for i := 0; i < maxHistoryLen+5; i++ {
		_ = AppendCommandHistory(path, "cmd")
	}
	s, _ := LoadState(path)
	if len(s.CommandHistory) != maxHistoryLen {
		t.Fatalf("expected %d history entries, got %d", maxHistoryLen, len(s.CommandHistory))
	}
}

func TestAppendCommandHistory_KeepsNewest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	for i := 0; i < maxHistoryLen; i++ {
		_ = AppendCommandHistory(path, "old")
	}
	_ = AppendCommandHistory(path, "newest")
	s, _ := LoadState(path)
	last := s.CommandHistory[len(s.CommandHistory)-1]
	if last != "newest" {
		t.Fatalf("expected 'newest' at end, got %q", last)
	}
}

// ── SaveTheme ─────────────────────────────────────────────────────────────────

func TestSaveTheme(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := SaveTheme(path, "catppuccin"); err != nil {
		t.Fatalf("SaveTheme: %v", err)
	}
	s, _ := LoadState(path)
	if s.Theme != "catppuccin" {
		t.Fatalf("expected theme=catppuccin, got %q", s.Theme)
	}
}

// ── SetCurrentInstance ────────────────────────────────────────────────────────

func TestSetCurrentInstance(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := SetCurrentInstance(path, "dev"); err != nil {
		t.Fatalf("SetCurrentInstance: %v", err)
	}
	s, _ := LoadState(path)
	if s.CurrentInstance != "dev" {
		t.Fatalf("expected dev, got %q", s.CurrentInstance)
	}
}

func TestSetCurrentInstance_Clear(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	_ = SetCurrentInstance(path, "dev")
	_ = SetCurrentInstance(path, "")
	s, _ := LoadState(path)
	if s.CurrentInstance != "" {
		t.Fatalf("expected empty, got %q", s.CurrentInstance)
	}
}

// ── WriteCustomConfig ─────────────────────────────────────────────────────────

func TestWriteCustomConfig(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	cfg := CustomInstanceConfig{
		Instance:   "myinst",
		CPU:        "4",
		RAM:        "4g",
		Components: []string{"checkout-backend", "user-bff"},
		Mode:       "dev",
	}
	if err := WriteCustomConfig(statePath, "myinst", cfg); err != nil {
		t.Fatalf("WriteCustomConfig: %v", err)
	}
	outPath := filepath.Join(dir, "myinst_selections.json")
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("selections file not created: %v", err)
	}
	content := string(data)
	for _, want := range []string{"checkout-backend", "user-bff", "\"mode\"", "dev"} {
		if !strings.Contains(content, want) {
			t.Errorf("expected %q in output, got:\n%s", want, content)
		}
	}
}

// ── LoadComponents ────────────────────────────────────────────────────────────

func TestLoadComponents_MissingFile(t *testing.T) {
	cf, err := LoadComponents("/nonexistent/components.json")
	if err != nil {
		t.Fatalf("missing file should not error, got: %v", err)
	}
	if len(cf.Systems) != 0 {
		t.Fatalf("expected empty, got %v", cf)
	}
}

func TestLoadComponents_Valid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "components.json")
	data := `{"systems":[{"name":"checkout","components":[{"name":"checkout-backend"},{"name":"checkout-bff"}]}]}`
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	cf, err := LoadComponents(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cf.Systems) != 1 {
		t.Fatalf("expected 1 system, got %d", len(cf.Systems))
	}
	if cf.Systems[0].Name != "checkout" {
		t.Fatalf("expected checkout, got %q", cf.Systems[0].Name)
	}
	if len(cf.Systems[0].Components) != 2 {
		t.Fatalf("expected 2 components, got %d", len(cf.Systems[0].Components))
	}
}

func TestLoadComponents_Invalid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "components.json")
	if err := os.WriteFile(path, []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadComponents(path)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}
