package main

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

// ── multiSelect ───────────────────────────────────────────────────────────────

func newTestMS(available []string) multiSelect {
	return newMultiSelect("test", available, 40)
}

func TestMultiSelect_UpdateFilter_EmptyQuery(t *testing.T) {
	ms := newTestMS([]string{"alpha", "beta", "gamma"})
	if len(ms.filtered) != 3 {
		t.Fatalf("expected all 3 items, got %d", len(ms.filtered))
	}
}

func TestMultiSelect_UpdateFilter_MatchesInfix(t *testing.T) {
	ms := newTestMS([]string{"auth-service", "user-service", "product-service"})
	ms.search.SetValue("user")
	ms.updateFilter()
	if len(ms.filtered) != 1 || ms.filtered[0] != "user-service" {
		t.Fatalf("expected [user-service], got %v", ms.filtered)
	}
}

func TestMultiSelect_UpdateFilter_CaseInsensitive(t *testing.T) {
	ms := newTestMS([]string{"AuthService", "userservice"})
	ms.search.SetValue("AUTH")
	ms.updateFilter()
	if len(ms.filtered) != 1 || ms.filtered[0] != "AuthService" {
		t.Fatalf("expected [AuthService], got %v", ms.filtered)
	}
}

func TestMultiSelect_UpdateFilter_NoMatch(t *testing.T) {
	ms := newTestMS([]string{"alpha", "beta"})
	ms.search.SetValue("zzz")
	ms.updateFilter()
	if len(ms.filtered) != 0 {
		t.Fatalf("expected no matches, got %v", ms.filtered)
	}
}

func TestMultiSelect_UpdateFilter_ClampsListIdx(t *testing.T) {
	ms := newTestMS([]string{"alpha", "beta", "gamma"})
	ms.listIdx = 2
	ms.search.SetValue("alpha")
	ms.updateFilter()
	// only 1 result; listIdx should clamp to 0
	if ms.listIdx != 0 {
		t.Fatalf("expected listIdx=0, got %d", ms.listIdx)
	}
}

func TestMultiSelect_IsSelected(t *testing.T) {
	ms := newTestMS([]string{"a", "b"})
	ms.selected = []string{"a"}
	if !ms.isSelected("a") {
		t.Fatal("expected a to be selected")
	}
	if ms.isSelected("b") {
		t.Fatal("b should not be selected")
	}
}

func TestMultiSelect_Toggle_Adds(t *testing.T) {
	ms := newTestMS([]string{"a", "b"})
	ms.toggle("a")
	if !ms.isSelected("a") {
		t.Fatal("expected a to be selected after toggle")
	}
}

func TestMultiSelect_Toggle_Removes(t *testing.T) {
	ms := newTestMS([]string{"a", "b"})
	ms.selected = []string{"a"}
	ms.toggle("a")
	if ms.isSelected("a") {
		t.Fatal("expected a to be removed after toggle")
	}
}

func TestMultiSelect_Toggle_PreservesOthers(t *testing.T) {
	ms := newTestMS([]string{"a", "b", "c"})
	ms.selected = []string{"a", "b", "c"}
	ms.toggle("b")
	if ms.isSelected("b") {
		t.Fatal("b should be removed")
	}
	if !ms.isSelected("a") || !ms.isSelected("c") {
		t.Fatal("a and c should remain selected")
	}
}

// ── syncFocus smoke test ──────────────────────────────────────────────────────

func TestSyncFocus_DoesNotPanic(t *testing.T) {
	ti := textinput.New()
	wiz := &startWizard{
		screen:       wizScreenCustom,
		custField:    custFieldBackends,
		nameInput:    ti,
		configInput:  ti,
		custName:     ti,
		custMFEInput: ti,
		backends:     newTestMS([]string{"x"}),
		bffs:         newTestMS([]string{"y"}),
	}
	// Should not panic for any field.
	for f := 0; f < custNumFields; f++ {
		wiz.custField = f
		wiz.syncFocus()
	}
	wiz.screen = wizScreenFile
	for f := 0; f < wizNumFields; f++ {
		wiz.field = f
		wiz.syncFocus()
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
	// A line that fits exactly should not gain an extra newline.
	line := strings.Repeat("x", 10)
	got := wrapContent([]string{line}, 10)
	if strings.Count(got, "\n") != 0 {
		t.Fatalf("expected 0 newlines, got: %q", got)
	}
}
