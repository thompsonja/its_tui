package tui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/thompsonja/its_tui/config"
	"github.com/thompsonja/its_tui/step"
)

// copyToClipboard writes text to the system clipboard by piping to the first
// available clipboard tool: wl-copy (Wayland), xclip, xsel (X11), pbcopy (macOS).
func copyToClipboard(text string) error {
	tools := [][]string{
		{"wl-copy"},
		{"xclip", "-selection", "clipboard"},
		{"xsel", "--clipboard", "--input"},
		{"pbcopy"},
	}
	for _, args := range tools {
		path, err := exec.LookPath(args[0])
		if err != nil {
			continue
		}
		cmd := exec.Command(path, args[1:]...)
		cmd.Stdin = strings.NewReader(text)
		if err := cmd.Run(); err == nil {
			return nil
		}
	}
	return fmt.Errorf("no clipboard tool found (install xclip, xsel, or wl-clipboard)")
}

// debugRuntime returns a human-readable runtime name for a skaffold portName.
func debugRuntime(portName string) string {
	switch portName {
	case "dlv":
		return "dlv/Go"
	case "jvm":
		return "jvm/Java"
	case "ptvsd", "debugpy":
		return "debugpy/Python"
	case "node", "nodejs":
		return "node/Node.js"
	default:
		if portName != "" {
			return portName
		}
		return "unknown"
	}
}

// vscodeLaunchConfig returns the lines of a VSCode launch configuration object
// (without the surrounding braces) for the given debug port.
func vscodeLaunchConfig(p step.DebugPortMsg, addr string) []string {
	name := p.ResourceName
	if name == "" {
		name = fmt.Sprintf("port-%d", p.LocalPort)
	}
	switch p.PortName {
	case "dlv":
		return []string{
			`{`,
			fmt.Sprintf(`  "name": "Attach %s",`, name),
			`  "type": "go",`,
			`  "request": "attach",`,
			`  "mode": "remote",`,
			fmt.Sprintf(`  "port": %d,`, p.LocalPort),
			fmt.Sprintf(`  "host": "%s"`, addr),
		}
	case "jvm":
		return []string{
			`{`,
			fmt.Sprintf(`  "name": "Attach %s",`, name),
			`  "type": "java",`,
			`  "request": "attach",`,
			fmt.Sprintf(`  "hostName": "%s",`, addr),
			fmt.Sprintf(`  "port": %d`, p.LocalPort),
		}
	case "ptvsd", "debugpy":
		return []string{
			`{`,
			fmt.Sprintf(`  "name": "Attach %s",`, name),
			`  "type": "python",`,
			`  "request": "attach",`,
			`  "connect": {`,
			fmt.Sprintf(`    "host": "%s",`, addr),
			fmt.Sprintf(`    "port": %d`, p.LocalPort),
			`  }`,
		}
	case "node", "nodejs":
		return []string{
			`{`,
			fmt.Sprintf(`  "name": "Attach %s",`, name),
			`  "type": "node",`,
			`  "request": "attach",`,
			fmt.Sprintf(`  "address": "%s",`, addr),
			fmt.Sprintf(`  "port": %d,`, p.LocalPort),
			`  "localRoot": "${workspaceFolder}",`,
			`  "remoteRoot": "/app"`,
		}
	default:
		return []string{
			`{`,
			fmt.Sprintf(`  "name": "Attach %s",`, name),
			fmt.Sprintf(`  "port": %d,`, p.LocalPort),
			fmt.Sprintf(`  "address": "%s"`, addr),
		}
	}
}

// wizardValuesFromState reconstructs a WizardValues from a saved InstanceState.
func wizardValuesFromState(inst *InstanceState) WizardValues {
	str := inst.StringValues
	strs := inst.SliceValues
	if str == nil {
		str = map[string]string{}
	}
	if strs == nil {
		strs = map[string][]string{}
	}
	return WizardValues{str: str, strs: strs}
}

// switchToInstance cancels the current instance context, clears panel content,
// and sets the new instance name. Callers are responsible for calling
// registerPipeline and starting watchers / executeStart as needed.
func (m *model) switchToInstance(name string) {
	cancelInstance()
	instanceCtx, cancelInstance = context.WithCancel(context.Background())
	m.instanceName = name
	m.debugPorts = nil
	// Clear panel buffers (defs are reset by a subsequent registerPipeline call).
	for i := range m.panels {
		for j := range m.panels[i].bufs {
			m.panels[i].bufs[j] = nil
		}
		m.panels[i].activeIdx = 0
	}
	for i := range m.panelVPs {
		m.panelVPs[i].SetContent("")
	}
}

// buildPipelineFromState reconstructs the step graph from a saved InstanceState,
// used when restoring a previously-running instance after a restart.
func (m *model) buildPipelineFromState(instanceName string, inst *InstanceState) []StepDef {
	values := wizardValuesFromState(inst)
	defs, _ := m.buildDefsFromTemplates(values)
	return defs
}

// ResumeAction indicates what to do with a step on restart.
type ResumeAction string

const (
	ResumeActionSkip    ResumeAction = "skip"    // completed, don't restart
	ResumeActionRetry   ResumeAction = "retry"   // failed, try again
	ResumeActionRestart ResumeAction = "restart" // running when quit
	ResumeActionStart   ResumeAction = "start"   // pending/new
)

// determineResumeAction decides what to do with a step based on its saved state.
func determineResumeAction(stepID string, savedState map[string]StepState) ResumeAction {
	ss, exists := savedState[stepID]
	if !exists {
		return ResumeActionStart
	}

	switch ss.Status {
	case config.StepStatusCompleted:
		return ResumeActionSkip
	case config.StepStatusFailed:
		return ResumeActionRetry
	case config.StepStatusRunning:
		return ResumeActionRestart
	default:
		return ResumeActionStart
	}
}

// buildDefsFromTemplates builds a StepDef slice from all templates using values.
// On error it returns the error; callers that want best-effort (session restore)
// can ignore it.
func (m *model) buildDefsFromTemplates(values WizardValues) ([]StepDef, error) {
	sp := m.statePath
	var defs []StepDef

	// Clear and rebuild command registry
	m.customCommands = make(map[string]CommandSpec)

	for _, tmpl := range m.cfg.Steps {
		s, err := tmpl.Build(values)
		if err != nil {
			label := tmpl.Label
			if label == "" {
				label = "step"
			}
			return nil, fmt.Errorf("%s: %w", label, err)
		}
		if s == nil {
			continue
		}
		label := tmpl.Label
		if tmpl.LabelFunc != nil {
			label = tmpl.LabelFunc(values)
		}
		// Validate that Step.ID() matches template ID if template ID is set.
		if tmpl.ID != "" && s.ID() != tmpl.ID {
			return nil, fmt.Errorf("step %q: Step.ID() returned %q but template ID is %q",
				label, s.ID(), tmpl.ID)
		}

		// Register commands from this template
		for _, cmd := range tmpl.Commands {
			// Validate command
			if cmd.Name == "" {
				return nil, fmt.Errorf("step %q: command has empty Name", label)
			}
			if cmd.Handler == nil {
				return nil, fmt.Errorf("step %q: command %q has nil Handler", label, cmd.Name)
			}

			// Check for conflicts with other step commands
			if _, exists := m.customCommands[cmd.Name]; exists {
				return nil, fmt.Errorf("command name conflict: %q defined by multiple steps", cmd.Name)
			}

			// Check against built-in commands
			builtins := []string{"help", "start", "stop", "restart", "logs", "test", "theme"}
			for _, b := range builtins {
				if b == cmd.Name {
					return nil, fmt.Errorf("step %q: command %q conflicts with built-in command", label, cmd.Name)
				}
			}

			// Register command
			m.customCommands[cmd.Name] = cmd
		}

		var onReady func()
		if tmpl.OnReady != nil {
			fn := tmpl.OnReady
			onReady = func() { fn(sp) }
		}
		defs = append(defs, StepDef{
			Step: s,
			meta: stepMetadata{
				panel:        tmpl.Panel,
				label:        label,
				waitFor:      tmpl.WaitFor,
				autoActivate: tmpl.AutoActivate,
				hidden:       tmpl.Hidden,
				onReady:      onReady,
			},
		})
	}
	return defs, nil
}

// dispatchCommand routes typed text to internal command handlers.
func (m *model) dispatchCommand(line string) {
	parts := strings.Fields(line)
	if len(parts) == 0 {
		return
	}
	sp := m.statePath
	go func() { _ = AppendCommandHistory(sp, line) }()
	switch parts[0] {
	case "help":
		m.overlay = overlayHelp
		m.flipTarget = 1.0

	case "start":
		// Pre-populate the wizard from the last saved session, if any.
		var initial WizardValues
		if state, err := LoadState(sp); err == nil && state.Instance != nil {
			initial = wizardValuesFromState(state.Instance)
		}
		m.overlay = overlayWizard
		m.wizard = newStartWizard(m, initial)
		m.flipTarget = 1.0

	case "stop":
		name := m.instanceName
		m.printLine("$ stop")
		if name == "" {
			m.printLine("  no active instance — run: start")
			break
		}
		m.steps = map[string]*commandStep{}
		cancelInstance()
		instanceCtx, cancelInstance = context.WithCancel(context.Background())
		m.instanceName = ""
		// Clear panel bufs (keep defs so stop output routes correctly).
		for i := range m.panels {
			for j := range m.panels[i].bufs {
				m.panels[i].bufs[j] = nil
			}
			m.panels[i].activeIdx = 0
		}
		cancelTest()
		m.testBuf = nil
		m.testRunning = false
		m.testVP.SetContent("")
		m.debugPorts = nil
		m.fwdPorts = nil
		for i := range m.panelVPs {
			m.panelVPs[i].SetContent("")
		}
		// Collect stop tasks from active defs in reverse order (last-in, first-out).
		type stopTask struct {
			id    string
			label string
			step  Step
		}
		var stopTasks []stopTask
		for i := len(m.activeDefs) - 1; i >= 0; i-- {
			def := m.activeDefs[i]
			label := "stopping " + def.meta.label
			if def.meta.label == "" {
				label = "stopping step"
			}
			stopTasks = append(stopTasks, stopTask{
				id:    fmt.Sprintf("stop-%d", i),
				label: label,
				step:  def.Step,
			})
		}
		// Check if MFE is running before adding it to the stop list.
		var mfePGID int
		if state, err := LoadState(sp); err == nil && state.Instance != nil {
			mfePGID = state.Instance.MFEPGID
		}
		if mfePGID > 0 {
			m.startStep("mfe-stop", "stopping MFE")
		}
		for _, t := range stopTasks {
			m.startStep(t.id, t.label)
		}
		go func() {
			prog.Send(cmdActiveMsg(+1))
			// Kill MFE process group if running.
			if mfePGID > 0 {
				step.KillProcessGroup(mfePGID)
				prog.Send(stepDoneMsg{id: "mfe-stop", ok: true, label: "MFE stopped"})
			}
			// Stop steps in reverse order.
			for _, t := range stopTasks {
				_ = t.step.Stop(context.Background(), name)
				prog.Send(stepDoneMsg{id: t.id, ok: true, label: t.label + " done"})
			}
			prog.Send(cmdActiveMsg(-1))
			_ = MarkInactive(sp)
			prog.Send(instanceStoppedMsg{})
			prog.Send(clearActiveDefsMsg{})
		}()

	case "restart":
		m.printLine("$ " + strings.Join(parts, " "))
		if m.instanceName == "" {
			m.printLine("  no active instance — run: start")
			break
		}
		if len(parts) < 2 {
			m.printLine("  usage: restart <step-id>")
			break
		}
		id := parts[1]
		def, ok := m.findDef(id)
		if !ok {
			m.printLine("  unknown step: " + id)
			break
		}
		name := m.instanceName
		// Cancel the existing step context.
		if e, exists := m.stepCtxs[id]; exists {
			e.cancel()
		}
		// Clear the panel buffer for this step.
		if pid, idx := m.panelAndIdx(id); idx >= 0 {
			m.panels[pid].bufs[idx] = nil
			if m.panels[pid].activeIdx == idx {
				m.panelVPs[pid].SetContent("")
			}
		}
		// Create a new per-step context.
		stepCtx, stepCancel := context.WithCancel(instanceCtx)
		if m.stepCtxs == nil {
			m.stepCtxs = make(map[string]stepEntry)
		}
		m.stepCtxs[id] = stepEntry{ctx: stepCtx, cancel: stepCancel}
		// Clear step state before restart
		_ = UpdateStepState(sp, id, config.StepStatusPending, nil)
		// Truncate log file if any.
		if lp := def.Step.LogPath(name); lp != "" {
			_ = os.Truncate(lp, 0)
			go watchStep(stepCtx, def, name)
		}
		// Re-register spinner and start the step.
		m.startStep(id, def.effectiveLabel())
		go func() {
			_ = UpdateStepState(sp, id, config.StepStatusRunning, nil)
			if err := def.Step.Start(stepCtx, name); err != nil {
				_ = UpdateStepState(sp, id, config.StepStatusFailed, err)
				prog.Send(stepDoneMsg{id: id, ok: false, label: def.effectiveLabel() + " failed: " + err.Error()})
				return
			}
			_ = UpdateStepState(sp, id, config.StepStatusCompleted, nil)
			prog.Send(stepDoneMsg{id: id, ok: true, label: def.effectiveLabel() + " running"})
		}()

	case "logs":
		m.printLine("$ logs")
		if m.instanceName == "" {
			m.printLine("  no active instance — run: start")
		} else {
			name := m.instanceName
			for _, pv := range m.panels {
				for _, def := range pv.defs {
					lp := def.Step.LogPath(name)
					if lp != "" {
						m.printLine(fmt.Sprintf("  %-10s %s", def.Step.ID()+":", lp))
					}
				}
			}
		}

	case "test":
		m.printLine("$ test")
		if m.instanceName == "" {
			m.printLine("  no active instance — run: start")
			break
		}
		if len(m.cfg.Tests) == 0 {
			m.printLine("  no tests configured")
			break
		}
		if m.testRunning {
			m.printLine("  test already running")
			break
		}
		// Resolve which test template to run.
		var tmpl *TestTemplate
		if len(parts) > 1 {
			label := strings.Join(parts[1:], " ")
			for i := range m.cfg.Tests {
				if m.cfg.Tests[i].Label == label {
					tmpl = &m.cfg.Tests[i]
					break
				}
			}
			if tmpl == nil {
				m.printLine("  unknown test: " + label)
				m.printLine("  available:")
				for _, t := range m.cfg.Tests {
					m.printLine("    " + t.Label)
				}
				break
			}
		} else if len(m.cfg.Tests) == 1 {
			tmpl = &m.cfg.Tests[0]
		} else {
			m.printLine("  multiple test suites configured — use: test <label>")
			for _, t := range m.cfg.Tests {
				m.printLine("    " + t.Label)
			}
			break
		}
		// Build wizard values from saved state (same selections used at start).
		var values WizardValues
		if state, err := LoadState(sp); err == nil && state.Instance != nil {
			values = wizardValuesFromState(state.Instance)
		}
		tc, err := tmpl.Build(values)
		if err != nil {
			m.printLine("  error building test command: " + err.Error())
			break
		}
		if tc.Cmd == "" {
			m.printLine("  test template returned empty command")
			break
		}
		// Switch BottomRight to the Tests tab and clear previous output.
		m.testBuf = nil
		m.testVP.SetContent("")
		m.panels[PanelBottomRight].activeIdx = len(m.panels[PanelBottomRight].defs)
		m.testRunning = true
		cancelTest()
		var testCtx context.Context
		testCtx, cancelTest = context.WithCancel(instanceCtx)
		label := tmpl.Label
		go func() {
			cmd := exec.Command(tc.Cmd, tc.Args...)
			cmd.Dir = tc.Dir
			if len(tc.Env) > 0 {
				cmd.Env = os.Environ()
				for k, v := range tc.Env {
					cmd.Env = append(cmd.Env, k+"="+v)
				}
			}
			ok := true
			step.StreamCmd(testCtx, cmd, func(line string) {
				if strings.HasPrefix(line, "[exited:") {
					ok = false
				}
				prog.Send(testLineMsg(line))
			})
			if testCtx.Err() != nil {
				prog.Send(testLineMsg("  [" + label + " cancelled]"))
				prog.Send(testDoneMsg{ok: false})
				return
			}
			prog.Send(testDoneMsg{ok: ok})
		}()

	case "theme":
		m.printLine("$ " + line)
		if len(parts) < 2 {
			m.printLine("themes: " + themeNames())
		} else {
			name := parts[1]
			found := false
			for _, t := range presets {
				if t.Name == name {
					currentTheme = t
					m.helpOverlayVP.SetContent(m.helpContent(m.helpOverlayVP.Width))
					m.printLine("theme set to: " + name)
					found = true
					go func() { _ = SaveTheme(sp, name) }()
					break
				}
			}
			if !found {
				m.printLine("unknown theme: " + name + " (try: " + themeNames() + ")")
			}
		}

	default:
		cmdName := parts[0]
		args := parts[1:]

		// Try custom commands
		if cmd, ok := m.customCommands[cmdName]; ok {
			m.printLine("$ " + line)

			// Get current wizard values for the handler
			var values WizardValues
			if state, err := LoadState(sp); err == nil && state.Instance != nil {
				values = wizardValuesFromState(state.Instance)
			}

			// Execute handler in goroutine (don't block UI)
			go func() {
				prog.Send(cmdActiveMsg(+1))
				err := cmd.Handler(args, m.instanceName, values)
				prog.Send(cmdActiveMsg(-1))

				if err != nil {
					prog.Send(commandLineMsg(fmt.Sprintf("  error: %v", err)))
				}
			}()
		} else {
			m.printLine("$ " + line)
			m.printLine("unknown command: " + cmdName + " (try: help)")
		}
	}
}

func (m *model) executeStartFromWizard() {
	wiz := m.wizard
	sp := m.statePath
	name := m.configuredName()

	m.printLine("$ start")
	values := wiz.buildValues()
	defs, err := m.buildDefsFromTemplates(values)
	if err != nil {
		prog.Send(commandLineMsg("error: " + err.Error()))
		return
	}

	// Check if there's an existing instance state with step states (resume scenario)
	var existingStepStates map[string]StepState
	if state, err := LoadState(sp); err == nil && state.Instance != nil && len(state.Instance.StepStates) > 0 {
		existingStepStates = state.Instance.StepStates
	}

	// Persist wizard selections and set StartedAt immediately.
	// If resuming, keep existing step states; otherwise initialize all as pending.
	var stepStates map[string]StepState
	if existingStepStates != nil {
		stepStates = existingStepStates
	} else {
		stepStates = make(map[string]StepState)
		for _, def := range defs {
			stepStates[def.Step.ID()] = StepState{
				ID:     def.Step.ID(),
				Status: config.StepStatusPending,
			}
		}
	}
	go func() {
		_ = SaveInstanceState(sp, InstanceState{
			StartedAt:    time.Now().UTC().Format(time.RFC3339),
			StringValues: values.str,
			SliceValues:  values.strs,
			StepStates:   stepStates,
		})
	}()

	m.switchToInstance(name)
	m.activeDefs = defs // Store for use by stop command
	m.registerPipeline(defs)
	m.fullscreenTarget = 0

	// Choose execution path based on whether we're resuming
	if existingStepStates != nil {
		// Resume: use saved step states to skip/retry/restart
		m.executeStartWithResume(defs, existingStepStates)
	} else {
		// Fresh start: truncate logs and start all steps
		for _, def := range defs {
			if lp := def.Step.LogPath(name); lp != "" {
				_ = os.Truncate(lp, 0)
			}
		}
		m.executeStart(defs)
	}

	// Start watchers using the per-step contexts created by executeStart/executeStartWithResume.
	// Skip steps with PanelNone (no output destination).
	for _, def := range defs {
		if def.meta.panel == PanelNone {
			continue
		}
		id := def.Step.ID()
		if e, ok := m.stepCtxs[id]; ok {
			go watchStep(e.ctx, def, name)
		}
	}
}

// topoSortSteps returns a topologically sorted copy of defs.
// Steps with no dependencies come first, followed by steps that depend on them.
// Steps at the same dependency level maintain their original relative order.
// If there's a cycle, returns the original order.
func topoSortSteps(defs []StepDef) []StepDef {
	if len(defs) == 0 {
		return defs
	}

	// Build dependency graph and index mapping
	graph := make(map[string][]string)   // id -> list of ids that depend on it
	inDegree := make(map[string]int)     // id -> count of unresolved dependencies
	idToIdx := make(map[string]int)      // id -> original index in defs
	allIDs := make(map[string]bool)      // set of all step IDs

	// First pass: collect all IDs
	for _, def := range defs {
		allIDs[def.Step.ID()] = true
	}

	// Second pass: build graph
	for i, def := range defs {
		id := def.Step.ID()
		idToIdx[id] = i
		if _, ok := inDegree[id]; !ok {
			inDegree[id] = 0
		}
		for _, dep := range def.meta.waitFor {
			// Only count dependencies that exist in this step set
			if allIDs[dep] {
				graph[dep] = append(graph[dep], id)
				inDegree[id]++
			}
		}
	}

	// Find all nodes with no dependencies, preserving original order
	var queue []string
	for _, def := range defs {
		id := def.Step.ID()
		if inDegree[id] == 0 {
			queue = append(queue, id)
		}
	}

	// Kahn's algorithm for topological sort
	var sorted []string
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		sorted = append(sorted, id)

		// Reduce in-degree for all dependents
		for _, dependent := range graph[id] {
			inDegree[dependent]--
			if inDegree[dependent] == 0 {
				queue = append(queue, dependent)
			}
		}
	}

	// If cycle detected (not all nodes processed), return original order
	if len(sorted) != len(defs) {
		return defs
	}

	// Build sorted defs slice
	result := make([]StepDef, len(defs))
	for i, id := range sorted {
		result[i] = defs[idToIdx[id]]
	}

	return result
}

// executeStart launches all step processes with dependency ordering.
// Steps with WaitFor set block until their dependency signals ready.
func (m *model) executeStart(defs []StepDef) {
	m.steps = map[string]*commandStep{}
	m.stepCtxs = make(map[string]stepEntry)
	name := m.instanceName

	// Create per-step contexts and build a ready channel for each step.
	ready := make(map[string]chan struct{}, len(defs))
	for _, def := range defs {
		id := def.Step.ID()
		ready[id] = make(chan struct{})
		stepCtx, stepCancel := context.WithCancel(instanceCtx)
		m.stepCtxs[id] = stepEntry{ctx: stepCtx, cancel: stepCancel}
	}

	// Register visible steps in the commands panel tracker in topological order.
	sortedDefs := topoSortSteps(defs)
	for _, def := range sortedDefs {
		if def.meta.hidden {
			continue
		}
		label := def.effectiveLabel()
		if len(def.meta.waitFor) == 0 {
			m.startStep(def.Step.ID(), label)
		} else {
			m.startPendingStep(def.Step.ID(), label, def.meta.waitFor)
		}
	}

	for _, def := range defs {
		def := def
		id := def.Step.ID()
		stepCtx := m.stepCtxs[id].ctx
		go func() {
			// Wait for all dependencies in parallel, crossing each off as it completes.
			if len(def.meta.waitFor) > 0 {
				remaining := make(chan struct{}, len(def.meta.waitFor))
				for _, dep := range def.meta.waitFor {
					dep := dep
					go func() {
						if ch, ok := ready[dep]; ok {
							select {
							case <-ch:
								prog.Send(stepDepReadyMsg{id: id, dep: dep})
								remaining <- struct{}{}
							case <-instanceCtx.Done():
							}
						} else {
							remaining <- struct{}{}
						}
					}()
				}
				for range def.meta.waitFor {
					select {
					case <-remaining:
					case <-instanceCtx.Done():
						return
					}
				}
				// Activate this step (triggers spinner + AutoActivate if set).
				prog.Send(stepActivateMsg{id: id})
			}

			// Mark step as running
			sp := m.statePath
			_ = UpdateStepState(sp, id, config.StepStatusRunning, nil)

			// Start the step.
			if err := def.Step.Start(stepCtx, name); err != nil {
				_ = UpdateStepState(sp, id, config.StepStatusFailed, err)

				// Close ready channel to unblock dependents
				close(ready[id])

				// Notify dependent steps of failure
				for _, otherDef := range defs {
					for _, dep := range otherDef.meta.waitFor {
						if dep == id {
							prog.Send(stepDepFailedMsg{id: otherDef.Step.ID(), failedDep: id})
						}
					}
				}

				if !def.meta.hidden {
					prog.Send(stepDoneMsg{
						id:    id,
						ok:    false,
						label: def.effectiveLabel() + " failed: " + err.Error(),
					})
				}
				return
			}

			// Mark step as completed
			_ = UpdateStepState(sp, id, config.StepStatusCompleted, nil)

			// Signal ready to unblock any dependents.
			close(ready[id])

			// Invoke the OnReady callback.
			if def.meta.onReady != nil {
				go def.meta.onReady()
			}

			if !def.meta.hidden {
				prog.Send(stepDoneMsg{
					id:    id,
					ok:    true,
					label: def.effectiveLabel() + " running",
				})
			}
		}()
	}
}

// executeStartWithResume launches step processes with resume logic based on saved state.
// Steps that completed successfully are skipped, failed steps are retried, and steps
// that were running when the instance quit are restarted.
func (m *model) executeStartWithResume(defs []StepDef, savedStates map[string]StepState) {
	m.steps = map[string]*commandStep{}
	m.stepCtxs = make(map[string]stepEntry)
	name := m.instanceName
	sp := m.statePath

	// Create per-step contexts and build a ready channel for each step.
	ready := make(map[string]chan struct{}, len(defs))
	for _, def := range defs {
		id := def.Step.ID()
		ready[id] = make(chan struct{})
		stepCtx, stepCancel := context.WithCancel(instanceCtx)
		m.stepCtxs[id] = stepEntry{ctx: stepCtx, cancel: stepCancel}
	}

	// Determine resume action for each step
	resumeActions := make(map[string]ResumeAction)
	for _, def := range defs {
		resumeActions[def.Step.ID()] = determineResumeAction(def.Step.ID(), savedStates)
	}

	// Register visible steps in the commands panel tracker in topological order.
	sortedDefs := topoSortSteps(defs)
	for _, def := range sortedDefs {
		if def.meta.hidden {
			continue
		}
		id := def.Step.ID()
		label := def.effectiveLabel()
		action := resumeActions[id]

		// Add suffix to label based on resume action
		switch action {
		case ResumeActionSkip:
			label = label + " (restored)"
		case ResumeActionRetry:
			label = label + " (retrying)"
		case ResumeActionRestart:
			label = label + " (restarting)"
		}

		if len(def.meta.waitFor) == 0 {
			m.startStep(id, label)
		} else {
			m.startPendingStep(id, label, def.meta.waitFor)
		}
	}

	for _, def := range defs {
		def := def
		id := def.Step.ID()
		action := resumeActions[id]
		stepCtx := m.stepCtxs[id].ctx

		go func() {
			// Wait for all dependencies in parallel, crossing each off as it completes.
			if len(def.meta.waitFor) > 0 {
				remaining := make(chan struct{}, len(def.meta.waitFor))
				for _, dep := range def.meta.waitFor {
					dep := dep
					go func() {
						if ch, ok := ready[dep]; ok {
							select {
							case <-ch:
								prog.Send(stepDepReadyMsg{id: id, dep: dep})
								remaining <- struct{}{}
							case <-instanceCtx.Done():
							}
						} else {
							remaining <- struct{}{}
						}
					}()
				}
				for range def.meta.waitFor {
					select {
					case <-remaining:
					case <-instanceCtx.Done():
						return
					}
				}
				// Activate this step (triggers spinner + AutoActivate if set).
				prog.Send(stepActivateMsg{id: id})
			}

			// Handle based on resume action
			if action == ResumeActionSkip {
				// Step already completed - skip Start(), just mark as done
				_ = UpdateStepState(sp, id, config.StepStatusCompleted, nil)
				close(ready[id])
				if def.meta.onReady != nil {
					go def.meta.onReady()
				}
				if !def.meta.hidden {
					prog.Send(stepDoneMsg{
						id:    id,
						ok:    true,
						label: def.effectiveLabel() + " (restored)",
					})
				}
				return
			}

			// For retry/restart/start: truncate log if restarting
			if action == ResumeActionRestart {
				if lp := def.Step.LogPath(name); lp != "" {
					_ = os.Truncate(lp, 0)
				}
			}

			// Clear error for retry
			if action == ResumeActionRetry {
				_ = UpdateStepState(sp, id, config.StepStatusPending, nil)
			}

			// Mark step as running
			_ = UpdateStepState(sp, id, config.StepStatusRunning, nil)

			// Start the step.
			if err := def.Step.Start(stepCtx, name); err != nil {
				_ = UpdateStepState(sp, id, config.StepStatusFailed, err)

				// Close ready channel to unblock dependents
				close(ready[id])

				// Notify dependent steps of failure
				for _, otherDef := range defs {
					for _, dep := range otherDef.meta.waitFor {
						if dep == id {
							prog.Send(stepDepFailedMsg{id: otherDef.Step.ID(), failedDep: id})
						}
					}
				}

				if !def.meta.hidden {
					prog.Send(stepDoneMsg{
						id:    id,
						ok:    false,
						label: def.effectiveLabel() + " failed: " + err.Error(),
					})
				}
				return
			}

			// Mark step as completed
			_ = UpdateStepState(sp, id, config.StepStatusCompleted, nil)

			// Signal ready to unblock any dependents.
			close(ready[id])

			// Invoke the OnReady callback.
			if def.meta.onReady != nil {
				go def.meta.onReady()
			}

			if !def.meta.hidden {
				prog.Send(stepDoneMsg{
					id:    id,
					ok:    true,
					label: def.effectiveLabel() + " running",
				})
			}
		}()
	}
}
