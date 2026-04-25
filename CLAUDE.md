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

- **`appState`** (`appstate.go`) — domain state: config, panels, log buffers, step contexts, wizard values, port lists. Independent of rendering.
- **`viewState`** (`viewstate.go`) — ephemeral display state: viewports, animations, overlays, the wizard pointer, search mode. Derived from appState on demand.

`model.app` is the source of truth; `model.vs` is what gets rendered.

The 60fps tick (`tickCmd`) drives spinner frames and card-flip / fullscreen animations.

## Key file map

| File | Purpose |
|---|---|
| `tui/tui.go` | Public API: `Config`, `StepTemplate`, `FieldSpec`, `WizardValues`, `PanelID`, `Run()`, type aliases |
| `tui/model.go` | `model` struct + `newModel` + `Init` + `cycleFocus` + `resizePanels` |
| `tui/appstate.go` | `appState`, `panelView`, `focusedPanelID`, buffer helpers |
| `tui/viewstate.go` | `viewState`, `resize`, all render methods, wizard render helpers |
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
| `step/step.go` | `Step` interface, `WatchStep`, `ResumeStep`, `Sender` interface |
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

`FieldSpec.OptionsFunc` / `SystemsFunc` are called with current `WizardValues` so fields can be dynamic. `reEvalDynamicFields` is called after every change.

## Step lifecycle

1. `buildDefsFromTemplates` calls each `StepTemplate.Build(values)` → `StepDef`.
2. `registerPipeline` partitions defs into `app.panels[pid]` by `Panel`.
3. `executeStartFromWizard` (or `executeStartWithResume` on session restore) launches goroutines:
   - Deps with `WaitFor` block on a channel until each dependency fires `stepActivateMsg`.
   - Each goroutine calls `step.Step.Start(ctx, instanceName)`.
   - On return: sends `stepDoneMsg{ok: true/false}`, calls `OnReady` if set.
4. `step.WatchStep` runs in a separate goroutine for each step with a log file.

`StepDef.meta.waitFor` is a `[]string` of step IDs. `topoSortSteps` does Kahn's algorithm to order execution so the Commands panel renders deps in dependency order.

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

## Adding a new step template

1. Implement `step.Step` in a new file (or in `builtins/`).
2. Write a `StepTemplate` constructor function with `Fields`, `Panel`, `Label`, `WaitFor`, and `Build`. Pass `StatePath: config.DefaultStatePath()` when constructing the step so it can access state.json.
3. If the step needs resume customization, implement `step.Resumer`. Long-running processes should save their PID via `config.SaveStepData` and check liveness in `Resume()`. One-shot steps (steps that complete and leave no process running) should return `nil` from `Resume()` unconditionally.
4. If it sends output directly (no log file), have `LogPath` return `""` and send `step.LineMsg` or `step.SetMsg` via `step.Send`.
5. Register the template in `Config.Steps`.

## Adding a custom REPL command

Add a `CommandSpec` to `StepTemplate.Commands`:

```go
Commands: []CommandSpec{{
    Name:    "rebuild",
    Help:    "trigger a rebuild",
    Handler: func(args []string, instanceName string, values WizardValues) error { ... },
}},
```

Built-in names (`help`, `start`, `stop`, `restart`, `logs`, `status`, `test`, `theme`) are reserved — `validateTemplates` will reject conflicts.

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

## Style rules

- `appState` owns data; `viewState` owns display. Don't store computed/transient display state in `appState`.
- All log ingestion goes through `Update` message dispatch — no direct viewport mutation from goroutines.
- Animations are driven by `tickMsg` advancing `flipProgress` / `fullscreenProgress` by a fixed step per frame; the viewport resize happens when the animation settles.
- Buffer cap is `maxBufLines = 5000`. `appendLine` enforces this.
