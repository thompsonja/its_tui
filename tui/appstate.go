package tui

import (
	"context"
	"strings"

	"github.com/thompsonja/its_tui/step"
)

const maxBufLines = 5000

// panelView holds the steps and log buffers for one content panel.
// activeIdx is persisted to disk and restored on session resume.
type panelView struct {
	defs      []StepDef
	bufs      [][]string
	activeIdx int
}

func (pv *panelView) activeBuf() []string {
	if len(pv.bufs) == 0 || pv.activeIdx < 0 || pv.activeIdx >= len(pv.bufs) {
		return nil
	}
	return pv.bufs[pv.activeIdx]
}

// stepEntry holds the context and cancel function for a single step goroutine.
type stepEntry struct {
	ctx    context.Context
	cancel context.CancelFunc
}

// appState holds domain state: data that is independent of how it is displayed.
type appState struct {
	cfg          Config
	instanceName string
	statePath    string
	workspaceDir string

	panels      [3]panelView
	commandsBuf []string

	cmdHistory   []string
	historyIdx   int
	historyDraft string

	fwdPorts   []step.DebugPortMsg
	debugPorts []step.DebugPortMsg

	testBuf     []string
	testRunning bool

	activeDefs []StepDef
	stepCtxs   map[string]stepEntry
	customCmds map[string]CommandSpec

	totalSteps     int
	completedSteps int
}

func (a *appState) configuredName() string {
	if a.cfg.InstanceName != "" {
		return a.cfg.InstanceName
	}
	return defaultInstanceName
}

func (a *appState) registerPipeline(defs []StepDef) {
	var pv [3]panelView
	for _, def := range defs {
		if def.meta.panel == PanelNone {
			continue
		}
		pid := int(def.meta.panel)
		pv[pid].defs = append(pv[pid].defs, def)
		pv[pid].bufs = append(pv[pid].bufs, nil)
	}
	a.panels = pv
}

func (a *appState) panelAndIdx(id string) (PanelID, int, bool) {
	for pid, pv := range a.panels {
		for i, def := range pv.defs {
			if def.Step.ID() == id {
				return PanelID(pid), i, true
			}
		}
	}
	return 0, -1, false
}

func (a *appState) findDef(id string) (StepDef, bool) {
	for _, pv := range a.panels {
		for _, def := range pv.defs {
			if def.Step.ID() == id {
				return def, true
			}
		}
	}
	return StepDef{}, false
}

func (a *appState) isPortsTabActive(pid PanelID) bool {
	if pid != PanelTopRight || len(a.fwdPorts) == 0 {
		return false
	}
	return a.panels[pid].activeIdx == len(a.panels[pid].defs)
}

func (a *appState) isDebugTabActive(pid PanelID) bool {
	if pid != PanelTopRight || len(a.debugPorts) == 0 {
		return false
	}
	portsOffset := 0
	if len(a.fwdPorts) > 0 {
		portsOffset = 1
	}
	return a.panels[pid].activeIdx == len(a.panels[pid].defs)+portsOffset
}

func (a *appState) isTestsTabActive(pid PanelID) bool {
	if pid != PanelBottomRight || len(a.cfg.Tests) == 0 {
		return false
	}
	return a.panels[pid].activeIdx == len(a.panels[pid].defs)
}

// focusedPanelID maps a panel focus constant to its PanelID.
func focusedPanelID(focused int) (PanelID, bool) {
	switch focused {
	case panelMinikube:
		return PanelTopLeft, true
	case panelSkaffold:
		return PanelTopRight, true
	case panelMFE:
		return PanelBottomRight, true
	}
	return 0, false
}

// ── Pure buffer helpers ───────────────────────────────────────────────────────

func appendLine(buf []string, line string) []string {
	buf = append(buf, line)
	if len(buf) > maxBufLines {
		buf = buf[len(buf)-maxBufLines:]
	}
	return buf
}

func joinLines(buf []string) string {
	return strings.Join(buf, "\n")
}

func wrapLine(line string, width int) string {
	runes := []rune(line)
	if width <= 0 || len(runes) <= width {
		return line
	}
	var sb strings.Builder
	for len(runes) > width {
		sb.WriteString(string(runes[:width]))
		sb.WriteByte('\n')
		runes = runes[width:]
	}
	if len(runes) > 0 {
		sb.WriteString(string(runes))
	}
	return sb.String()
}

func wrapContent(buf []string, width int) string {
	if width <= 0 {
		return joinLines(buf)
	}
	result := make([]string, 0, len(buf))
	for _, line := range buf {
		result = append(result, wrapLine(line, width))
	}
	return strings.Join(result, "\n")
}
