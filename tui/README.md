# TUI Package Organization

## Package Structure

The `tui` package is organized into focused files based on responsibility:

### Core Files
- **`tui.go`** - Public API: Config, StepTemplate, FieldSpec, WizardValues, etc.
- **`model.go`** - Core Bubbletea model and state
- **`update.go`** - Bubbletea Update() message handler
- **`messages.go`** - Message type definitions
- **`headless.go`** - Headless/CI mode: RunHeadless, HeadlessOptions, event loop
- **`ci_output.go`** - CIFormatter interface, GitHubActionsFormatter, PlainFormatter

### Command Handling
- **`commands.go`** - Command dispatch and handlers
- **`pipeline.go`** - Pipeline building and validation
- **`execution.go`** - Pipeline execution logic
- **`debug.go`** - Debug helpers and clipboard utilities

### UI & Rendering
- **`view.go`** - Main View() and rendering logic
- **`wizard.go`** - Wizard state and logic
- **`wizard_keys.go`** - Wizard keyboard input handling
- **`themes.go`** - Theme definitions
- **`styles.go`** - Style helpers

### Panel Management
- **`panel_steps.go`** - Step panel coordination
- **`history.go`** - Command history
- **`step.go`** - Step utilities

### Testing
- **`pipeline_test.go`** - Pipeline tests
- **`model_test.go`** - Model tests
- **`instance_test.go`** - Instance tests

## File Responsibilities

### Commands Flow
1. **`commands.go`** - Receives user input, dispatches to handlers
2. **`pipeline.go`** - Builds StepDef list from templates
3. **`execution.go`** - Executes steps with dependency management

### Data Flow (Interactive)
```
User Input → commands.go → dispatchCommand()
                          ↓
                    start command
                          ↓
           wizard.go → buildValues()
                          ↓
          pipeline.go → buildDefs()
                          ↓
         execution.go → executeStart() / executeStartWithResume()
                          ↓
                    steps execute
                          ↓
                  update.go handles messages
                          ↓
                   view.go renders UI
```

### Data Flow (Headless/CI)
```
Caller provides WizardValues
            ↓
  headless.go → RunHeadless()
            ↓
  pipeline.go → buildDefs()
            ↓
  headless.go → headlessExecuteSteps()
            ↓
        steps execute
            ↓
  headless.go → runHeadlessEventLoop()
            ↓
  ci_output.go → CIFormatter writes to stdout
            ↓
  (optional) runHeadlessTests()
```

## Guidelines

- **Adding commands**: Add to `dispatchCommand()` in `commands.go`
- **Modifying pipeline logic**: Edit `pipeline.go`
- **Changing execution**: Edit `execution.go`
- **UI changes**: Edit `view.go` or wizard files
- **New messages**: Add to `messages.go`

## Why This Organization?

Files are organized by **responsibility** rather than by **feature**:
- Easier to locate specific functionality
- Clear separation of concerns
- Better for team collaboration (fewer merge conflicts)
- Easier to test individual components
