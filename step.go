package tui

import (
	"context"
	"tui/step"
)

// watchStep tails the step's log file and forwards each line to the step's panel.
// Steps with no log file (LogPath=="") are skipped — they send output themselves.
// Blocks until ctx is cancelled — call it in a goroutine.
func watchStep(ctx context.Context, def StepDef, instanceName string) {
	step.WatchStep(ctx, def.Step, instanceName)
}

// resumeStep restarts steps with no log file after a session restore.
// Steps with a log file are already covered by watchStep.
func resumeStep(ctx context.Context, def StepDef, instanceName string) {
	step.ResumeStep(ctx, def.Step, instanceName)
}
