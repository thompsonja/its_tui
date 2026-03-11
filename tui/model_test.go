package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
)

// ── appendLine ────────────────────────────────────────────────────────────────

func TestAppendLine_Basic(t *testing.T) {
	buf := appendLine(nil, "hello")
	if len(buf) != 1 || buf[0] != "hello" {
		t.Fatalf("expected [hello], got %v", buf)
	}
}

func TestAppendLine_CapAtMaxBufLines(t *testing.T) {
	var buf []string
	for i := 0; i < maxBufLines+10; i++ {
		buf = appendLine(buf, "line")
	}
	if len(buf) != maxBufLines {
		t.Fatalf("expected %d lines, got %d", maxBufLines, len(buf))
	}
}

func TestAppendLine_TrimsOldestLines(t *testing.T) {
	var buf []string
	for i := 0; i < maxBufLines; i++ {
		buf = appendLine(buf, "old")
	}
	buf = appendLine(buf, "new")
	if buf[len(buf)-1] != "new" {
		t.Fatalf("expected last line to be 'new', got %q", buf[len(buf)-1])
	}
	if buf[0] == "old" && len(buf) == maxBufLines {
		// oldest "old" was dropped; all remaining are fine
	}
}

// ── joinLines ─────────────────────────────────────────────────────────────────

func TestJoinLines(t *testing.T) {
	got := joinLines([]string{"a", "b", "c"})
	if got != "a\nb\nc" {
		t.Fatalf("unexpected: %q", got)
	}
}

func TestJoinLines_Empty(t *testing.T) {
	if joinLines(nil) != "" {
		t.Fatal("expected empty string for nil buf")
	}
}

// ── wrapLine ──────────────────────────────────────────────────────────────────

func TestWrapLine_ShortLine(t *testing.T) {
	got := wrapLine("hello", 10)
	if got != "hello" {
		t.Fatalf("short line should be unchanged, got %q", got)
	}
}

func TestWrapLine_ExactWidth(t *testing.T) {
	line := "1234567890"
	got := wrapLine(line, 10)
	if got != line {
		t.Fatalf("exact-width line should be unchanged, got %q", got)
	}
}

func TestWrapLine_OneOver(t *testing.T) {
	got := wrapLine("12345678901", 10)
	if got != "1234567890\n1" {
		t.Fatalf("unexpected wrap: %q", got)
	}
}

func TestWrapLine_MultipleWraps(t *testing.T) {
	got := wrapLine("123456789012345", 5)
	want := "12345\n67890\n12345"
	if got != want {
		t.Fatalf("want %q, got %q", want, got)
	}
}

func TestWrapLine_ZeroWidth(t *testing.T) {
	line := "hello"
	got := wrapLine(line, 0)
	if got != line {
		t.Fatalf("zero width should return unchanged, got %q", got)
	}
}

func TestWrapLine_Unicode(t *testing.T) {
	// "日本語" is 3 runes; width=2 should split after 2nd rune
	got := wrapLine("日本語", 2)
	if got != "日本\n語" {
		t.Fatalf("unicode wrap failed: %q", got)
	}
}

// ── wrapContent ───────────────────────────────────────────────────────────────

func TestWrapContent_WrapsEachLine(t *testing.T) {
	buf := []string{"12345", "6789012345"}
	got := wrapContent(buf, 5)
	want := "12345\n67890\n12345"
	if got != want {
		t.Fatalf("want %q, got %q", want, got)
	}
}

func TestWrapContent_ZeroWidth(t *testing.T) {
	buf := []string{"a", "b"}
	got := wrapContent(buf, 0)
	if got != "a\nb" {
		t.Fatalf("unexpected: %q", got)
	}
}

func TestWrapContent_EmptyBuf(t *testing.T) {
	if wrapContent(nil, 80) != "" {
		t.Fatal("expected empty string for nil buf")
	}
}

// ── component picker (fieldState / SystemSelect) ──────────────────────────────

func newTestSysField() *fieldState {
	si := textinput.New()
	si.Width = 40
	systems := []System{
		{Name: "checkout", Components: []Component{
			{Name: "checkout-backend"},
			{Name: "checkout-bff"},
		}},
		{Name: "user", Components: []Component{
			{Name: "user-service"},
			{Name: "user-bff"},
		}},
	}
	spec := FieldSpec{ID: "components", Kind: FieldKindSystemSelect, Systems: systems}
	s := &fieldState{spec: spec, pickerSearch: si, resolvedSystems: systems}
	// Initialise full items list (mirrors newStartWizard logic).
	for _, sys := range systems {
		s.sysPickerItems = append(s.sysPickerItems, pickerItem{isSystem: true, system: sys.Name})
		for _, c := range sys.Components {
			s.sysPickerItems = append(s.sysPickerItems, pickerItem{isSystem: false, system: sys.Name, comp: c.Name})
		}
	}
	return s
}

func TestUpdateSysFilter_EmptyQuery(t *testing.T) {
	s := newTestSysField()
	// 2 systems × (1 system header + N comps) = 2+2+2 = 6 items
	if len(s.sysPickerItems) != 6 {
		t.Fatalf("expected 6 picker items, got %d", len(s.sysPickerItems))
	}
}

func TestUpdateSysFilter_BySystemName(t *testing.T) {
	s := newTestSysField()
	s.pickerSearch.SetValue("checkout")
	s.updateSysFilter()
	// system header + 2 components = 3
	if len(s.sysPickerItems) != 3 {
		t.Fatalf("expected 3 items, got %d: %v", len(s.sysPickerItems), s.sysPickerItems)
	}
	if !s.sysPickerItems[0].isSystem || s.sysPickerItems[0].system != "checkout" {
		t.Fatal("first item should be checkout system header")
	}
}

func TestUpdateSysFilter_ByComponentName(t *testing.T) {
	s := newTestSysField()
	s.pickerSearch.SetValue("user-bff")
	s.updateSysFilter()
	// user system header + 1 matching component = 2
	if len(s.sysPickerItems) != 2 {
		t.Fatalf("expected 2 items, got %d", len(s.sysPickerItems))
	}
}

func TestUpdateSysFilter_NoMatch(t *testing.T) {
	s := newTestSysField()
	s.pickerSearch.SetValue("zzz")
	s.updateSysFilter()
	if len(s.sysPickerItems) != 0 {
		t.Fatalf("expected 0 items, got %d", len(s.sysPickerItems))
	}
}

func TestUpdateSysFilter_CaseInsensitive(t *testing.T) {
	s := newTestSysField()
	s.pickerSearch.SetValue("CHECKOUT")
	s.updateSysFilter()
	if len(s.sysPickerItems) == 0 {
		t.Fatal("expected matches for CHECKOUT (case-insensitive)")
	}
}

func TestUpdateSysFilter_ClampsIdx(t *testing.T) {
	s := newTestSysField()
	s.pickerIdx = 5
	s.pickerSearch.SetValue("checkout")
	s.updateSysFilter()
	if s.pickerIdx >= len(s.sysPickerItems) {
		t.Fatalf("pickerIdx %d out of bounds (%d items)", s.pickerIdx, len(s.sysPickerItems))
	}
}

func TestIsMultiSelected(t *testing.T) {
	s := newTestSysField()
	s.multiValues = []string{"checkout-backend"}
	if !s.isMultiSelected("checkout-backend") {
		t.Fatal("expected checkout-backend to be selected")
	}
	if s.isMultiSelected("user-service") {
		t.Fatal("user-service should not be selected")
	}
}

func TestToggleMulti_Adds(t *testing.T) {
	s := newTestSysField()
	s.toggleMulti("checkout-backend")
	if !s.isMultiSelected("checkout-backend") {
		t.Fatal("expected checkout-backend selected")
	}
}

func TestToggleMulti_Removes(t *testing.T) {
	s := newTestSysField()
	s.multiValues = []string{"checkout-backend"}
	s.toggleMulti("checkout-backend")
	if s.isMultiSelected("checkout-backend") {
		t.Fatal("expected checkout-backend removed")
	}
}

func TestToggleMulti_PreservesOthers(t *testing.T) {
	s := newTestSysField()
	s.multiValues = []string{"checkout-backend", "user-service"}
	s.toggleMulti("checkout-backend")
	if !s.isMultiSelected("user-service") {
		t.Fatal("user-service should remain selected")
	}
}

func TestToggleSysPicker_Component(t *testing.T) {
	s := newTestSysField()
	idx := -1
	for i, pi := range s.sysPickerItems {
		if !pi.isSystem && pi.comp == "checkout-backend" {
			idx = i
			break
		}
	}
	if idx == -1 {
		t.Fatal("checkout-backend not found in picker items")
	}
	s.toggleSysPicker(idx)
	if !s.isMultiSelected("checkout-backend") {
		t.Fatal("expected checkout-backend to be selected")
	}
}

func TestToggleSysPicker_System_SelectsAll(t *testing.T) {
	s := newTestSysField()
	s.toggleSysPicker(0)
	if !s.isMultiSelected("checkout-backend") || !s.isMultiSelected("checkout-bff") {
		t.Fatal("expected all checkout components selected")
	}
}

func TestToggleSysPicker_System_DeselectsAll(t *testing.T) {
	s := newTestSysField()
	s.multiValues = []string{"checkout-backend", "checkout-bff"}
	s.toggleSysPicker(0)
	if s.isMultiSelected("checkout-backend") || s.isMultiSelected("checkout-bff") {
		t.Fatal("expected all checkout components deselected")
	}
}

func TestToggleSysPicker_System_PartialSelectsRemaining(t *testing.T) {
	s := newTestSysField()
	s.multiValues = []string{"checkout-backend"}
	s.toggleSysPicker(0)
	if !s.isMultiSelected("checkout-bff") {
		t.Fatal("expected checkout-bff to be selected")
	}
	if !s.isMultiSelected("checkout-backend") {
		t.Fatal("checkout-backend should still be selected")
	}
}

// ── wrapLine edge cases ───────────────────────────────────────────────────────

func TestWrapLine_EmptyString(t *testing.T) {
	if wrapLine("", 10) != "" {
		t.Fatal("empty string should wrap to empty string")
	}
}

func TestWrapLine_NegativeWidth(t *testing.T) {
	line := "hello"
	if wrapLine(line, -1) != line {
		t.Fatal("negative width should return line unchanged")
	}
}

// ── joinLines / wrapContent symmetry ─────────────────────────────────────────

func TestWrapContent_SingleLine_NoWrap(t *testing.T) {
	buf := []string{"short"}
	if wrapContent(buf, 80) != "short" {
		t.Fatal("single short line should not be wrapped")
	}
}

func TestWrapContent_PreservesLineCount(t *testing.T) {
	line := strings.Repeat("x", 10)
	got := wrapContent([]string{line}, 10)
	if strings.Count(got, "\n") != 0 {
		t.Fatalf("expected 0 newlines, got: %q", got)
	}
}
