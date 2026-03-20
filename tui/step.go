package tui

import (
	"context"
	"github.com/thompsonja/its_tui/step"
)

// watchStep tails the step's log file and forwards each line to the step's panel.
// Steps with no log file (LogPath=="") are skipped — they send output themselves.
// Blocks until ctx is cancelled — call it in a goroutine.
func watchStep(ctx context.Context, def StepDef, instanceName string) {
	step.WatchStep(ctx, def.Step, instanceName)
}
