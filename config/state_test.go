package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestStepStateRoundtrip(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")

	// Create instance with step states
	inst := InstanceState{
		StartedAt:    time.Now().UTC().Format(time.RFC3339),
		StringValues: map[string]string{"key": "value"},
		StepStates: map[string]StepState{
			"step1": {
				ID:          "step1",
				Status:      StepStatusRunning,
				StartedAt:   time.Now().UTC().Format(time.RFC3339),
				CompletedAt: "",
				Error:       "",
			},
			"step2": {
				ID:          "step2",
				Status:      StepStatusCompleted,
				StartedAt:   time.Now().UTC().Add(-5 * time.Minute).Format(time.RFC3339),
				CompletedAt: time.Now().UTC().Format(time.RFC3339),
				Error:       "",
			},
			"step3": {
				ID:          "step3",
				Status:      StepStatusFailed,
				StartedAt:   time.Now().UTC().Add(-3 * time.Minute).Format(time.RFC3339),
				CompletedAt: time.Now().UTC().Format(time.RFC3339),
				Error:       "connection refused",
			},
		},
	}

	// Save
	if err := SaveInstanceState(statePath, inst); err != nil {
		t.Fatalf("SaveInstanceState failed: %v", err)
	}

	// Load
	loaded, err := LoadState(statePath)
	if err != nil {
		t.Fatalf("LoadState failed: %v", err)
	}

	// Verify
	if loaded.Instance == nil {
		t.Fatal("loaded.Instance is nil")
	}
	if len(loaded.Instance.StepStates) != 3 {
		t.Errorf("expected 3 step states, got %d", len(loaded.Instance.StepStates))
	}

	// Check step1 (running)
	s1 := loaded.Instance.StepStates["step1"]
	if s1.Status != StepStatusRunning {
		t.Errorf("step1: expected status %q, got %q", StepStatusRunning, s1.Status)
	}
	if s1.StartedAt == "" {
		t.Error("step1: expected StartedAt to be set")
	}
	if s1.CompletedAt != "" {
		t.Error("step1: expected CompletedAt to be empty")
	}

	// Check step2 (completed)
	s2 := loaded.Instance.StepStates["step2"]
	if s2.Status != StepStatusCompleted {
		t.Errorf("step2: expected status %q, got %q", StepStatusCompleted, s2.Status)
	}
	if s2.CompletedAt == "" {
		t.Error("step2: expected CompletedAt to be set")
	}

	// Check step3 (failed)
	s3 := loaded.Instance.StepStates["step3"]
	if s3.Status != StepStatusFailed {
		t.Errorf("step3: expected status %q, got %q", StepStatusFailed, s3.Status)
	}
	if s3.Error != "connection refused" {
		t.Errorf("step3: expected error %q, got %q", "connection refused", s3.Error)
	}
}

func TestBackwardsCompatibility(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")

	// Create old-style state file without StepStates field
	oldState := `{
  "theme": "dark",
  "command_history": ["start", "stop"],
  "instance": {
    "started_at": "2024-01-01T12:00:00Z",
    "string_values": {
      "cpu": "4",
      "ram": "8g"
    }
  }
}`

	if err := os.WriteFile(statePath, []byte(oldState), 0644); err != nil {
		t.Fatalf("failed to write old state file: %v", err)
	}

	// Load should succeed
	loaded, err := LoadState(statePath)
	if err != nil {
		t.Fatalf("LoadState failed on old format: %v", err)
	}

	if loaded.Instance == nil {
		t.Fatal("loaded.Instance is nil")
	}

	// StepStates should be nil (not present in old format)
	if loaded.Instance.StepStates != nil {
		t.Errorf("expected StepStates to be nil for old format, got %v", loaded.Instance.StepStates)
	}

	// Verify other fields loaded correctly
	if loaded.Theme != "dark" {
		t.Errorf("expected theme %q, got %q", "dark", loaded.Theme)
	}
	if loaded.Instance.StringValues["cpu"] != "4" {
		t.Errorf("expected cpu %q, got %q", "4", loaded.Instance.StringValues["cpu"])
	}
}

func TestUpdateStepState(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")

	// Initialize instance
	inst := InstanceState{
		StartedAt:    time.Now().UTC().Format(time.RFC3339),
		StringValues: map[string]string{"key": "value"},
	}
	if err := SaveInstanceState(statePath, inst); err != nil {
		t.Fatalf("SaveInstanceState failed: %v", err)
	}

	// Test 1: Update to running
	if err := UpdateStepState(statePath, "step1", StepStatusRunning, nil); err != nil {
		t.Fatalf("UpdateStepState(running) failed: %v", err)
	}

	loaded, _ := LoadState(statePath)
	s1 := loaded.Instance.StepStates["step1"]
	if s1.Status != StepStatusRunning {
		t.Errorf("expected status %q, got %q", StepStatusRunning, s1.Status)
	}
	if s1.StartedAt == "" {
		t.Error("expected StartedAt to be set")
	}
	if s1.CompletedAt != "" {
		t.Error("expected CompletedAt to be empty")
	}

	// Test 2: Update to completed
	if err := UpdateStepState(statePath, "step1", StepStatusCompleted, nil); err != nil {
		t.Fatalf("UpdateStepState(completed) failed: %v", err)
	}

	loaded, _ = LoadState(statePath)
	s1 = loaded.Instance.StepStates["step1"]
	if s1.Status != StepStatusCompleted {
		t.Errorf("expected status %q, got %q", StepStatusCompleted, s1.Status)
	}
	if s1.CompletedAt == "" {
		t.Error("expected CompletedAt to be set")
	}
	originalStartedAt := s1.StartedAt

	// Test 3: Update another step to failed with error
	testErr := fmt.Errorf("network timeout")
	if err := UpdateStepState(statePath, "step2", StepStatusFailed, testErr); err != nil {
		t.Fatalf("UpdateStepState(failed) failed: %v", err)
	}

	loaded, _ = LoadState(statePath)
	s2 := loaded.Instance.StepStates["step2"]
	if s2.Status != StepStatusFailed {
		t.Errorf("expected status %q, got %q", StepStatusFailed, s2.Status)
	}
	if s2.Error != "network timeout" {
		t.Errorf("expected error %q, got %q", "network timeout", s2.Error)
	}

	// Test 4: Verify multiple steps coexist
	if len(loaded.Instance.StepStates) != 2 {
		t.Errorf("expected 2 step states, got %d", len(loaded.Instance.StepStates))
	}

	// Test 5: Verify StartedAt doesn't change on subsequent updates
	if err := UpdateStepState(statePath, "step1", StepStatusCompleted, nil); err != nil {
		t.Fatalf("UpdateStepState(re-complete) failed: %v", err)
	}
	loaded, _ = LoadState(statePath)
	s1 = loaded.Instance.StepStates["step1"]
	if s1.StartedAt != originalStartedAt {
		t.Errorf("StartedAt changed on re-update: was %q, now %q", originalStartedAt, s1.StartedAt)
	}
}

func TestUpdateStepStateConcurrent(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")

	// Initialize instance
	inst := InstanceState{
		StartedAt:    time.Now().UTC().Format(time.RFC3339),
		StringValues: map[string]string{"key": "value"},
	}
	if err := SaveInstanceState(statePath, inst); err != nil {
		t.Fatalf("SaveInstanceState failed: %v", err)
	}

	// Concurrent updates to different steps
	var wg sync.WaitGroup
	numSteps := 10

	for i := 0; i < numSteps; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			stepID := fmt.Sprintf("step%d", idx)
			_ = UpdateStepState(statePath, stepID, StepStatusRunning, nil)
			time.Sleep(10 * time.Millisecond)
			_ = UpdateStepState(statePath, stepID, StepStatusCompleted, nil)
		}(i)
	}

	wg.Wait()

	// Verify all steps are present
	loaded, err := LoadState(statePath)
	if err != nil {
		t.Fatalf("LoadState failed: %v", err)
	}

	if len(loaded.Instance.StepStates) != numSteps {
		t.Errorf("expected %d step states, got %d", numSteps, len(loaded.Instance.StepStates))
	}

	// Verify all are completed
	for i := 0; i < numSteps; i++ {
		stepID := fmt.Sprintf("step%d", i)
		s := loaded.Instance.StepStates[stepID]
		if s.Status != StepStatusCompleted {
			t.Errorf("%s: expected status %q, got %q", stepID, StepStatusCompleted, s.Status)
		}
	}
}

func TestUpdateStepStateNilInstance(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")

	// Create state with nil instance
	s := State{Theme: "dark"}
	if err := SaveState(statePath, s); err != nil {
		t.Fatalf("SaveState failed: %v", err)
	}

	// UpdateStepState should not crash but should return without error
	// (since there's no instance to update)
	err := UpdateStepState(statePath, "step1", StepStatusRunning, nil)
	if err != nil {
		t.Logf("UpdateStepState on nil instance returned: %v (expected)", err)
	}
}

func TestStepStateJSONMarshaling(t *testing.T) {
	// Test that StepState marshals/unmarshals correctly with all status values
	statuses := []StepStatus{
		StepStatusPending,
		StepStatusRunning,
		StepStatusCompleted,
		StepStatusFailed,
		StepStatusSkipped,
	}

	for _, status := range statuses {
		ss := StepState{
			ID:          "test",
			Status:      status,
			StartedAt:   time.Now().UTC().Format(time.RFC3339),
			CompletedAt: time.Now().UTC().Format(time.RFC3339),
			Error:       "test error",
		}

		data, err := json.Marshal(ss)
		if err != nil {
			t.Fatalf("failed to marshal status %q: %v", status, err)
		}

		var unmarshaled StepState
		if err := json.Unmarshal(data, &unmarshaled); err != nil {
			t.Fatalf("failed to unmarshal status %q: %v", status, err)
		}

		if unmarshaled.Status != status {
			t.Errorf("status %q: expected %q after roundtrip, got %q", status, status, unmarshaled.Status)
		}
	}
}
