# its_tui

A Go library for building terminal dashboards that manage long-running development processes. It
provides a fixed 2×2 panel layout, a wizard-driven start flow with saved state, a REPL command
panel, and built-in support for minikube, skaffold, kubectl, and micro-frontend workflows.

Built on [Bubble Tea](https://github.com/charmbracelet/bubbletea) and [Lip Gloss](https://github.com/charmbracelet/lipgloss).

## Installation

```
go get github.com/thompsonja/its_tui
```

## Layout

```
┌──────────────────────┬──────────────────────┐
│  Top Left            │  Top Right           │
│  (e.g. Minikube /    │  (e.g. Skaffold)     │
│   kubectl pods)      │                      │
├──────────────────────┼──────────────────────┤
│  Commands            │  Bottom Right        │
│  (REPL + wizard)     │  (e.g. MFE)          │
└──────────────────────┴──────────────────────┘
```

Three panels stream process output; the Commands panel is a REPL with an animated card-flip to the
start wizard. Panels are addressed by `PanelTopLeft`, `PanelTopRight`, and `PanelBottomRight`.

## Quick Start

```go
package main

import (
    "fmt"
    "os"

    "github.com/thompsonja/its_tui/tui"
)

func main() {
    cfg := tui.Config{
        InstanceName: "My Service",
        Steps: []tui.StepTemplate{
            tui.MinikubeTemplate(),
            tui.KubectlTemplate(),
            tui.SkaffoldTemplate(mySkaffoldGenerator, mySystemsFunc),
            tui.MFETemplate([]string{"checkout-mfe", "user-mfe"}, nil),
        },
    }

    if err := tui.Run(cfg); err != nil {
        fmt.Fprintln(os.Stderr, err)
        os.Exit(1)
    }
}
```

`tui.Run` blocks until the user quits with `ctrl+c` or `q`.

## Configuration

### `Config`

```go
type Config struct {
    // Display name shown in the top bar. Defaults to "Integration Test Suite".
    InstanceName string

    // Ordered list of step templates that define the pipeline.
    Steps []StepTemplate

    // Optional test suites runnable via the `test` REPL command.
    // When non-empty, a virtual "Tests" tab appears on the BottomRight panel.
    Tests []TestTemplate

    // Optional callback that produces the top bar text each frame.
    // instanceName is empty when no instance is running.
    // Defaults to showing instanceName, or "no instance running".
    StatusLine func(instanceName string) string
}
```

### `StepTemplate`

`StepTemplate` is the main building block. Each template contributes wizard fields, a target
panel, and a factory that constructs the `Step` to run.

```go
type StepTemplate struct {
    // Unique identifier. Must match the value returned by the built Step's ID().
    // Required when using WaitFor.
    ID string

    // Wizard fields contributed by this template.
    Fields []FieldSpec

    // Output panel for this step's log stream.
    Panel PanelID

    // Label shown in the commands panel tracker.
    Label string

    // LabelFunc overrides Label using the final wizard values.
    LabelFunc func(WizardValues) string

    // WaitFor is a list of IDs of steps that must be ready before this one starts.
    WaitFor []string

    // AutoActivate switches the panel view to this step when it is activated.
    AutoActivate bool

    // Hidden suppresses this step from the commands panel tracker.
    // Useful for "config-only" steps that contribute wizard fields but run no process.
    Hidden bool

    // OnReady is called in a goroutine when Start returns nil.
    // statePath is the path to the TUI state file (~/.tui/state.json).
    OnReady func(statePath string)

    // Build constructs the Step. Returning (nil, nil) skips the step entirely.
    Build func(WizardValues) (Step, error)

    // StopFunc is called during `stop` to clean up this step's resources.
    // Steps are stopped in reverse template order.
    StopFunc func(ctx context.Context, instanceName string)

    // StopLabel is shown in the commands panel while StopFunc runs.
    // Defaults to "stopping <Label>".
    StopLabel string
}
```

### `Step` Interface

Implement `Step` to integrate any process:

```go
type Step interface {
    // Unique key for message routing and WaitFor references.
    ID() string

    // Path to the log file tailed by the panel.
    // Return "" to push output directly via step.Send (e.g. polling steps).
    LogPath(instanceName string) string

    // Start the process. Block until it is running/ready or has failed.
    // ctx is cancelled when the instance is stopped or restarted.
    Start(ctx context.Context, instanceName string) error
}
```

**Log-file steps** write to a file; the TUI tails it with `tail -F`. **Direct-send steps** call
`step.Send(step.LineMsg{...})` or `step.Send(step.SetMsg{...})` from goroutines, and return `""`
from `LogPath`.

```go
// Example: a custom step that polls an HTTP endpoint
type HealthStep struct{}

func (s *HealthStep) ID() string                            { return "health" }
func (s *HealthStep) LogPath(name string) string            { return "" } // direct-send
func (s *HealthStep) Start(ctx context.Context, name string) error {
    go func() {
        for {
            select {
            case <-ctx.Done():
                return
            case <-time.After(5 * time.Second):
                status := checkHealth()
                step.Send(step.SetMsg{ID: "health", Content: []string{status}})
            }
        }
    }()
    return nil
}
```

### Wizard Fields

Fields are declared in `StepTemplate.Fields` and rendered in the start wizard in the order they
appear across all templates.

```go
type FieldSpec struct {
    ID          string                      // key in WizardValues
    Label       string                      // display text
    Kind        FieldKind
    OptionsFunc func(WizardValues) []string // provides choices for Select / SingleSelect / MultiSelect
    SystemsFunc func(WizardValues) []System // provides hierarchy for SystemSelect
    Default     int                         // for Select: index of the default option
}
```

Use `StaticOptions` and `StaticSystems` helper functions for fields with fixed choices:

```go
tui.StaticOptions("dev", "staging", "prod")  // returns an OptionsFunc
tui.StaticSystems(systems...)                 // returns a SystemsFunc
```

| `FieldKind`            | Interaction                                          | Read via           |
|------------------------|------------------------------------------------------|--------------------|
| `FieldKindSelect`      | Horizontal `←→` selector from a fixed list           | `v.String(id)`     |
| `FieldKindSingleSelect`| Searchable single-item picker                        | `v.String(id)`     |
| `FieldKindMultiSelect` | Searchable multi-item picker                         | `v.Strings(id)`    |
| `FieldKindSystemSelect`| Hierarchical system → component tree, multi-select   | `v.Strings(id)`    |
| `FieldKindText`        | Free-text input                                      | `v.String(id)`     |

```go
// Select: arrow keys choose from a fixed list
{ID: "env", Label: "Environment", Kind: tui.FieldKindSelect,
    OptionsFunc: tui.StaticOptions("dev", "staging", "prod"), Default: 0}

// SingleSelect: type to filter, Enter to confirm
{ID: "region", Label: "Region", Kind: tui.FieldKindSingleSelect,
    OptionsFunc: tui.StaticOptions("us-east-1", "eu-west-1", "ap-southeast-1")}

// MultiSelect: type to filter, Enter to toggle
{ID: "features", Label: "Features", Kind: tui.FieldKindMultiSelect,
    OptionsFunc: tui.StaticOptions("auth", "payments", "notifications")}

// SystemSelect: hierarchical tree; toggle whole systems or individual components
{ID: "components", Label: "Components", Kind: tui.FieldKindSystemSelect,
    SystemsFunc: tui.StaticSystems([]tui.System{
        {Name: "checkout", Components: []tui.Component{
            {Name: "checkout-backend"},
            {Name: "checkout-bff"},
        }},
    }...)}

// Text: free input, e.g. a namespace override
{ID: "namespace", Label: "Namespace", Kind: tui.FieldKindText}
```

### Dynamic Field Options

All fields use `OptionsFunc` or `SystemsFunc` to provide their choices. For static lists, use
the `StaticOptions` or `StaticSystems` helpers. For dynamic behavior, provide your own function
that **re-evaluates reactively** after every field change. This is useful when a discovery step
queries the cluster and stores results that the wizard then reads, or when one field's selection
should drive the options shown in another field.

```go
var systemsForEnv = map[string][]tui.System{
    "dev":  devSystems,
    "test": testSystems,
}

tui.StepTemplate{
    ID:    "skaffold",
    Panel: tui.PanelTopRight,
    Fields: []tui.FieldSpec{
        {ID: "env", Label: "Environment", Kind: tui.FieldKindSelect,
            OptionsFunc: tui.StaticOptions("dev", "test"), Default: 0},
        {
            ID:    "components",
            Label: "Components",
            Kind:  tui.FieldKindSystemSelect,
            SystemsFunc: func(v tui.WizardValues) []tui.System {
                env := v.String("env")
                return systemsForEnv[env] // map populated by a discovery step
            },
        },
        {ID: "mode", Label: "Mode", Kind: tui.FieldKindSelect,
            OptionsFunc: tui.StaticOptions("dev", "run", "debug")},
    },
    Build: buildSkaffold,
}
```

The same pattern applies to `OptionsFunc` for all field kinds that accept options:

```go
{
    ID:          "namespace",
    Label:       "Namespace",
    Kind:        tui.FieldKindSingleSelect,
    OptionsFunc: func(v tui.WizardValues) []string {
        return listNamespacesForEnv(v.String("env")) // cached by a step
    },
}
```

The functions are called **at wizard-open** (with the initial values) and **after every field
change**, synchronously. They must be fast — a read of a variable or small slice already
populated by a running step, not a blocking network call. When a dependent field's options change,
any selections that no longer appear in the new list are automatically dropped.

### `WizardValues`

`WizardValues` is passed to every `Build` and `TestTemplate.Build` function:

```go
func (v WizardValues) String(id string) string    // single value (Select, SingleSelect, Text)
func (v WizardValues) Strings(id string) []string // slice value (MultiSelect, SystemSelect)
```

Fields not set by the user return `""` / `nil` — always apply a default:

```go
Build: func(v tui.WizardValues) (tui.Step, error) {
    env := v.String("env")
    if env == "" {
        env = "dev"
    }
    components := v.Strings("components") // may be nil if user skipped
    return &MyStep{Env: env, Components: components}, nil
},
```

For tests, use `NewWizardValues` to construct values directly:

```go
vals := tui.NewWizardValues(
    map[string]string{"env": "dev", "cpu": "4"},
    map[string][]string{"components": {"checkout-backend"}},
)
```

## Built-in Templates

### `MinikubeTemplate()`

Starts a minikube cluster. Contributes `cpu` (Select: 2/4/8/16, default 4) and `ram` (Select:
2g/4g/8g/16g, default 4g) wizard fields. Provides a `StopFunc` that runs `minikube delete` on
stop. Routes output to `PanelTopLeft`.

```go
tui.MinikubeTemplate()
```

### `KubectlTemplate()`

Polls `kubectl get pods` every 5 seconds and replaces the panel content with the current pod
table. Hidden from the step tracker. Waits for `"minikube"`, auto-activates to replace the
minikube log view, and calls `tui.MarkActive` when ready. Routes output to `PanelTopLeft`.

```go
tui.KubectlTemplate()
// WaitFor: []string{"minikube"}, AutoActivate: true, Hidden: true
```

### `SkaffoldTemplate(generate, systemsfunc)`

Runs `skaffold dev`, `run`, or `debug`. Contributes `components` (SystemSelect) and `mode`
(Select: dev/run/debug) wizard fields. Waits for `"minikube"`. Routes output to `PanelTopRight`.

```go
tui.SkaffoldTemplate(
    func(v tui.WizardValues) (path string, profiles []string, err error) {
        comps := v.Strings("components")
        mode  := v.String("mode")
        // Generate skaffold.yaml; return its path and any profiles to activate.
        // Return ("", nil, nil) to skip skaffold entirely.
        return generateSkaffold(comps, mode)
    },
    func(v tui.WizardValues) []tui.System {
        return []tui.System{
            {Name: "backend", Components: []tui.Component{
                {Name: "api-service"},
                {Name: "worker"},
            }},
        }
    },
)
```

The `systemsfunc` parameter enables dynamic system selection based on other wizard fields. For
example, to show different components per environment:

```go
var systemsByEnv = map[string][]tui.System{
    "dev": {
        {Name: "checkout", Components: []tui.Component{{Name: "checkout-backend"}}},
    },
    "prod": {
        {Name: "checkout", Components: []tui.Component{{Name: "checkout-backend"}}},
        {Name: "analytics", Components: []tui.Component{{Name: "analytics-service"}}},
    },
}

tui.SkaffoldTemplate(
    generateFunc,
    func(v tui.WizardValues) []tui.System {
        env := v.String("env")
        return systemsByEnv[env] // re-evaluated when "env" field changes
    },
)
```

### `MFETemplate(mfes, run)`

Runs a micro-frontend process. Contributes an `mfe` (SingleSelect) wizard field. Routes output
to `PanelBottomRight`. If `run` is nil, defaults to `npm start` in a directory named after the
selected MFE.

```go
tui.MFETemplate(
    []string{"checkout-mfe", "user-mfe"},
    func(name string, v tui.WizardValues) tui.MFECommand {
        port := v.String("api_port")
        return tui.MFECommand{
            Cmd: "node",
            Args: []string{"index.js"},
            Dir: filepath.Join("frontend", name),
            Env: map[string]string{"API_BASE": "http://localhost:" + port},
        }
    },
)
```

## Step Dependencies

Use `ID` and `WaitFor` to sequence steps. The dependent step starts only after its dependency's
`Start` returns `nil`.

```go
Steps: []tui.StepTemplate{
    {ID: "infra",  Label: "Infrastructure", Panel: tui.PanelTopLeft,  Build: buildInfra},
    {ID: "app",    Label: "Application",    Panel: tui.PanelTopRight,
        WaitFor: []string{"infra"}, Build: buildApp},
    {ID: "worker", Label: "Worker",         Panel: tui.PanelBottomRight,
        WaitFor: []string{"infra"}, Build: buildWorker},
},
```

Steps with no `WaitFor` start in parallel. The pending steps display `"(waiting for <id>)"` in
the tracker until unblocked.

## Config-Only Steps

A step that returns `(nil, nil)` from `Build` is silently skipped — no process is started. This
is useful for "wizard-only" templates that contribute fields consumed by other steps:

```go
{
    ID:     "config",
    Panel:  tui.PanelTopLeft,
    Hidden: true,
    Fields: []tui.FieldSpec{
        {ID: "namespace", Label: "Namespace", Kind: tui.FieldKindText},
        {ID: "env",       Label: "Env",       Kind: tui.FieldKindSelect,
            Options: []string{"dev", "prod"}, Default: 0},
    },
    Build: func(v tui.WizardValues) (tui.Step, error) { return nil, nil },
},
```

## Test Suites

`TestTemplate` defines a command runnable via the `test` REPL command. Results stream to a
virtual "Tests" tab on the BottomRight panel.

```go
Tests: []tui.TestTemplate{
    {
        Label: "Integration",
        Build: func(v tui.WizardValues) (tui.TestCommand, error) {
            return tui.TestCommand{
                Cmd:  "go",
                Args: []string{"test", "-v", "-count=1", "./integration/..."},
                Dir:  ".",
                Env:  map[string]string{"ENV": v.String("env")},
            }, nil
        },
    },
    {
        Label: "E2E",
        Build: func(v tui.WizardValues) (tui.TestCommand, error) {
            return tui.TestCommand{
                Cmd:  "npx",
                Args: []string{"playwright", "test"},
                Dir:  "e2e",
            }, nil
        },
    },
},
```

With multiple test suites, run a specific one with `test Integration` or `test E2E`. With a
single suite, `test` alone suffices.

## Custom Status Line

Override the top bar text, for example to show the active environment or cluster health:

```go
cfg := tui.Config{
    StatusLine: func(instanceName string) string {
        if instanceName == "" {
            return "no instance running"
        }
        cluster := currentCluster() // your own logic
        return instanceName + "  ·  " + cluster
    },
}
```

## REPL Commands

The Commands panel accepts typed commands:

| Command              | Description |
|----------------------|-------------|
| `start`              | Open the configuration wizard; starts the pipeline on confirm |
| `stop`               | Cancel all running steps, kill MFE process group, run StopFuncs in reverse order |
| `restart <step-id>`  | Cancel and re-launch a single step by ID |
| `logs`               | Print the log file path for each running step |
| `test [label]`       | Run a test suite (label required when multiple suites are configured) |
| `theme <name>`       | Switch color theme; persisted across sessions |
| `help`               | Open the keyboard reference card |

Command history is navigated with `↑` / `↓` in the Commands panel.

## Keyboard Shortcuts

| Key              | Action |
|------------------|--------|
| `Tab`            | Cycle focus forward through panels |
| `Shift+Tab`      | Cycle focus backward |
| `ctrl+f`         | Toggle fullscreen on the focused panel |
| `Esc`            | Exit fullscreen / close wizard / close help |
| `t`              | Cycle tabs within a focused content panel |
| `/`              | Enter search mode in a content panel |
| `Esc` (search)   | Exit search mode |
| `↑` / `↓`        | Scroll panel content; navigate command history (Commands panel) |
| `c`              | Copy VSCode launch config to clipboard (Debug tab) |
| `ctrl+c`         | Quit |

## Themes

Four built-in color themes, switchable at runtime and persisted to `~/.tui/state.json`:

| Name          | Description              |
|---------------|--------------------------|
| `dark`        | Default; cyan-green on dark grey |
| `light`       | Dark teal on off-white   |
| `dracula`     | Pink on dark purple      |
| `catppuccin`  | Blue Sapphire on dark    |

```
theme dracula
theme catppuccin
```

## Session Persistence

State is persisted to `~/.tui/state.json` between sessions:

- **Wizard selections** — `start` pre-populates the wizard from the last run's values
- **Forwarded ports** — restored to the Ports tab on the TopRight panel
- **Debug ports** — restored to the Debug tab
- **Active instance** — if an instance was running when the TUI was closed, it is automatically
  reconnected (log tailing resumes, polling steps restart)
- **Theme** — last theme persisted across sessions

The state path can be read with `tui.DefaultStatePath()`.

## Debug and Port Forwarding

When skaffold port-forwards a service, it emits `step.DebugPortMsg` messages. The TUI
automatically:

- Adds a virtual **Ports** tab to the TopRight panel listing all forwarded addresses
- Adds a virtual **Debug** tab when debug sessions are detected (dlv, jvm, node, ptvsd)
- Generates a VSCode `launch.json` snippet on the Debug tab; press `c` to copy it to the clipboard

These messages are sent by `SkaffoldStep` automatically — no configuration required.

## Full Example

```go
package main

import (
    "fmt"
    "os"
    "path/filepath"

    "github.com/thompsonja/its_tui/tui"
)

func main() {
    cfg := tui.Config{
        InstanceName: "Platform",
        StatusLine: func(name string) string {
            if name == "" {
                return "no instance running — type: start"
            }
            return "● " + name
        },
        Steps: []tui.StepTemplate{
            tui.MinikubeTemplate(),
            tui.KubectlTemplate(),

            // Config-only step: contributes "env" field used by skaffold + tests.
            {
                ID:     "env",
                Panel:  tui.PanelTopLeft,
                Hidden: true,
                Fields: []tui.FieldSpec{
                    {
                        ID:          "env",
                        Label:       "Environment",
                        Kind:        tui.FieldKindSelect,
                        OptionsFunc: tui.StaticOptions("dev", "test"),
                        Default:     0,
                    },
                },
                Build: func(v tui.WizardValues) (tui.Step, error) { return nil, nil },
            },

            tui.SkaffoldTemplate(
                func(v tui.WizardValues) (string, []string, error) {
                    env := v.String("env")
                    comps := v.Strings("components")
                    path, profiles, err := generateSkaffold(env, comps)
                    return path, profiles, err
                },
                func(v tui.WizardValues) []tui.System {
                    return []tui.System{
                        {
                            Name: "checkout",
                            Components: []tui.Component{
                                {Name: "checkout-backend"},
                                {Name: "checkout-bff"},
                            },
                        },
                        {
                            Name: "user",
                            Components: []tui.Component{
                                {Name: "user-service"},
                                {Name: "user-bff"},
                            },
                        },
                    }
                },
            ),

            tui.MFETemplate(
                []string{"checkout-mfe", "user-mfe"},
                func(name string, v tui.WizardValues) tui.MFECommand {
                    return tui.MFECommand{
                        Cmd:  "npm",
                        Args: []string{"start"},
                        Dir:  filepath.Join("frontend", name),
                        Env:  map[string]string{"NODE_ENV": v.String("env")},
                    }
                },
            ),
        },
        Tests: []tui.TestTemplate{
            {
                Label: "Integration",
                Build: func(v tui.WizardValues) (tui.TestCommand, error) {
                    return tui.TestCommand{
                        Cmd:  "go",
                        Args: []string{"test", "-v", "./integration/..."},
                        Env:  map[string]string{"ENV": v.String("env")},
                    }, nil
                },
            },
        },
    }

    if err := tui.Run(cfg); err != nil {
        fmt.Fprintln(os.Stderr, err)
        os.Exit(1)
    }
}

func generateSkaffold(env string, components []string) (string, []string, error) {
    // Write a skaffold.yaml based on selections and return its path.
    // Return ("", nil, nil) to skip skaffold entirely.
    return "skaffold.yaml", []string{env}, nil
}
```

## Requirements

- Go 1.24+
- A terminal with 256-color support
- `tail` in `$PATH` (used for log file streaming)
- For clipboard copy (`c` on the Debug tab): `wl-copy`, `xclip`, `xsel`, or `pbcopy`
