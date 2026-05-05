package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/thompsonja/its_tui/step"
)

// ── Test template helpers ─────────────────────────────────────────────────────

// These are simplified test versions of the template factories.
// We can't use the real ones from builtins due to circular import issues.

func MinikubeTemplate(args ...string) StepTemplate {
	return StepTemplate{
		ID:    "minikube",
		Panel: PanelTopLeft,
		Label: "Minikube",
		Fields: []FieldSpec{
			{ID: "cpu", Label: "CPU", Kind: FieldKindSelect, OptionsFunc: StaticOptions("2", "4", "8", "16"), Default: 1},
			{ID: "ram", Label: "RAM", Kind: FieldKindSelect, OptionsFunc: StaticOptions("2g", "4g", "8g", "16g"), Default: 1},
		},
		Build: func(v WizardValues) (step.Step, error) {
			return &fakeStep{id: "minikube"}, nil
		},
	}
}

func KubectlTemplate() StepTemplate {
	return StepTemplate{
		ID:           "kubectl",
		Panel:        PanelTopLeft,
		Label:        "kubectl",
		WaitFor:      []string{"minikube"},
		AutoActivate: true,
		Hidden:       true,
		Build: func(v WizardValues) (step.Step, error) {
			return &fakeStep{id: "kubectl"}, nil
		},
	}
}

func SkaffoldTemplate(generate func(v WizardValues) (path string, profiles []string, err error), systemsFunc ...func(WizardValues) []System) StepTemplate {
	fields := []FieldSpec{
		{ID: "mode", Label: "Mode", Kind: FieldKindSelect, OptionsFunc: StaticOptions("dev", "run", "debug"), Default: 0},
	}

	// If a SystemsFunc is provided, add a components field for testing
	if len(systemsFunc) > 0 && systemsFunc[0] != nil {
		fields = append([]FieldSpec{{
			ID:          "components",
			Label:       "Components",
			Kind:        FieldKindSystemSelect,
			SystemsFunc: systemsFunc[0],
		}}, fields...)
	}

	return StepTemplate{
		ID:      "skaffold",
		Panel:   PanelTopRight,
		Label:   "Skaffold",
		WaitFor: []string{"minikube"},
		Fields:  fields,
		Build: func(v WizardValues) (step.Step, error) {
			if generate != nil {
				path, _, err := generate(v)
				if err != nil || path == "" {
					return nil, err
				}
			}
			return &fakeStep{id: "skaffold"}, nil
		},
	}
}

func MFETemplate(mfes []string, run interface{}) StepTemplate {
	return StepTemplate{
		ID:    "mfe",
		Panel: PanelBottomRight,
		Label: "MFE",
		Fields: []FieldSpec{
			{ID: "mfe", Label: "MFE", Kind: FieldKindSingleSelect, OptionsFunc: StaticOptions(mfes...)},
		},
		Build: func(v WizardValues) (step.Step, error) {
			mfe := v.String("mfe")
			if mfe == "" {
				return nil, nil
			}
			return &fakeStep{id: "mfe"}, nil
		},
	}
}

// ── fakeStep ──────────────────────────────────────────────────────────────────

// fakeStep is a test double for the Step interface.
type fakeStep struct {
	id       string
	logPath  string
	startErr error
}

func (f *fakeStep) ID() string                                   { return f.id }
func (f *fakeStep) LogPath(name string) string                   { return f.logPath }
func (f *fakeStep) Start(ctx context.Context, name string) error { return f.startErr }

// fakeBuild returns a Build function that always returns a fakeStep with the
// given ID, or the given error.
func fakeBuild(id string, err error) func(WizardValues) (Step, error) {
	return func(v WizardValues) (Step, error) {
		if err != nil {
			return nil, err
		}
		return &fakeStep{id: id}, nil
	}
}

// ── buildDefsFromTemplates ────────────────────────────────────────────────────

func TestBuildDefsFromTemplates_SkipsNilStep(t *testing.T) {
	m := &model{app: appState{cfg: Config{Steps: []StepTemplate{{
		Label: "opt",
		Build: func(v WizardValues) (Step, error) { return nil, nil },
	}}}}}
	defs, err := m.buildDefsFromTemplates(WizardValues{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(defs) != 0 {
		t.Fatalf("expected 0 defs for nil step, got %d", len(defs))
	}
}

func TestBuildDefsFromTemplates_PropagatesError(t *testing.T) {
	m := &model{app: appState{cfg: Config{Steps: []StepTemplate{{
		Label: "bad",
		Build: fakeBuild("", errors.New("boom")),
	}}}}}
	_, err := m.buildDefsFromTemplates(WizardValues{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestBuildDefsFromTemplates_ErrorIncludesLabel(t *testing.T) {
	m := &model{app: appState{cfg: Config{Steps: []StepTemplate{{
		Label: "my-step",
		Build: fakeBuild("", errors.New("oops")),
	}}}}}
	_, err := m.buildDefsFromTemplates(WizardValues{})
	if err == nil || err.Error() == "" {
		t.Fatal("expected non-empty error")
	}
	// Error should mention the template label.
	if !strings.Contains(err.Error(), "my-step") {
		t.Fatalf("error should mention template label, got: %v", err)
	}
}

func TestBuildDefsFromTemplates_RejectsDuplicateStepIDs(t *testing.T) {
	m := &model{app: appState{cfg: Config{Steps: []StepTemplate{
		{
			Label: "first",
			Build: fakeBuild("duplicate-id", nil),
		},
		{
			Label: "second",
			Build: fakeBuild("duplicate-id", nil),
		},
	}}}}
	_, err := m.buildDefsFromTemplates(WizardValues{})
	if err == nil {
		t.Fatal("expected error for duplicate step IDs, got nil")
	}
	// Error should mention both labels and the duplicate ID.
	errMsg := err.Error()
	if !strings.Contains(errMsg, "duplicate-id") {
		t.Fatalf("error should mention duplicate ID, got: %v", err)
	}
	if !strings.Contains(errMsg, "first") || !strings.Contains(errMsg, "second") {
		t.Fatalf("error should mention both step labels, got: %v", err)
	}
}

func TestBuildDefsFromTemplates_UsesLabelFunc(t *testing.T) {
	m := &model{app: appState{cfg: Config{Steps: []StepTemplate{{
		Label:     "base",
		LabelFunc: func(v WizardValues) string { return "dynamic-" + v.String("x") },
		Build:     fakeBuild("s", nil),
	}}}}}
	vals := NewWizardValues(map[string]string{"x": "42"}, nil)
	defs, err := m.buildDefsFromTemplates(vals)
	if err != nil {
		t.Fatal(err)
	}
	if defs[0].meta.label != "dynamic-42" {
		t.Fatalf("expected dynamic-42, got %q", defs[0].meta.label)
	}
}

func TestBuildDefsFromTemplates_FallsBackToLabel(t *testing.T) {
	m := &model{app: appState{cfg: Config{Steps: []StepTemplate{{
		Label: "static",
		Build: fakeBuild("s", nil),
	}}}}}
	defs, _ := m.buildDefsFromTemplates(WizardValues{})
	if defs[0].meta.label != "static" {
		t.Fatalf("expected static, got %q", defs[0].meta.label)
	}
}

func TestBuildDefsFromTemplates_WiresPanel(t *testing.T) {
	m := &model{app: appState{cfg: Config{Steps: []StepTemplate{{
		Label: "x",
		Panel: PanelTopRight,
		Build: fakeBuild("x", nil),
	}}}}}
	defs, _ := m.buildDefsFromTemplates(WizardValues{})
	if defs[0].meta.panel != PanelTopRight {
		t.Fatalf("expected PanelTopRight, got %d", defs[0].meta.panel)
	}
}

func TestBuildDefsFromTemplates_WiresWaitFor(t *testing.T) {
	m := &model{app: appState{cfg: Config{Steps: []StepTemplate{{
		Label:   "x",
		WaitFor: []string{"dep"},
		Build:   fakeBuild("x", nil),
	}}}}}
	defs, _ := m.buildDefsFromTemplates(WizardValues{})
	if defs[0].meta.waitFor[0] != "dep" {
		t.Fatalf("expected dep, got %q", defs[0].meta.waitFor)
	}
}

func TestBuildDefsFromTemplates_WiresMultipleWaitFor(t *testing.T) {
	m := &model{app: appState{cfg: Config{Steps: []StepTemplate{{
		Label:   "x",
		WaitFor: []string{"a", "b"},
		Build:   fakeBuild("x", nil),
	}}}}}
	defs, _ := m.buildDefsFromTemplates(WizardValues{})
	if len(defs[0].meta.waitFor) != 2 {
		t.Fatalf("expected 2 WaitFor entries, got %d", len(defs[0].meta.waitFor))
	}
	if defs[0].meta.waitFor[0] != "a" || defs[0].meta.waitFor[1] != "b" {
		t.Fatalf("expected [a b], got %v", defs[0].meta.waitFor)
	}
}

func TestBuildDefsFromTemplates_WiresOnReady(t *testing.T) {
	var got string
	m := &model{app: appState{
		cfg: Config{Steps: []StepTemplate{{
			Label:   "x",
			OnReady: func(sp string) { got = sp },
			Build:   fakeBuild("x", nil),
		}}},
		statePath: "/test/state.json",
	}}
	defs, _ := m.buildDefsFromTemplates(WizardValues{})
	defs[0].meta.onReady()
	if got != "/test/state.json" {
		t.Fatalf("expected statePath, got %q", got)
	}
}

func TestBuildDefsFromTemplates_PassesValuesToBuild(t *testing.T) {
	var received WizardValues
	m := &model{app: appState{cfg: Config{Steps: []StepTemplate{{
		Label: "x",
		Build: func(v WizardValues) (Step, error) {
			received = v
			return &fakeStep{id: "x"}, nil
		},
	}}}}}
	vals := NewWizardValues(map[string]string{"cpu": "8"}, nil)
	m.buildDefsFromTemplates(vals)
	if received.String("cpu") != "8" {
		t.Fatalf("expected cpu=8, got %q", received.String("cpu"))
	}
}

func TestBuildDefsFromTemplates_MultipleTemplates(t *testing.T) {
	m := &model{app: appState{cfg: Config{Steps: []StepTemplate{
		{Label: "a", Build: fakeBuild("a", nil)},
		{Label: "b", Build: func(v WizardValues) (Step, error) { return nil, nil }}, // skipped
		{Label: "c", Build: fakeBuild("c", nil)},
	}}}}
	defs, _ := m.buildDefsFromTemplates(WizardValues{})
	if len(defs) != 2 {
		t.Fatalf("expected 2 defs (b skipped), got %d", len(defs))
	}
}

func TestBuildDefsFromTemplates_NilOnReadyNotWired(t *testing.T) {
	m := &model{app: appState{cfg: Config{Steps: []StepTemplate{{
		Label:   "x",
		OnReady: nil,
		Build:   fakeBuild("x", nil),
	}}}}}
	defs, _ := m.buildDefsFromTemplates(WizardValues{})
	if defs[0].meta.onReady != nil {
		t.Fatal("OnReady should be nil when template.OnReady is nil")
	}
}

// ── buildPipelineFromState ────────────────────────────────────────────────────

func TestBuildPipelineFromState_PassesStringValues(t *testing.T) {
	var gotMode string
	m := &model{app: appState{cfg: Config{Steps: []StepTemplate{{
		Label: "x",
		Build: func(v WizardValues) (Step, error) {
			gotMode = v.String("mode")
			return &fakeStep{id: "x"}, nil
		},
	}}}}}
	inst := &InstanceState{
		StringValues: map[string]string{"mode": "debug"},
		SliceValues:  map[string][]string{},
	}
	defs := m.buildPipelineFromState("test", inst)
	if len(defs) != 1 {
		t.Fatalf("expected 1 def, got %d", len(defs))
	}
	if gotMode != "debug" {
		t.Fatalf("expected debug, got %q", gotMode)
	}
}

func TestBuildPipelineFromState_PassesSliceValues(t *testing.T) {
	var gotComps []string
	m := &model{app: appState{cfg: Config{Steps: []StepTemplate{{
		Label: "x",
		Build: func(v WizardValues) (Step, error) {
			gotComps = v.Strings("components")
			return &fakeStep{id: "x"}, nil
		},
	}}}}}
	inst := &InstanceState{
		StringValues: map[string]string{},
		SliceValues:  map[string][]string{"components": {"a", "b"}},
	}
	m.buildPipelineFromState("test", inst)
	if len(gotComps) != 2 || gotComps[0] != "a" || gotComps[1] != "b" {
		t.Fatalf("unexpected components: %v", gotComps)
	}
}

func TestBuildPipelineFromState_NilMapsAreSafe(t *testing.T) {
	m := &model{app: appState{cfg: Config{Steps: []StepTemplate{{
		Label: "x",
		Build: func(v WizardValues) (Step, error) {
			// Should not panic on nil maps.
			_ = v.String("anything")
			_ = v.Strings("anything")
			return &fakeStep{id: "x"}, nil
		},
	}}}}}
	// InstanceState with nil maps.
	inst := &InstanceState{}
	defs := m.buildPipelineFromState("test", inst)
	if len(defs) != 1 {
		t.Fatalf("expected 1 def, got %d", len(defs))
	}
}

func TestBuildPipelineFromState_IgnoresBuildError(t *testing.T) {
	// Errors during session restore are silently dropped so we don't crash.
	m := &model{app: appState{cfg: Config{Steps: []StepTemplate{{
		Label: "x",
		Build: fakeBuild("", errors.New("generate failed")),
	}}}}}
	inst := &InstanceState{}
	defs := m.buildPipelineFromState("test", inst)
	if len(defs) != 0 {
		t.Fatalf("expected 0 defs on error, got %d", len(defs))
	}
}

// ── WizardValues / buildValues ────────────────────────────────────────────────

func TestBuildValues_Select(t *testing.T) {
	wiz := &startWizard{
		states: []fieldState{{
			spec:            FieldSpec{ID: "cpu", Kind: FieldKindSelect, OptionsFunc: StaticOptions("2", "4", "8")},
			resolvedOptions: []string{"2", "4", "8"},
			selectIdx:       1,
		}},
	}
	v := wiz.buildValues()
	if v.String("cpu") != "4" {
		t.Fatalf("expected 4, got %q", v.String("cpu"))
	}
}

func TestBuildValues_Select_OutOfRange(t *testing.T) {
	wiz := &startWizard{
		states: []fieldState{{
			spec:            FieldSpec{ID: "cpu", Kind: FieldKindSelect, OptionsFunc: StaticOptions("2", "4")},
			resolvedOptions: []string{"2", "4"},
			selectIdx:       99, // out of range
		}},
	}
	v := wiz.buildValues()
	if v.String("cpu") != "" {
		t.Fatalf("expected empty for out-of-range index, got %q", v.String("cpu"))
	}
}

func TestBuildValues_SingleSelect(t *testing.T) {
	wiz := &startWizard{
		states: []fieldState{{
			spec:        FieldSpec{ID: "mfe", Kind: FieldKindSingleSelect},
			singleValue: "checkout-mfe",
		}},
	}
	v := wiz.buildValues()
	if v.String("mfe") != "checkout-mfe" {
		t.Fatalf("expected checkout-mfe, got %q", v.String("mfe"))
	}
}

func TestBuildValues_SingleSelect_Empty(t *testing.T) {
	wiz := &startWizard{
		states: []fieldState{{
			spec:        FieldSpec{ID: "mfe", Kind: FieldKindSingleSelect},
			singleValue: "",
		}},
	}
	v := wiz.buildValues()
	if v.String("mfe") != "" {
		t.Fatalf("expected empty, got %q", v.String("mfe"))
	}
}

func TestBuildValues_SystemSelect(t *testing.T) {
	wiz := &startWizard{
		states: []fieldState{{
			spec:        FieldSpec{ID: "components", Kind: FieldKindSystemSelect},
			multiValues: []string{"checkout-backend", "user-bff"},
		}},
	}
	v := wiz.buildValues()
	comps := v.Strings("components")
	if len(comps) != 2 || comps[0] != "checkout-backend" || comps[1] != "user-bff" {
		t.Fatalf("unexpected components: %v", comps)
	}
}

func TestBuildValues_MultipleFields(t *testing.T) {
	wiz := &startWizard{
		states: []fieldState{
			{spec: FieldSpec{ID: "cpu", Kind: FieldKindSelect, OptionsFunc: StaticOptions("2", "4")}, resolvedOptions: []string{"2", "4"}, selectIdx: 0},
			{spec: FieldSpec{ID: "mfe", Kind: FieldKindSingleSelect}, singleValue: "my-mfe"},
			{spec: FieldSpec{ID: "tags", Kind: FieldKindMultiSelect}, multiValues: []string{"a", "b"}},
		},
	}
	v := wiz.buildValues()
	if v.String("cpu") != "2" {
		t.Fatalf("cpu: expected 2, got %q", v.String("cpu"))
	}
	if v.String("mfe") != "my-mfe" {
		t.Fatalf("mfe: expected my-mfe, got %q", v.String("mfe"))
	}
	if tags := v.Strings("tags"); len(tags) != 2 {
		t.Fatalf("tags: expected 2, got %v", tags)
	}
}

// ── validateTemplates ─────────────────────────────────────────────────────────

func TestValidateTemplates_Empty(t *testing.T) {
	if err := validateTemplates(nil); err != nil {
		t.Fatalf("empty slice should be valid: %v", err)
	}
}

func TestValidateTemplates_NilBuild(t *testing.T) {
	err := validateTemplates([]StepTemplate{{Label: "bad"}})
	if err == nil {
		t.Fatal("expected error for nil Build")
	}
}

func TestValidateTemplates_NilBuild_UnlabeledTemplate(t *testing.T) {
	err := validateTemplates([]StepTemplate{{}})
	if err == nil {
		t.Fatal("expected error for unlabeled template with nil Build")
	}
}

func TestValidateTemplates_InvalidPanel_TooHigh(t *testing.T) {
	err := validateTemplates([]StepTemplate{{
		Label: "x",
		Panel: PanelID(99),
		Build: fakeBuild("x", nil),
	}})
	if err == nil {
		t.Fatal("expected error for Panel=99")
	}
}

func TestValidateTemplates_InvalidPanel_Negative(t *testing.T) {
	err := validateTemplates([]StepTemplate{{
		Label: "x",
		Panel: PanelID(-2),
		Build: fakeBuild("x", nil),
	}})
	if err == nil {
		t.Fatal("expected error for Panel=-2")
	}
}

func TestValidateTemplates_PanelNone_Valid(t *testing.T) {
	err := validateTemplates([]StepTemplate{{
		Label: "x",
		Panel: PanelNone,
		Build: fakeBuild("x", nil),
	}})
	if err != nil {
		t.Fatalf("PanelNone should be valid: %v", err)
	}
}

func TestValidateTemplates_DuplicateID(t *testing.T) {
	tmpl := func(id string) StepTemplate {
		return StepTemplate{ID: id, Label: id, Build: fakeBuild(id, nil)}
	}
	err := validateTemplates([]StepTemplate{tmpl("x"), tmpl("x")})
	if err == nil {
		t.Fatal("expected error for duplicate ID")
	}
}

func TestValidateTemplates_UnknownWaitFor(t *testing.T) {
	err := validateTemplates([]StepTemplate{
		{ID: "a", Label: "a", Build: fakeBuild("a", nil)},
		{ID: "b", Label: "b", WaitFor: []string{"nonexistent"}, Build: fakeBuild("b", nil)},
	})
	if err == nil {
		t.Fatal("expected error for unknown WaitFor")
	}
}

func TestValidateTemplates_ValidWaitFor(t *testing.T) {
	err := validateTemplates([]StepTemplate{
		{ID: "a", Label: "a", Build: fakeBuild("a", nil)},
		{ID: "b", Label: "b", WaitFor: []string{"a"}, Build: fakeBuild("b", nil)},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateTemplates_MultipleWaitFor_AllValid(t *testing.T) {
	err := validateTemplates([]StepTemplate{
		{ID: "a", Label: "a", Build: fakeBuild("a", nil)},
		{ID: "b", Label: "b", Build: fakeBuild("b", nil)},
		{ID: "c", Label: "c", WaitFor: []string{"a", "b"}, Build: fakeBuild("c", nil)},
	})
	if err != nil {
		t.Fatalf("unexpected error for valid multi-dep WaitFor: %v", err)
	}
}

func TestValidateTemplates_MultipleWaitFor_OneInvalid(t *testing.T) {
	err := validateTemplates([]StepTemplate{
		{ID: "a", Label: "a", Build: fakeBuild("a", nil)},
		{ID: "b", Label: "b", WaitFor: []string{"a", "nonexistent"}, Build: fakeBuild("b", nil)},
	})
	if err == nil {
		t.Fatal("expected error when one dep in WaitFor list is unknown")
	}
	if !strings.Contains(err.Error(), "nonexistent") {
		t.Fatalf("error should name the unknown dep, got: %v", err)
	}
}

func TestValidateTemplates_WaitForSkippedWithoutIDs(t *testing.T) {
	// When no templates have IDs, WaitFor validation is skipped.
	err := validateTemplates([]StepTemplate{
		{Label: "a", WaitFor: []string{"unknown"}, Build: fakeBuild("a", nil)},
	})
	if err != nil {
		t.Fatalf("should not error when IDs are not set: %v", err)
	}
}

func TestValidateTemplates_WaitForValidatedWithPartialIDs(t *testing.T) {
	// When SOME templates have IDs, WaitFor is validated even for templates without IDs.
	// This catches typos in configs like skaffold pipelines where the dev template
	// has no ID but its WaitFor deps reference stable templates that do.
	err := validateTemplates([]StepTemplate{
		{ID: "minikube", Label: "minikube", Build: fakeBuild("minikube", nil)},
		{Label: "dev", WaitFor: []string{"minikube_typo"}, Build: fakeBuild("dev", nil)},
	})
	if err == nil {
		t.Fatal("expected error: WaitFor dep is unknown, even though template has no ID")
	}
	if !strings.Contains(err.Error(), "minikube_typo") {
		t.Fatalf("error should name the unknown dep, got: %v", err)
	}
}

func TestValidateTemplates_NoIDTemplate_ValidWaitFor(t *testing.T) {
	// A template without its own ID can still have valid WaitFor deps.
	err := validateTemplates([]StepTemplate{
		{ID: "minikube", Label: "minikube", Build: fakeBuild("minikube", nil)},
		{Label: "dev", WaitFor: []string{"minikube"}, Build: fakeBuild("dev", nil)},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateTemplates_AllValidTemplates(t *testing.T) {
	err := validateTemplates([]StepTemplate{
		MinikubeTemplate(),
		KubectlTemplate(),
		SkaffoldTemplate(nil, StaticSystems()),
		MFETemplate(nil, nil),
	})
	if err != nil {
		t.Fatalf("provided templates should be valid: %v", err)
	}
}

// ── FieldSpec.Options shorthand ───────────────────────────────────────────────

func TestValidateTemplates_FieldSpec_Options_PassesWithoutOptionsFunc(t *testing.T) {
	err := validateTemplates([]StepTemplate{{
		Label: "x",
		Build: fakeBuild("x", nil),
		Fields: []FieldSpec{
			{ID: "env", Label: "Environment", Kind: FieldKindSelect, Options: []string{"dev", "test"}},
		},
	}})
	if err != nil {
		t.Fatalf("Options without OptionsFunc should be valid: %v", err)
	}
}

func TestValidateTemplates_FieldSpec_Options_ErrorsWithNeither(t *testing.T) {
	err := validateTemplates([]StepTemplate{{
		Label: "x",
		Build: fakeBuild("x", nil),
		Fields: []FieldSpec{
			{ID: "env", Label: "Environment", Kind: FieldKindSelect},
		},
	}})
	if err == nil {
		t.Fatal("expected error when neither Options nor OptionsFunc is set")
	}
}

func TestNewStartWizard_FieldSpec_Options_ResolvesCorrectly(t *testing.T) {
	cfg := Config{Steps: []StepTemplate{{
		Label: "x",
		Build: fakeBuild("x", nil),
		Fields: []FieldSpec{
			{ID: "env", Label: "Environment", Kind: FieldKindSelect, Options: []string{"dev", "test"}, Default: 1},
		},
	}}}
	wiz := newStartWizard(cfg, 0, WizardValues{})
	if len(wiz.states) != 1 {
		t.Fatalf("expected 1 field state, got %d", len(wiz.states))
	}
	s := wiz.states[0]
	if len(s.resolvedOptions) != 2 {
		t.Fatalf("expected 2 resolved options, got %d: %v", len(s.resolvedOptions), s.resolvedOptions)
	}
	if s.resolvedOptions[0] != "dev" || s.resolvedOptions[1] != "test" {
		t.Fatalf("unexpected options: %v", s.resolvedOptions)
	}
	if s.selectIdx != 1 {
		t.Fatalf("expected Default=1 to set selectIdx=1, got %d", s.selectIdx)
	}
}

func TestNewStartWizard_FieldSpec_OptionsFuncOverridesOptions(t *testing.T) {
	// When both Options and OptionsFunc are set, OptionsFunc takes precedence.
	cfg := Config{Steps: []StepTemplate{{
		Label: "x",
		Build: fakeBuild("x", nil),
		Fields: []FieldSpec{{
			ID:          "env",
			Kind:        FieldKindSelect,
			Options:     []string{"ignored"},
			OptionsFunc: StaticOptions("a", "b", "c"),
		}},
	}}}
	wiz := newStartWizard(cfg, 0, WizardValues{})
	s := wiz.states[0]
	if len(s.resolvedOptions) != 3 || s.resolvedOptions[0] != "a" {
		t.Fatalf("OptionsFunc should override Options, got: %v", s.resolvedOptions)
	}
}

// ── Wizard pre-population ─────────────────────────────────────────────────────

func TestNewStartWizard_PrePopulatesSelectField(t *testing.T) {
	m := &model{app: appState{cfg: Config{Steps: []StepTemplate{MinikubeTemplate()}}}}
	// CPU options: "2"(0), "4"(1), "8"(2), "16"(3) — select "8"
	initial := NewWizardValues(map[string]string{"cpu": "8"}, nil)
	wiz := newStartWizard(m.app.cfg, 0, initial)
	cpuState := wiz.states[0] // first field is cpu
	if cpuState.selectIdx != 2 {
		t.Fatalf("expected cpuIdx=2 (\"8\"), got %d", cpuState.selectIdx)
	}
}

func TestNewStartWizard_PrePopulatesSelectUnknownValue(t *testing.T) {
	m := &model{app: appState{cfg: Config{Steps: []StepTemplate{MinikubeTemplate()}}}}
	// Unknown value falls back to Default (index 1 for CPU).
	initial := NewWizardValues(map[string]string{"cpu": "999"}, nil)
	wiz := newStartWizard(m.app.cfg, 0, initial)
	if wiz.states[0].selectIdx != 1 {
		t.Fatalf("expected default index 1, got %d", wiz.states[0].selectIdx)
	}
}

func TestNewStartWizard_PrePopulatesSingleSelect(t *testing.T) {
	m := &model{app: appState{cfg: Config{Steps: []StepTemplate{
		MFETemplate([]string{"checkout-mfe", "user-mfe"}, nil),
	}}}}
	initial := NewWizardValues(map[string]string{"mfe": "user-mfe"}, nil)
	wiz := newStartWizard(m.app.cfg, 0, initial)
	if wiz.states[0].singleValue != "user-mfe" {
		t.Fatalf("expected user-mfe, got %q", wiz.states[0].singleValue)
	}
}

func TestNewStartWizard_PrePopulatesSystemSelect(t *testing.T) {
	systems := []System{{
		Name:       "checkout",
		Components: []Component{{Name: "checkout-backend"}, {Name: "checkout-bff"}},
	}}
	m := &model{app: appState{cfg: Config{Steps: []StepTemplate{
		SkaffoldTemplate(nil, func(v WizardValues) []System { return systems }),
	}}}}
	initial := NewWizardValues(
		map[string]string{"mode": "debug"},
		map[string][]string{"components": {"checkout-backend"}},
	)
	wiz := newStartWizard(m.app.cfg, 0, initial)

	// Field 0 is "components" (SystemSelect), field 1 is "mode" (Select).
	compState := wiz.states[0]
	if len(compState.multiValues) != 1 || compState.multiValues[0] != "checkout-backend" {
		t.Fatalf("expected [checkout-backend], got %v", compState.multiValues)
	}
	modeState := wiz.states[1]
	// mode options: "dev"(0), "run"(1), "debug"(2)
	if modeState.selectIdx != 2 {
		t.Fatalf("expected modeIdx=2 (\"debug\"), got %d", modeState.selectIdx)
	}
}

func TestNewStartWizard_EmptyInitialLeavesDefaults(t *testing.T) {
	m := &model{app: appState{cfg: Config{Steps: []StepTemplate{MinikubeTemplate()}}}}
	wiz := newStartWizard(m.app.cfg, 0, WizardValues{})
	// CPU default is index 1 ("4"), RAM default is index 1 ("4g").
	if wiz.states[0].selectIdx != 1 {
		t.Fatalf("expected default cpuIdx=1, got %d", wiz.states[0].selectIdx)
	}
	if wiz.states[1].selectIdx != 1 {
		t.Fatalf("expected default ramIdx=1, got %d", wiz.states[1].selectIdx)
	}
}


// ── reEvalDynamicFields ───────────────────────────────────────────────────────

func TestReEvalDynamicFields_SystemsFunc_ReactsToSelectChange(t *testing.T) {
	devSystems := []System{{
		Name:       "dev-sys",
		Components: []Component{{Name: "dev-comp-a"}, {Name: "dev-comp-b"}},
	}}
	testSystems := []System{{
		Name:       "test-sys",
		Components: []Component{{Name: "test-comp-x"}},
	}}

	wiz := &startWizard{
		states: []fieldState{
			{
				spec:            FieldSpec{ID: "env", Kind: FieldKindSelect, OptionsFunc: StaticOptions("dev", "test")},
				resolvedOptions: []string{"dev", "test"},
				selectIdx:       0, // "dev"
			},
			{
				spec: FieldSpec{
					ID:   "components",
					Kind: FieldKindSystemSelect,
					SystemsFunc: func(v WizardValues) []System {
						if v.String("env") == "test" {
							return testSystems
						}
						return devSystems
					},
				},
				resolvedSystems: devSystems,
				sysPickerItems: []pickerItem{
					{isSystem: true, system: "dev-sys"},
					{isSystem: false, system: "dev-sys", comp: "dev-comp-a"},
					{isSystem: false, system: "dev-sys", comp: "dev-comp-b"},
				},
			},
		},
	}

	// Simulate switching env to "test".
	wiz.states[0].selectIdx = 1
	wiz.reEvalDynamicFields()

	items := wiz.states[1].sysPickerItems
	if len(items) != 2 { // 1 header + 1 component
		t.Fatalf("expected 2 picker items for test env, got %d: %v", len(items), items)
	}
	if items[0].system != "test-sys" {
		t.Fatalf("expected test-sys header, got %q", items[0].system)
	}
}

func TestReEvalDynamicFields_SystemsFunc_FiltersStaleSelections(t *testing.T) {
	devSystems := []System{{
		Name:       "dev-sys",
		Components: []Component{{Name: "dev-comp-a"}},
	}}
	testSystems := []System{{
		Name:       "test-sys",
		Components: []Component{{Name: "test-comp-x"}},
	}}

	wiz := &startWizard{
		states: []fieldState{
			{
				spec:            FieldSpec{ID: "env", Kind: FieldKindSelect, OptionsFunc: StaticOptions("dev", "test")},
				resolvedOptions: []string{"dev", "test"},
				selectIdx:       0,
			},
			{
				spec: FieldSpec{
					ID:   "components",
					Kind: FieldKindSystemSelect,
					SystemsFunc: func(v WizardValues) []System {
						if v.String("env") == "test" {
							return testSystems
						}
						return devSystems
					},
				},
				resolvedSystems: devSystems,
				multiValues:     []string{"dev-comp-a"}, // selected in dev
				sysPickerItems: []pickerItem{
					{isSystem: true, system: "dev-sys"},
					{isSystem: false, system: "dev-sys", comp: "dev-comp-a"},
				},
			},
		},
	}

	// Switch to test env — "dev-comp-a" no longer exists.
	wiz.states[0].selectIdx = 1
	wiz.reEvalDynamicFields()

	if len(wiz.states[1].multiValues) != 0 {
		t.Fatalf("expected stale selection to be removed, got %v", wiz.states[1].multiValues)
	}
}

func TestReEvalDynamicFields_OptionsFunc_ReactsToSelectChange(t *testing.T) {
	wiz := &startWizard{
		states: []fieldState{
			{
				spec:            FieldSpec{ID: "env", Kind: FieldKindSelect, OptionsFunc: StaticOptions("dev", "prod")},
				resolvedOptions: []string{"dev", "prod"},
				selectIdx:       0,
			},
			{
				spec: FieldSpec{
					ID:   "ns",
					Kind: FieldKindSingleSelect,
					OptionsFunc: func(v WizardValues) []string {
						if v.String("env") == "prod" {
							return []string{"prod-ns-1", "prod-ns-2"}
						}
						return []string{"dev-ns-1"}
					},
				},
				resolvedOptions: []string{"dev-ns-1"},
				strPickerItems:  []string{"dev-ns-1"},
			},
		},
	}

	wiz.states[0].selectIdx = 1 // switch to prod
	wiz.reEvalDynamicFields()

	items := wiz.states[1].strPickerItems
	if len(items) != 2 {
		t.Fatalf("expected 2 prod namespaces, got %d: %v", len(items), items)
	}
	if items[0] != "prod-ns-1" {
		t.Fatalf("expected prod-ns-1, got %q", items[0])
	}
}

func TestReEvalDynamicFields_OptionsFunc_ClearsSingleValueIfGone(t *testing.T) {
	wiz := &startWizard{
		states: []fieldState{
			{
				spec:            FieldSpec{ID: "env", Kind: FieldKindSelect, OptionsFunc: StaticOptions("dev", "prod")},
				resolvedOptions: []string{"dev", "prod"},
				selectIdx:       0,
			},
			{
				spec: FieldSpec{
					ID:   "ns",
					Kind: FieldKindSingleSelect,
					OptionsFunc: func(v WizardValues) []string {
						if v.String("env") == "prod" {
							return []string{"prod-ns-1"}
						}
						return []string{"dev-ns-1"}
					},
				},
				resolvedOptions: []string{"dev-ns-1"},
				strPickerItems:  []string{"dev-ns-1"},
				singleValue:     "dev-ns-1", // currently selected
			},
		},
	}

	wiz.states[0].selectIdx = 1 // switch to prod
	wiz.reEvalDynamicFields()

	if wiz.states[1].singleValue != "" {
		t.Fatalf("expected singleValue to be cleared, got %q", wiz.states[1].singleValue)
	}
}

func TestReEvalDynamicFields_MergeValuesFunc_AddsValues(t *testing.T) {
	wiz := &startWizard{
		states: []fieldState{
			{
				spec:            FieldSpec{ID: "suite", Kind: FieldKindSingleSelect, OptionsFunc: StaticOptions("suite-a", "suite-b")},
				resolvedOptions: []string{"suite-a", "suite-b"},
				strPickerItems:  []string{"suite-a", "suite-b"},
				singleValue:     "suite-a",
			},
			{
				spec: FieldSpec{
					ID:          "components",
					Kind:        FieldKindSystemSelect,
					SystemsFunc: StaticSystems(System{Name: "sys", Components: []Component{{Name: "comp-x"}, {Name: "comp-y"}, {Name: "comp-z"}}}),
					MergeValuesFunc: func(v WizardValues) []string {
						if v.String("suite") == "suite-a" {
							return []string{"comp-x", "comp-y"}
						}
						return nil
					},
				},
				resolvedSystems: []System{{Name: "sys", Components: []Component{{Name: "comp-x"}, {Name: "comp-y"}, {Name: "comp-z"}}}},
				sysPickerItems: []pickerItem{
					{isSystem: true, system: "sys"},
					{isSystem: false, system: "sys", comp: "comp-x"},
					{isSystem: false, system: "sys", comp: "comp-y"},
					{isSystem: false, system: "sys", comp: "comp-z"},
				},
			},
		},
	}

	wiz.reEvalDynamicFields()

	got := wiz.states[1].multiValues
	if len(got) != 2 || got[0] != "comp-x" || got[1] != "comp-y" {
		t.Fatalf("expected [comp-x comp-y], got %v", got)
	}
}

func TestReEvalDynamicFields_MergeValuesFunc_RemovesOldAutoValues(t *testing.T) {
	wiz := &startWizard{
		states: []fieldState{
			{
				spec:            FieldSpec{ID: "suite", Kind: FieldKindSingleSelect, OptionsFunc: StaticOptions("suite-a", "suite-b")},
				resolvedOptions: []string{"suite-a", "suite-b"},
				strPickerItems:  []string{"suite-a", "suite-b"},
				singleValue:     "suite-a",
			},
			{
				spec: FieldSpec{
					ID:          "components",
					Kind:        FieldKindSystemSelect,
					SystemsFunc: StaticSystems(System{Name: "sys", Components: []Component{{Name: "comp-x"}, {Name: "comp-y"}, {Name: "comp-z"}}}),
					MergeValuesFunc: func(v WizardValues) []string {
						switch v.String("suite") {
						case "suite-a":
							return []string{"comp-x", "comp-y"}
						case "suite-b":
							return []string{"comp-z"}
						}
						return nil
					},
				},
				resolvedSystems: []System{{Name: "sys", Components: []Component{{Name: "comp-x"}, {Name: "comp-y"}, {Name: "comp-z"}}}},
				sysPickerItems: []pickerItem{
					{isSystem: true, system: "sys"},
					{isSystem: false, system: "sys", comp: "comp-x"},
					{isSystem: false, system: "sys", comp: "comp-y"},
					{isSystem: false, system: "sys", comp: "comp-z"},
				},
			},
		},
	}

	// First eval with suite-a: auto-selects comp-x, comp-y.
	wiz.reEvalDynamicFields()
	if got := wiz.states[1].multiValues; len(got) != 2 {
		t.Fatalf("after suite-a: expected 2 auto values, got %v", got)
	}

	// Switch to suite-b: comp-x, comp-y removed; comp-z added.
	wiz.states[0].singleValue = "suite-b"
	wiz.reEvalDynamicFields()

	got := wiz.states[1].multiValues
	if len(got) != 1 || got[0] != "comp-z" {
		t.Fatalf("after suite-b: expected [comp-z], got %v", got)
	}
}

func TestReEvalDynamicFields_MergeValuesFunc_PreservesManualSelections(t *testing.T) {
	wiz := &startWizard{
		states: []fieldState{
			{
				spec:            FieldSpec{ID: "suite", Kind: FieldKindSingleSelect, OptionsFunc: StaticOptions("suite-a", "suite-b")},
				resolvedOptions: []string{"suite-a", "suite-b"},
				strPickerItems:  []string{"suite-a", "suite-b"},
				singleValue:     "suite-a",
			},
			{
				spec: FieldSpec{
					ID:          "components",
					Kind:        FieldKindSystemSelect,
					SystemsFunc: StaticSystems(System{Name: "sys", Components: []Component{{Name: "comp-x"}, {Name: "comp-y"}, {Name: "comp-z"}}}),
					MergeValuesFunc: func(v WizardValues) []string {
						if v.String("suite") == "suite-a" {
							return []string{"comp-x"}
						}
						return nil
					},
				},
				resolvedSystems: []System{{Name: "sys", Components: []Component{{Name: "comp-x"}, {Name: "comp-y"}, {Name: "comp-z"}}}},
				multiValues:     []string{"comp-z"}, // manually selected
				sysPickerItems: []pickerItem{
					{isSystem: true, system: "sys"},
					{isSystem: false, system: "sys", comp: "comp-x"},
					{isSystem: false, system: "sys", comp: "comp-y"},
					{isSystem: false, system: "sys", comp: "comp-z"},
				},
			},
		},
	}

	wiz.reEvalDynamicFields()

	got := wiz.states[1].multiValues
	// comp-z (manual) preserved, comp-x (auto) added.
	if len(got) != 2 || got[0] != "comp-z" || got[1] != "comp-x" {
		t.Fatalf("expected [comp-z comp-x], got %v", got)
	}

	// Switch to suite-b (no deps): comp-x removed, comp-z preserved.
	wiz.states[0].singleValue = "suite-b"
	wiz.reEvalDynamicFields()

	got = wiz.states[1].multiValues
	if len(got) != 1 || got[0] != "comp-z" {
		t.Fatalf("after clearing suite: expected [comp-z], got %v", got)
	}
}

// ── MergeValuesFunc on FieldKindSelect (applyAutoSelect) ─────────────────

func TestReEvalDynamicFields_MergeValuesFunc_AutoSelectsSelectField(t *testing.T) {
	wiz := &startWizard{
		states: []fieldState{
			{
				spec:            FieldSpec{ID: "suite", Kind: FieldKindSingleSelect, OptionsFunc: StaticOptions("suite-a", "suite-b")},
				resolvedOptions: []string{"suite-a", "suite-b"},
				strPickerItems:  []string{"suite-a", "suite-b"},
			},
			{
				spec: FieldSpec{
					ID:      "mode",
					Kind:    FieldKindSelect,
					Options: []string{"off", "all except SUT", "all"},
					OptionsFunc: StaticOptions("off", "all except SUT", "all"),
					Default: 0,
					MergeValuesFunc: func(v WizardValues) []string {
						if v.String("suite") == "suite-a" {
							return []string{"all except SUT"}
						}
						return nil
					},
				},
				resolvedOptions: []string{"off", "all except SUT", "all"},
				selectIdx:       0, // default "off"
			},
		},
	}

	// Select suite-a → mode should auto-select "all except SUT".
	wiz.states[0].singleValue = "suite-a"
	wiz.reEvalDynamicFields()

	if wiz.states[1].selectIdx != 1 {
		t.Fatalf("expected selectIdx=1 (all except SUT), got %d", wiz.states[1].selectIdx)
	}

	// Deselect suite → mode should revert to default "off".
	wiz.states[0].singleValue = ""
	wiz.reEvalDynamicFields()

	if wiz.states[1].selectIdx != 0 {
		t.Fatalf("expected selectIdx=0 (off) after clearing suite, got %d", wiz.states[1].selectIdx)
	}
}

func TestReEvalDynamicFields_MergeValuesFunc_AutoSelectsSingleSelectField(t *testing.T) {
	wiz := &startWizard{
		states: []fieldState{
			{
				spec:            FieldSpec{ID: "suite", Kind: FieldKindSingleSelect, OptionsFunc: StaticOptions("suite-a", "suite-b")},
				resolvedOptions: []string{"suite-a", "suite-b"},
				strPickerItems:  []string{"suite-a", "suite-b"},
			},
			{
				spec: FieldSpec{
					ID:   "sut",
					Kind: FieldKindSingleSelect,
					OptionsFunc: func(v WizardValues) []string {
						if v.String("suite") != "" {
							return []string{"system-x", "system-y"}
						}
						return nil
					},
					MergeValuesFunc: func(v WizardValues) []string {
						if v.String("suite") == "suite-a" {
							return []string{"system-x"}
						}
						return nil
					},
				},
				resolvedOptions: nil,
			},
		},
	}

	// Select suite-a → sut should auto-select "system-x".
	wiz.states[0].singleValue = "suite-a"
	wiz.reEvalDynamicFields()

	if wiz.states[1].singleValue != "system-x" {
		t.Fatalf("expected singleValue=system-x, got %q", wiz.states[1].singleValue)
	}

	// Deselect suite → sut should clear.
	wiz.states[0].singleValue = ""
	wiz.reEvalDynamicFields()

	if wiz.states[1].singleValue != "" {
		t.Fatalf("expected singleValue cleared, got %q", wiz.states[1].singleValue)
	}
}

func TestReEvalDynamicFields_HiddenField_SkippedInNavigation(t *testing.T) {
	wiz := &startWizard{
		states: []fieldState{
			{
				spec:            FieldSpec{ID: "mode", Kind: FieldKindSelect, OptionsFunc: StaticOptions("off", "on")},
				resolvedOptions: []string{"off", "on"},
				selectIdx:       0,
			},
			{
				spec: FieldSpec{
					ID:   "detail",
					Kind: FieldKindSingleSelect,
					OptionsFunc: func(v WizardValues) []string {
						if v.String("mode") == "on" {
							return []string{"a", "b"}
						}
						return nil
					},
				},
				resolvedOptions: nil,
			},
			{
				spec:            FieldSpec{ID: "other", Kind: FieldKindSelect, OptionsFunc: StaticOptions("x", "y")},
				resolvedOptions: []string{"x", "y"},
				selectIdx:       0,
			},
		},
	}

	// Field 1 (detail) is hidden because mode is "off".
	if !wiz.states[1].isHidden() {
		t.Fatal("expected detail field to be hidden when options are empty")
	}

	// Navigation from field 0 should skip field 1 and go to field 2.
	wiz.fieldIdx = 0
	next := wiz.nextVisibleField(0, 1)
	if next != 2 {
		t.Fatalf("expected next visible field from 0 to be 2, got %d", next)
	}

	// Navigation backward from field 2 should skip field 1.
	prev := wiz.nextVisibleField(2, -1)
	if prev != 0 {
		t.Fatalf("expected prev visible field from 2 to be 0, got %d", prev)
	}

	// Enable mode → detail becomes visible.
	wiz.states[0].selectIdx = 1 // "on"
	wiz.reEvalDynamicFields()

	if wiz.states[1].isHidden() {
		t.Fatal("expected detail field to be visible when mode is on")
	}

	next = wiz.nextVisibleField(0, 1)
	if next != 1 {
		t.Fatalf("expected next visible field from 0 to be 1 when visible, got %d", next)
	}
}

func TestReEvalDynamicFields_MergeValuesFunc_SelectPreservesManualOverride(t *testing.T) {
	wiz := &startWizard{
		states: []fieldState{
			{
				spec:            FieldSpec{ID: "suite", Kind: FieldKindSingleSelect, OptionsFunc: StaticOptions("suite-a", "suite-b")},
				resolvedOptions: []string{"suite-a", "suite-b"},
				strPickerItems:  []string{"suite-a", "suite-b"},
			},
			{
				spec: FieldSpec{
					ID:      "mode",
					Kind:    FieldKindSelect,
					OptionsFunc: StaticOptions("off", "all except SUT", "all"),
					Default: 0,
					MergeValuesFunc: func(v WizardValues) []string {
						if v.String("suite") == "suite-a" {
							return []string{"all except SUT"}
						}
						return nil
					},
				},
				resolvedOptions: []string{"off", "all except SUT", "all"},
				selectIdx:       0,
			},
		},
	}

	// Auto-select triggers.
	wiz.states[0].singleValue = "suite-a"
	wiz.reEvalDynamicFields()
	if wiz.states[1].selectIdx != 1 {
		t.Fatalf("expected auto-select to index 1, got %d", wiz.states[1].selectIdx)
	}

	// User manually overrides to "all" (index 2).
	wiz.states[1].selectIdx = 2
	wiz.reEvalDynamicFields()

	// Should preserve manual override.
	if wiz.states[1].selectIdx != 2 {
		t.Fatalf("expected manual override preserved at index 2, got %d", wiz.states[1].selectIdx)
	}

	// Deselect suite — user's manual "all" should be preserved since they overrode.
	wiz.states[0].singleValue = ""
	wiz.reEvalDynamicFields()

	if wiz.states[1].selectIdx != 2 {
		t.Fatalf("expected manual override still preserved at index 2, got %d", wiz.states[1].selectIdx)
	}
}

// ── topoSortSteps ────────────────────────────────────────────────────────

func TestTopoSortSteps_Empty(t *testing.T) {
	result := topoSortSteps(nil)
	if len(result) != 0 {
		t.Fatalf("expected empty result for nil input, got %d steps", len(result))
	}
}

func TestTopoSortSteps_NoDependencies(t *testing.T) {
	defs := []StepDef{
		{Step: &fakeStep{id: "a"}},
		{Step: &fakeStep{id: "b"}},
		{Step: &fakeStep{id: "c"}},
	}
	result := topoSortSteps(defs)
	if len(result) != 3 {
		t.Fatalf("expected 3 steps, got %d", len(result))
	}
	// With no dependencies, original order should be preserved
	if result[0].Step.ID() != "a" || result[1].Step.ID() != "b" || result[2].Step.ID() != "c" {
		t.Fatalf("expected original order [a, b, c], got [%s, %s, %s]",
			result[0].Step.ID(), result[1].Step.ID(), result[2].Step.ID())
	}
}

func TestTopoSortSteps_LinearChain(t *testing.T) {
	// c depends on b, b depends on a
	defs := []StepDef{
		{Step: &fakeStep{id: "c"}, meta: stepMetadata{waitFor: []string{"b"}}},
		{Step: &fakeStep{id: "b"}, meta: stepMetadata{waitFor: []string{"a"}}},
		{Step: &fakeStep{id: "a"}},
	}
	result := topoSortSteps(defs)
	if len(result) != 3 {
		t.Fatalf("expected 3 steps, got %d", len(result))
	}
	// Should be sorted as a, b, c
	if result[0].Step.ID() != "a" || result[1].Step.ID() != "b" || result[2].Step.ID() != "c" {
		t.Fatalf("expected [a, b, c], got [%s, %s, %s]",
			result[0].Step.ID(), result[1].Step.ID(), result[2].Step.ID())
	}
}

func TestTopoSortSteps_Diamond(t *testing.T) {
	// d depends on b and c, b and c depend on a
	defs := []StepDef{
		{Step: &fakeStep{id: "d"}, meta: stepMetadata{waitFor: []string{"b", "c"}}},
		{Step: &fakeStep{id: "c"}, meta: stepMetadata{waitFor: []string{"a"}}},
		{Step: &fakeStep{id: "b"}, meta: stepMetadata{waitFor: []string{"a"}}},
		{Step: &fakeStep{id: "a"}},
	}
	result := topoSortSteps(defs)
	if len(result) != 4 {
		t.Fatalf("expected 4 steps, got %d", len(result))
	}
	// a must come first
	if result[0].Step.ID() != "a" {
		t.Fatalf("expected 'a' first, got %s", result[0].Step.ID())
	}
	// d must come last
	if result[3].Step.ID() != "d" {
		t.Fatalf("expected 'd' last, got %s", result[3].Step.ID())
	}
	// b and c can be in either order (both depend only on a)
	ids := map[string]bool{result[1].Step.ID(): true, result[2].Step.ID(): true}
	if !ids["b"] || !ids["c"] {
		t.Fatalf("expected b and c in middle positions, got [%s, %s]",
			result[1].Step.ID(), result[2].Step.ID())
	}
}

func TestTopoSortSteps_MultipleDependencies(t *testing.T) {
	// kubectl depends on minikube, skaffold depends on minikube, mfe depends on skaffold
	defs := []StepDef{
		{Step: &fakeStep{id: "mfe"}, meta: stepMetadata{waitFor: []string{"skaffold"}}},
		{Step: &fakeStep{id: "skaffold"}, meta: stepMetadata{waitFor: []string{"minikube"}}},
		{Step: &fakeStep{id: "kubectl"}, meta: stepMetadata{waitFor: []string{"minikube"}}},
		{Step: &fakeStep{id: "minikube"}},
	}
	result := topoSortSteps(defs)
	if len(result) != 4 {
		t.Fatalf("expected 4 steps, got %d", len(result))
	}
	// Build position map for easier testing
	pos := make(map[string]int)
	for i, def := range result {
		pos[def.Step.ID()] = i
	}
	// Verify minikube comes before kubectl and skaffold
	if pos["minikube"] >= pos["kubectl"] {
		t.Fatalf("minikube should come before kubectl")
	}
	if pos["minikube"] >= pos["skaffold"] {
		t.Fatalf("minikube should come before skaffold")
	}
	// Verify skaffold comes before mfe
	if pos["skaffold"] >= pos["mfe"] {
		t.Fatalf("skaffold should come before mfe")
	}
}

func TestTopoSortSteps_NonExistentDependency(t *testing.T) {
	// b depends on "missing" which doesn't exist
	defs := []StepDef{
		{Step: &fakeStep{id: "b"}, meta: stepMetadata{waitFor: []string{"missing"}}},
		{Step: &fakeStep{id: "a"}},
	}
	result := topoSortSteps(defs)
	if len(result) != 2 {
		t.Fatalf("expected 2 steps, got %d", len(result))
	}
	// Non-existent dependencies should be ignored, so b has no real dependencies
	// Both should appear in result
	ids := make(map[string]bool)
	for _, def := range result {
		ids[def.Step.ID()] = true
	}
	if !ids["a"] || !ids["b"] {
		t.Fatalf("expected both a and b in result")
	}
}
