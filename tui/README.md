# TUI Package Organization

## Package Structure

The `tui` package is organized into focused files based on responsibility:

### Core Files
- **`tui.go`** (463 lines) - Public API: Config, StepTemplate, FieldSpec, WizardValues, etc.
- **`model.go`** (284 lines) - Core Bubbletea model and state
- **`update.go`** (527 lines) - Bubbletea Update() message handler
- **`messages.go`** (59 lines) - Message type definitions

### Command Handling
- **`commands.go`** (372 lines) - Command dispatch and handlers
- **`pipeline.go`** (230 lines) - Pipeline building and validation
- **`execution.go`** (389 lines) - Pipeline execution logic
- **`debug.go`** (106 lines) - Debug helpers and clipboard utilities

### UI & Rendering
- **`view.go`** (1120 lines) - Main View() and rendering logic
- **`wizard.go`** (394 lines) - Wizard state and logic
- **`wizard_keys.go`** (243 lines) - Wizard keyboard input handling
- **`themes.go`** (74 lines) - Theme definitions
- **`styles.go`** (45 lines) - Style helpers

### Panel Management
- **`panel_steps.go`** (103 lines) - Step panel coordination
- **`history.go`** (40 lines) - Command history
- **`step.go`** (19 lines) - Step utilities

### Testing
- **`pipeline_test.go`** (947 lines) - Pipeline tests
- **`model_test.go`** (331 lines) - Model tests
- **`instance_test.go`** (37 lines) - Instance tests

## File Responsibilities

### Commands Flow
1. **`commands.go`** - Receives user input, dispatches to handlers
2. **`pipeline.go`** - Builds StepDef list from templates
3. **`execution.go`** - Executes steps with dependency management

### Data Flow
```
User Input → commands.go → dispatchCommand()
                          ↓
                    start command
                          ↓
           wizard.go → buildValues()
                          ↓
          pipeline.go → buildDefsFromTemplates()
                          ↓
         execution.go → executeStart() / executeStartWithResume()
                          ↓
                    steps execute
                          ↓
                  update.go handles messages
                          ↓
                   view.go renders UI
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
