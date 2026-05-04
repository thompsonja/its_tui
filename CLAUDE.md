# CLAUDE.md — tui

## What this is

A Go library (`module github.com/thompsonja/its_tui`) that provides a Bubbletea terminal dashboard for Kubernetes development workflows. Callers import package `tui`, supply a `Config` with step templates, and call `tui.Run()`. The `sample/` directory is the reference binary.

## Module layout

```
tui/          — library (package tui): the entire TUI implementation
step/         — Step interface + WatchStep + exec helpers (package step)
config/       — InstanceState persistence + catalogue types (package config)
builtins/     — ready-made step constructors (minikube, kubectl, skaffold, mfe)
sample/       — example binary wiring Config with builtins
```

## Architecture: model → appState + viewState

The Bubbletea `tea.Model` shim is `model` in `model.go`. It is split into two sub-structs:

- **`appState`** (`appstate.go`) — domain state: config, panels, log buffers, step contexts, overlay kind, wizard pointer, port lists. Independent of rendering.
- **`viewState`** (`viewstate.go`) — ephemeral display state: viewports, animations, search state. Derived from appState on demand.

`model.app` is the source of truth; `model.vs` is what gets rendered.

The 60fps tick (`tickCmd`) drives spinner frames and card-flip / fullscreen animations.

`dispatchCommand` returns `tea.Cmd` so command handlers can schedule Bubbletea effects. Currently all built-in handlers return `nil` (they use goroutines + `prog.Send`), but the signature allows future handlers to emit proper `tea.Cmd` values.

## Key file map

| File | Purpose |
|---|---|
| `tui/tui.go` | Public API: `Config`, `StepTemplate`, `FieldSpec`, `WizardValues`, `PanelID`, `Run()`, type aliases |
| `tui/model.go` | `model` struct + `newModel` + `Init` + `cycleFocus` + `resizePanels` + `refreshFocusedPanel` |
| `tui/appstate.go` | `appState`, `instanceRuntime`, `panelView`, `stepEntry`, `focusedPanelID`, buffer helpers |
| `tui/viewstate.go` | `viewState`, `resize`, all render methods, `wrapContentSearch` |
| `tui/update.go` | `Update()` — message dispatch, animation advances, log ingestion |
| `tui/view.go` | `View()` shim (delegates to `vs.render`), `appendToVP` |
| `tui/messages.go` | All internal message types (`tickMsg`, `stepDoneMsg`, etc.) |
| `tui/pipeline.go` | `buildDefsFromTemplates`, `topoSortSteps`, `switchToInstance`, resume logic |
| `tui/execution.go` | `executeStartFromWizard`, `executeStartWithResume` |
| `tui/commands.go` | `dispatchCommand` + all REPL command handlers (`start`, `stop`, `restart`, `logs`, `status`, `test`, `theme`, custom) |
| `tui/panel_steps.go` | `commandStep` tracker: `startStep`, `startPendingStep`, `finishStep` |
| `tui/wizard.go` | `startWizard`, `fieldState`, `newStartWizard`, `reEvalDynamicFields` |
| `tui/wizard_keys.go` | `handleWizardKey` per FieldKind |
| `tui/history.go` | REPL history (up/down arrows) |
| `tui/styles.go` | Panel/title styles |
| `tui/themes.go` | `Theme` struct, presets, `currentTheme` global |
| `tui/log.go` | Debug log init/write/close (`~/.tui/debug.log`) |
| `tui/debug.go` | Clipboard helper, VSCode launch.json management, debug port utilities |
| `step/step.go` | `Step` interface, `WatchStep`, `ResumeStep`, `Stopper`/`Resumer`/`Sender` optional interfaces |
| `step/messages.go` | `LineMsg`, `SetMsg`, `DebugPortMsg` sent from goroutines → TUI |
| `config/state.go` | `State`, `InstanceState`, `StepState`, `SaveInstanceState`, `LoadState` |
| `config/config.go` | `System`, `Component`, `ComponentsFile` catalogue types |

## Panels

The 2×2 grid has three content panels (indexed by `PanelID`) plus the Commands panel:

```
PanelTopLeft  (0)  | PanelTopRight  (1)
───────────────────────────────────────
Commands panel     | PanelBottomRight (2)
```

Focus cycles through four constants: `panelMinikube`, `panelSkaffold`, `panelCommands`, `panelMFE`. These map to PanelIDs via `focusedPanelID()`.

Each content panel can host multiple steps. The `panelView` struct holds `defs []StepDef`, `bufs [][]string` (one log buffer per step), and `activeIdx` (which step is visible). Press `t` to cycle tabs within a panel.

`PanelNone` (`PanelID = -1`) assigns a step to no panel: it runs but produces no visible output and no tab.

## Streaming log flow

1. A step goroutine writes to its log file (returned by `Step.LogPath`).
2. `step.WatchStep` tails the file and calls `Send(LineMsg{ID, Line})`.
3. `Send` is the package-level function in `step/`, set to `prog.Send` at startup.
4. `Update` receives `step.LineMsg`, finds the panel/bufIdx via `app.panelAndIdx`, appends to `pv.bufs[idx]`, and syncs the viewport if that buffer is active.

Steps that produce output themselves (e.g. polling steps) skip the log file and send `step.SetMsg` directly to replace the viewport content rather than append.

## Wizard

`startWizard` (`wizard.go`) is created by `newStartWizard(cfg Config, vpWidth int, initial WizardValues)`. Each `StepTemplate.Fields` slice contributes `FieldSpec` entries. The wizard renders inside the Commands panel via a card-flip animation (`flipProgress` 0→1).

Field kinds:
- `FieldKindSelect` — horizontal selector (left/right arrows)
- `FieldKindSingleSelect` — searchable single picker
- `FieldKindMultiSelect` — searchable multi picker
- `FieldKindSystemSelect` — hierarchical system/component picker
- `FieldKindText` — free text

`FieldSpec.Options []string` provides a static option list for Select/SingleSelect/MultiSelect fields. `FieldSpec.OptionsFunc` provides dynamic options and takes precedence over `Options` when both are set. `SystemsFunc` is always required for SystemSelect. `reEvalDynamicFields` is called after every change.

The `overlay overlayKind` and `wizard *startWizard` fields live in `appState` (domain state), not `viewState`. The wizard pointer is model state that persists across render cycles; the overlay kind determines which UI mode is active.

## Step lifecycle

1. `buildDefsFromTemplates` calls each `StepTemplate.Build(values)` → `StepDef`.
2. `registerPipeline` partitions defs into `app.panels[pid]` by `Panel`.
3. `executeStartFromWizard` (or `executeStartWithResume` on session restore) launches goroutines:
   - Deps with `WaitFor` block on a channel until each dependency fires `stepActivateMsg`.
   - Each goroutine calls `step.Step.Start(ctx, instanceName)`.
   - On return: sends `stepDoneMsg{ok: true/false}`, calls `OnReady` if set.
4. `step.WatchStep` runs in a separate goroutine for each step with a log file.

`StepDef.meta.waitFor` is a `[]string` of step IDs. `topoSortSteps` does Kahn's algorithm to order execution so the Commands panel renders deps in dependency order.

Per-instance execution state is grouped in `instanceRuntime`:
```go
type instanceRuntime struct {
    name           string
    activeDefs     []StepDef
    stepCtxs       map[string]stepEntry
    completedSteps int
    totalSteps     int
}
```
Access via `m.app.inst.*`.

## Session persistence

State lives at `DefaultStatePath()` (typically `~/.tui/state.json`). `InstanceState` stores:
- `StringValues` / `SliceValues` — wizard selections to pre-populate next wizard open
- `StepStates` — each step's last status so resume can skip/retry/restart appropriately
- `PanelTabs` — active tab index per panel
- `StepData map[string]map[string]string` — per-step key-value store for arbitrary runtime state

On resume, `buildPipelineFromState` reconstructs defs from saved values, then `executeStartWithResume` checks each step's saved `StepStatus`:
- `completed` → skip (unless `Resumer.Resume()` returns error)
- `failed` → retry via `Start()`
- `running` / `pending` → restart via `Start()`

`wizardValuesFromState` sets `isRestore: true` on the returned `WizardValues`. `Build()` implementations can call `values.IsRestore()` to detect the session-restore path and skip expensive re-generation (e.g. writing a new skaffold.yaml) when they can reuse previously saved state.

**Important:** `SaveInstanceState` replaces the entire `InstanceState` struct. Any call site that invokes `SaveInstanceState` must first load and carry forward `StepData` to avoid clobbering data written by `Build()` calls. See `executeStartFromWizard` for the pattern.

### StepData — per-step key-value storage

Steps that need to survive TUI restarts (e.g., long-running processes) use `StepData` to persist runtime state. The API in `config/state.go`:

```go
config.SaveStepData(statePath, stepID, key, value string) error
config.LoadStepData(statePath, stepID, key string) (string, bool)
config.GetStepData(statePath, stepID string) map[string]string
```

**PID persistence pattern** (used by `kubectl` and `skaffold` builtins):
- After `cmd.Start()`, call `config.SaveStepData(sp, s.ID(), "pid", strconv.Itoa(cmd.Process.Pid))` **synchronously** before blocking on process completion. Writing synchronously ensures the PID lands in state.json even if the TUI exits immediately after startup.
- In the process exit goroutine, clear the PID: `config.SaveStepData(sp, s.ID(), "pid", "")`.
- In `Resume()`, read the PID with `config.LoadStepData`, parse it, and check liveness with `proc.Signal(syscall.Signal(0))`. Return `nil` if alive (skip `Start`), return an error if dead/missing (triggers `Start`).

Steps that complete and don't need resuming (e.g., one-shot `skaffold build`) should not save a PID — their `Resume()` simply returns `nil`.

**Generator cache pattern** (used by `SkaffoldFileGeneratorTemplate` / `SkaffoldPipeline`):
- In `Build()`, check `values.IsRestore()`. If true, load the previously generated file path from `StepData` and skip re-generation if the file still exists.
- On a fresh run, generate the file and save its path to `StepData` so the next restore can find it.
- Return `(nil, nil)` from `Build()` to indicate this is a side-effect-only step with no running process.

## VSCode launch.json management

When debug ports are forwarded, the TUI writes `attach` launch configurations into `.vscode/launch.json` inside `Config.WorkspaceDir`. All TUI-managed entries are prefixed with `its_tui_<instanceName> ` so they can be identified and removed cleanly on `stop`.

This is a no-op when `WorkspaceDir` is empty or when no `.vscode` directory exists there.

## Adding a new step template

1. Implement `step.Step` (or the equivalent `tui.Step` alias) in a new file (or in `builtins/`). Custom steps only need to import `tui` for the interface; `step` and `config` are only needed for `step.Send` / `step.SetMsg` and state persistence.
2. Write a `StepTemplate` constructor function with `Fields`, `Panel`, `Label`, `WaitFor`, and `Build`. Pass `StatePath: config.DefaultStatePath()` when constructing the step so it can access state.json.
3. If the step needs custom cleanup on stop (e.g. kill a process group, run a teardown command), implement `tui.Stopper` (`= step.Stopper`). Steps that don't implement `Stopper` are skipped during stop — their processes are terminated by context cancellation.
4. If the step needs resume customization, implement `tui.Resumer` (`= step.Resumer`). Long-running processes should save their PID via `config.SaveStepData` and check liveness in `Resume()`. One-shot steps (steps that complete and leave no process running) should return `nil` from `Resume()` unconditionally.
5. If it sends output directly (no log file), have `LogPath` return `""` and send `tui.SetMsg` or `tui.LineMsg` via `tui.SendMsg` (or the injected sender if you implement `tui.Sender`).
6. Register the template in `Config.Steps`.

## Adding a custom REPL command

Add a `CommandSpec` to `StepTemplate.Commands`:

```go
Commands: []CommandSpec{{
    Name:    "rebuild",
    Help:    "trigger a rebuild",
    Handler: func(args []string, instanceName string, values WizardValues) error { ... },
}},
```

Built-in names (`help`, `start`, `stop`, `restart`, `logs`, `status`, `test`, `theme`) are reserved — `buildDefsFromTemplates` will return an error for conflicts.

## Build and test

```
go build ./...
go test ./...
```

No build tags. Tests use `fakeStep` and `fakeBuild` helpers defined at the top of `pipeline_test.go`. Tests in `tui/` construct `model` structs directly as `&model{app: appState{cfg: Config{...}}}` — do not use `newModel` in tests as it reads `os.Getwd()`.

`newStartWizard` takes `(cfg Config, commandsVPWidth int, initial WizardValues)` — pass `0` for width in tests.

## Runtime globals

Three package-level globals in `tui/tui.go`:
- `prog *tea.Program` — the running program; used by goroutines to send messages
- `instanceCtx / cancelInstance` — governs all goroutines for the current instance
- `cancelTest` — stops any in-progress test run

`step.Send` (in `step/step.go`) is set once at startup to `prog.Send`. Steps also accept injected senders via the `step.Sender` interface for isolated testing.

## Skaffold pipeline (builtins)

`SkaffoldPipeline` is the recommended entry point for multi-step skaffold workflows. It creates and wires the generator, build, and dev templates from a single generate function:

```go
pipeline := builtins.NewSkaffoldPipeline(func(v tui.WizardValues) (string, []string, error) {
    return generateSkaffoldYAML(v.String("env"))
})

steps := []tui.StepTemplate{builtins.MinikubeTemplate(), builtins.KubectlTemplate(), ...}
steps = append(steps, pipeline.Templates()...)  // generator + build + dev
```

- `GeneratorTemplate()` — hidden step that calls generate once and caches to `StepData`
- `BuildTemplate()` — `skaffold build`, waits for minikube
- `DevTemplate()` — `skaffold dev/run/debug`, waits for minikube + build (build dep silently no-ops if `BuildTemplate` is not registered)
- `Templates()` — returns all three in registration order

For custom wiring (e.g. no build step), use the lower-level functions directly: `SkaffoldFileGeneratorTemplate`, `SkaffoldTemplateFrom`, `SkaffoldBuildTemplateFrom`.

## Style rules

- `appState` owns domain data (including overlay kind and wizard pointer); `viewState` owns ephemeral display state (viewports, animations). The rule: if removing the field would change observable program behavior, it belongs in `appState`.
- All log ingestion goes through `Update` message dispatch — no direct viewport mutation from goroutines.
- `dispatchCommand` returns `tea.Cmd`. All command handler methods (`handleXxx`) return `tea.Cmd`. Handlers that only use goroutines + `prog.Send` return `nil`.
- Animations are driven by `tickMsg` advancing `flipProgress` / `fullscreenProgress` by a fixed step per frame; the viewport resize happens when the animation settles.
- Buffer cap is `maxBufLines = 5000`. `appendLine` enforces this.
