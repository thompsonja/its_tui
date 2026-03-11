package tui

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// ── fakeStep ──────────────────────────────────────────────────────────────────

// fakeStep is a test double for the Step interface.
type fakeStep struct {
	id      string
	logPath string
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
	m := &model{cfg: Config{Steps: []StepTemplate{{
		Label: "opt",
		Build: func(v WizardValues) (Step, error) { return nil, nil },
	}}}}
	defs, err := m.buildDefsFromTemplates(WizardValues{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(defs) != 0 {
		t.Fatalf("expected 0 defs for nil step, got %d", len(defs))
	}
}

func TestBuildDefsFromTemplates_PropagatesError(t *testing.T) {
	m := &model{cfg: Config{Steps: []StepTemplate{{
		Label: "bad",
		Build: fakeBuild("", errors.New("boom")),
	}}}}
	_, err := m.buildDefsFromTemplates(WizardValues{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestBuildDefsFromTemplates_ErrorIncludesLabel(t *testing.T) {
	m := &model{cfg: Config{Steps: []StepTemplate{{
		Label: "my-step",
		Build: fakeBuild("", errors.New("oops")),
	}}}}
	_, err := m.buildDefsFromTemplates(WizardValues{})
	if err == nil || err.Error() == "" {
		t.Fatal("expected non-empty error")
	}
	// Error should mention the template label.
	if !strings.Contains(err.Error(), "my-step") {
		t.Fatalf("error should mention template label, got: %v", err)
	}
}

func TestBuildDefsFromTemplates_UsesLabelFunc(t *testing.T) {
	m := &model{cfg: Config{Steps: []StepTemplate{{
		Label:     "base",
		LabelFunc: func(v WizardValues) string { return "dynamic-" + v.String("x") },
		Build:     fakeBuild("s", nil),
	}}}}
	vals := NewWizardValues(map[string]string{"x": "42"}, nil)
	defs, err := m.buildDefsFromTemplates(vals)
	if err != nil {
		t.Fatal(err)
	}
	if defs[0].Label != "dynamic-42" {
		t.Fatalf("expected dynamic-42, got %q", defs[0].Label)
	}
}

func TestBuildDefsFromTemplates_FallsBackToLabel(t *testing.T) {
	m := &model{cfg: Config{Steps: []StepTemplate{{
		Label: "static",
		Build: fakeBuild("s", nil),
	}}}}
	defs, _ := m.buildDefsFromTemplates(WizardValues{})
	if defs[0].Label != "static" {
		t.Fatalf("expected static, got %q", defs[0].Label)
	}
}

func TestBuildDefsFromTemplates_WiresPanel(t *testing.T) {
	m := &model{cfg: Config{Steps: []StepTemplate{{
		Label: "x",
		Panel: PanelTopRight,
		Build: fakeBuild("x", nil),
	}}}}
	defs, _ := m.buildDefsFromTemplates(WizardValues{})
	if defs[0].Panel != PanelTopRight {
		t.Fatalf("expected PanelTopRight, got %d", defs[0].Panel)
	}
}

func TestBuildDefsFromTemplates_WiresWaitFor(t *testing.T) {
	m := &model{cfg: Config{Steps: []StepTemplate{{
		Label:   "x",
		WaitFor: "dep",
		Build:   fakeBuild("x", nil),
	}}}}
	defs, _ := m.buildDefsFromTemplates(WizardValues{})
	if defs[0].WaitFor != "dep" {
		t.Fatalf("expected dep, got %q", defs[0].WaitFor)
	}
}

func TestBuildDefsFromTemplates_WiresOnReady(t *testing.T) {
	var got string
	m := &model{
		cfg: Config{Steps: []StepTemplate{{
			Label:   "x",
			OnReady: func(sp string) { got = sp },
			Build:   fakeBuild("x", nil),
		}}},
		statePath: "/test/state.json",
	}
	defs, _ := m.buildDefsFromTemplates(WizardValues{})
	defs[0].OnReady()
	if got != "/test/state.json" {
		t.Fatalf("expected statePath, got %q", got)
	}
}

func TestBuildDefsFromTemplates_PassesValuesToBuild(t *testing.T) {
	var received WizardValues
	m := &model{cfg: Config{Steps: []StepTemplate{{
		Label: "x",
		Build: func(v WizardValues) (Step, error) {
			received = v
			return &fakeStep{id: "x"}, nil
		},
	}}}}
	vals := NewWizardValues(map[string]string{"cpu": "8"}, nil)
	m.buildDefsFromTemplates(vals)
	if received.String("cpu") != "8" {
		t.Fatalf("expected cpu=8, got %q", received.String("cpu"))
	}
}

func TestBuildDefsFromTemplates_MultipleTemplates(t *testing.T) {
	m := &model{cfg: Config{Steps: []StepTemplate{
		{Label: "a", Build: fakeBuild("a", nil)},
		{Label: "b", Build: func(v WizardValues) (Step, error) { return nil, nil }}, // skipped
		{Label: "c", Build: fakeBuild("c", nil)},
	}}}
	defs, _ := m.buildDefsFromTemplates(WizardValues{})
	if len(defs) != 2 {
		t.Fatalf("expected 2 defs (b skipped), got %d", len(defs))
	}
}

func TestBuildDefsFromTemplates_NilOnReadyNotWired(t *testing.T) {
	m := &model{cfg: Config{Steps: []StepTemplate{{
		Label:   "x",
		OnReady: nil,
		Build:   fakeBuild("x", nil),
	}}}}
	defs, _ := m.buildDefsFromTemplates(WizardValues{})
	if defs[0].OnReady != nil {
		t.Fatal("OnReady should be nil when template.OnReady is nil")
	}
}

// ── buildPipelineFromState ────────────────────────────────────────────────────

func TestBuildPipelineFromState_PassesStringValues(t *testing.T) {
	var gotMode string
	m := &model{cfg: Config{Steps: []StepTemplate{{
		Label: "x",
		Build: func(v WizardValues) (Step, error) {
			gotMode = v.String("mode")
			return &fakeStep{id: "x"}, nil
		},
	}}}}
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
	m := &model{cfg: Config{Steps: []StepTemplate{{
		Label: "x",
		Build: func(v WizardValues) (Step, error) {
			gotComps = v.Strings("components")
			return &fakeStep{id: "x"}, nil
		},
	}}}}
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
	m := &model{cfg: Config{Steps: []StepTemplate{{
		Label: "x",
		Build: func(v WizardValues) (Step, error) {
			// Should not panic on nil maps.
			_ = v.String("anything")
			_ = v.Strings("anything")
			return &fakeStep{id: "x"}, nil
		},
	}}}}
	// InstanceState with nil maps.
	inst := &InstanceState{}
	defs := m.buildPipelineFromState("test", inst)
	if len(defs) != 1 {
		t.Fatalf("expected 1 def, got %d", len(defs))
	}
}

func TestBuildPipelineFromState_IgnoresBuildError(t *testing.T) {
	// Errors during session restore are silently dropped so we don't crash.
	m := &model{cfg: Config{Steps: []StepTemplate{{
		Label: "x",
		Build: fakeBuild("", errors.New("generate failed")),
	}}}}
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
			spec:      FieldSpec{ID: "cpu", Kind: FieldKindSelect, Options: []string{"2", "4", "8"}},
			selectIdx: 1,
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
			spec:      FieldSpec{ID: "cpu", Kind: FieldKindSelect, Options: []string{"2", "4"}},
			selectIdx: 99, // out of range
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
			{spec: FieldSpec{ID: "cpu", Kind: FieldKindSelect, Options: []string{"2", "4"}}, selectIdx: 0},
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
		Panel: PanelID(-1),
		Build: fakeBuild("x", nil),
	}})
	if err == nil {
		t.Fatal("expected error for Panel=-1")
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
		{ID: "b", Label: "b", WaitFor: "nonexistent", Build: fakeBuild("b", nil)},
	})
	if err == nil {
		t.Fatal("expected error for unknown WaitFor")
	}
}

func TestValidateTemplates_ValidWaitFor(t *testing.T) {
	err := validateTemplates([]StepTemplate{
		{ID: "a", Label: "a", Build: fakeBuild("a", nil)},
		{ID: "b", Label: "b", WaitFor: "a", Build: fakeBuild("b", nil)},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateTemplates_WaitForSkippedWithoutIDs(t *testing.T) {
	// When no templates have IDs, WaitFor validation is skipped.
	err := validateTemplates([]StepTemplate{
		{Label: "a", WaitFor: "unknown", Build: fakeBuild("a", nil)},
	})
	if err != nil {
		t.Fatalf("should not error when IDs are not set: %v", err)
	}
}

func TestValidateTemplates_AllValidTemplates(t *testing.T) {
	err := validateTemplates([]StepTemplate{
		MinikubeTemplate(),
		KubectlTemplate(),
		SkaffoldTemplate(nil, nil),
		MFETemplate(nil, nil),
	})
	if err != nil {
		t.Fatalf("provided templates should be valid: %v", err)
	}
}

// ── Wizard pre-population ─────────────────────────────────────────────────────

func TestNewStartWizard_PrePopulatesSelectField(t *testing.T) {
	m := &model{cfg: Config{Steps: []StepTemplate{MinikubeTemplate()}}}
	// CPU options: "2"(0), "4"(1), "8"(2), "16"(3) — select "8"
	initial := NewWizardValues(map[string]string{"cpu": "8"}, nil)
	wiz := newStartWizard(m, initial)
	cpuState := wiz.states[0] // first field is cpu
	if cpuState.selectIdx != 2 {
		t.Fatalf("expected cpuIdx=2 (\"8\"), got %d", cpuState.selectIdx)
	}
}

func TestNewStartWizard_PrePopulatesSelectUnknownValue(t *testing.T) {
	m := &model{cfg: Config{Steps: []StepTemplate{MinikubeTemplate()}}}
	// Unknown value falls back to Default (index 1 for CPU).
	initial := NewWizardValues(map[string]string{"cpu": "999"}, nil)
	wiz := newStartWizard(m, initial)
	if wiz.states[0].selectIdx != 1 {
		t.Fatalf("expected default index 1, got %d", wiz.states[0].selectIdx)
	}
}

func TestNewStartWizard_PrePopulatesSingleSelect(t *testing.T) {
	m := &model{cfg: Config{Steps: []StepTemplate{
		MFETemplate([]string{"checkout-mfe", "user-mfe"}, nil),
	}}}
	initial := NewWizardValues(map[string]string{"mfe": "user-mfe"}, nil)
	wiz := newStartWizard(m, initial)
	if wiz.states[0].singleValue != "user-mfe" {
		t.Fatalf("expected user-mfe, got %q", wiz.states[0].singleValue)
	}
}

func TestNewStartWizard_PrePopulatesSystemSelect(t *testing.T) {
	systems := []System{{
		Name: "checkout",
		Components: []Component{{Name: "checkout-backend"}, {Name: "checkout-bff"}},
	}}
	m := &model{cfg: Config{Steps: []StepTemplate{
		SkaffoldTemplate(nil, systems),
	}}}
	initial := NewWizardValues(
		map[string]string{"mode": "debug"},
		map[string][]string{"components": {"checkout-backend"}},
	)
	wiz := newStartWizard(m, initial)

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
	m := &model{cfg: Config{Steps: []StepTemplate{MinikubeTemplate()}}}
	wiz := newStartWizard(m, WizardValues{})
	// CPU default is index 1 ("4"), RAM default is index 1 ("4g").
	if wiz.states[0].selectIdx != 1 {
		t.Fatalf("expected default cpuIdx=1, got %d", wiz.states[0].selectIdx)
	}
	if wiz.states[1].selectIdx != 1 {
		t.Fatalf("expected default ramIdx=1, got %d", wiz.states[1].selectIdx)
	}
}

// ── Dynamic field options (OptionsFunc / SystemsFunc) ─────────────────────────

func TestNewStartWizard_SystemsFunc_OverridesStaticSystems(t *testing.T) {
	dynamic := []System{{
		Name:       "dynamic-sys",
		Components: []Component{{Name: "dyn-comp-a"}, {Name: "dyn-comp-b"}},
	}}
	m := &model{cfg: Config{Steps: []StepTemplate{{
		Label: "x",
		Build: fakeBuild("x", nil),
		Fields: []FieldSpec{{
			ID:          "components",
			Label:       "Components",
			Kind:        FieldKindSystemSelect,
			Systems:     []System{{Name: "static-sys", Components: []Component{{Name: "static-comp"}}}},
			SystemsFunc: func() []System { return dynamic },
		}},
	}}}}
	wiz := newStartWizard(m, WizardValues{})
	items := wiz.states[0].sysPickerItems
	if len(items) != 3 { // 1 header + 2 components
		t.Fatalf("expected 3 picker items, got %d: %v", len(items), items)
	}
	if items[0].system != "dynamic-sys" {
		t.Fatalf("expected dynamic-sys header, got %q", items[0].system)
	}
}

func TestNewStartWizard_SystemsFunc_NilFallsBackToStatic(t *testing.T) {
	static := []System{{
		Name:       "static-sys",
		Components: []Component{{Name: "static-comp"}},
	}}
	m := &model{cfg: Config{Steps: []StepTemplate{{
		Label: "x",
		Build: fakeBuild("x", nil),
		Fields: []FieldSpec{{
			ID:      "components",
			Label:   "Components",
			Kind:    FieldKindSystemSelect,
			Systems: static,
			// SystemsFunc is nil
		}},
	}}}}
	wiz := newStartWizard(m, WizardValues{})
	items := wiz.states[0].sysPickerItems
	if len(items) != 2 { // 1 header + 1 component
		t.Fatalf("expected 2 picker items, got %d: %v", len(items), items)
	}
	if items[0].system != "static-sys" {
		t.Fatalf("expected static-sys header, got %q", items[0].system)
	}
}

func TestNewStartWizard_OptionsFunc_OverridesStaticOptions(t *testing.T) {
	m := &model{cfg: Config{Steps: []StepTemplate{{
		Label: "x",
		Build: fakeBuild("x", nil),
		Fields: []FieldSpec{{
			ID:          "ns",
			Label:       "Namespace",
			Kind:        FieldKindSingleSelect,
			Options:     []string{"old-a", "old-b"},
			OptionsFunc: func() []string { return []string{"new-x", "new-y", "new-z"} },
		}},
	}}}}
	wiz := newStartWizard(m, WizardValues{})
	items := wiz.states[0].strPickerItems
	if len(items) != 3 {
		t.Fatalf("expected 3 items, got %d: %v", len(items), items)
	}
	if items[0] != "new-x" {
		t.Fatalf("expected new-x, got %q", items[0])
	}
}

func TestNewStartWizard_OptionsFunc_NilFallsBackToStatic(t *testing.T) {
	m := &model{cfg: Config{Steps: []StepTemplate{{
		Label: "x",
		Build: fakeBuild("x", nil),
		Fields: []FieldSpec{{
			ID:      "ns",
			Label:   "Namespace",
			Kind:    FieldKindMultiSelect,
			Options: []string{"alpha", "beta"},
			// OptionsFunc is nil
		}},
	}}}}
	wiz := newStartWizard(m, WizardValues{})
	items := wiz.states[0].strPickerItems
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d: %v", len(items), items)
	}
	if items[0] != "alpha" {
		t.Fatalf("expected alpha, got %q", items[0])
	}
}

// ── validateTemplates: OptionsFunc / SystemsFunc conflicts ────────────────────

func TestValidateTemplates_BothOptionsAndOptionsFunc_Error(t *testing.T) {
	err := validateTemplates([]StepTemplate{{
		ID:    "x",
		Label: "x",
		Build: fakeBuild("x", nil),
		Fields: []FieldSpec{{
			ID:          "ns",
			Kind:        FieldKindSingleSelect,
			Options:     []string{"a", "b"},
			OptionsFunc: func() []string { return []string{"c"} },
		}},
	}})
	if err == nil {
		t.Fatal("expected error when both Options and OptionsFunc are set")
	}
	if !strings.Contains(err.Error(), "ns") {
		t.Fatalf("error should mention field ID %q, got: %v", "ns", err)
	}
}

func TestValidateTemplates_BothSystemsAndSystemsFunc_Error(t *testing.T) {
	err := validateTemplates([]StepTemplate{{
		ID:    "x",
		Label: "x",
		Build: fakeBuild("x", nil),
		Fields: []FieldSpec{{
			ID:          "components",
			Kind:        FieldKindSystemSelect,
			Systems:     []System{{Name: "s", Components: []Component{{Name: "c"}}}},
			SystemsFunc: func() []System { return nil },
		}},
	}})
	if err == nil {
		t.Fatal("expected error when both Systems and SystemsFunc are set")
	}
	if !strings.Contains(err.Error(), "components") {
		t.Fatalf("error should mention field ID %q, got: %v", "components", err)
	}
}

func TestValidateTemplates_OnlyOptionsFunc_Valid(t *testing.T) {
	err := validateTemplates([]StepTemplate{{
		ID:    "x",
		Label: "x",
		Build: fakeBuild("x", nil),
		Fields: []FieldSpec{{
			ID:          "ns",
			Kind:        FieldKindSingleSelect,
			OptionsFunc: func() []string { return []string{"a"} },
		}},
	}})
	if err != nil {
		t.Fatalf("OptionsFunc alone should be valid: %v", err)
	}
}

func TestValidateTemplates_OnlySystemsFunc_Valid(t *testing.T) {
	err := validateTemplates([]StepTemplate{{
		ID:    "x",
		Label: "x",
		Build: fakeBuild("x", nil),
		Fields: []FieldSpec{{
			ID:          "components",
			Kind:        FieldKindSystemSelect,
			SystemsFunc: func() []System { return nil },
		}},
	}})
	if err != nil {
		t.Fatalf("SystemsFunc alone should be valid: %v", err)
	}
}

// ── StopFunc ──────────────────────────────────────────────────────────────────

func TestMinikubeTemplate_HasStopFunc(t *testing.T) {
	if MinikubeTemplate().StopFunc == nil {
		t.Fatal("MinikubeTemplate should provide a StopFunc")
	}
}

func TestMinikubeTemplate_HasStopLabel(t *testing.T) {
	if MinikubeTemplate().StopLabel == "" {
		t.Fatal("MinikubeTemplate should provide a non-empty StopLabel")
	}
}

func TestKubectlTemplate_NoStopFunc(t *testing.T) {
	if KubectlTemplate().StopFunc != nil {
		t.Fatal("KubectlTemplate should not have a StopFunc")
	}
}

func TestSkaffoldTemplate_NoStopFunc(t *testing.T) {
	if SkaffoldTemplate(nil, nil).StopFunc != nil {
		t.Fatal("SkaffoldTemplate should not have a StopFunc (cancelled via context)")
	}
}

func TestMFETemplate_NoStopFunc(t *testing.T) {
	if MFETemplate(nil, nil).StopFunc != nil {
		t.Fatal("MFETemplate should not have a StopFunc (killed via PGID)")
	}
}

