package daemon

import (
	"testing"
	"time"

	"github.com/AdriaanVE/jack-in/src/domain"
)

func TestResetWatchdog(t *testing.T) {
	state := &domain.WorkerState{
		Name:             "w1",
		Agent:            domain.AgentClaude,
		CurrentTask:      "task-1",
		AssignedAt:       time.Now().UnixMilli(),
		LastPaneSnapshot: "snapshot",
		LastSnapshotAt:   12345,
		LLMEvalCount:     2,
		EscalatedToUser:  true,
	}
	resetWatchdog(state)
	if state.LastPaneSnapshot != "" || state.LastSnapshotAt != 0 || state.LLMEvalCount != 0 || state.EscalatedToUser {
		t.Error("resetWatchdog did not clear watchdog fields")
	}
	// Should NOT clear task assignment
	if state.CurrentTask != "task-1" {
		t.Error("resetWatchdog should not clear CurrentTask")
	}
}

func TestClearAssignment(t *testing.T) {
	state := &domain.WorkerState{
		Name:             "w1",
		Agent:            domain.AgentClaude,
		CurrentTask:      "task-1",
		AssignedAt:       time.Now().UnixMilli(),
		ReviewTaskID:     "review-1",
		LastPaneSnapshot: "snap",
		LastSnapshotAt:   999,
		LLMEvalCount:     3,
		EscalatedToUser:  true,
	}
	clearAssignment(state)

	if state.CurrentTask != "" {
		t.Error("clearAssignment should clear CurrentTask")
	}
	if state.AssignedAt != 0 {
		t.Error("clearAssignment should clear AssignedAt")
	}
	if state.LastPaneSnapshot != "" || state.LLMEvalCount != 0 || state.EscalatedToUser {
		t.Error("clearAssignment should reset watchdog fields")
	}
	// Should NOT clear ReviewTaskID -- that's managed separately
	if state.ReviewTaskID != "review-1" {
		t.Error("clearAssignment should not clear ReviewTaskID")
	}
}

func TestMarkComplete(t *testing.T) {
	state := &domain.WorkerState{
		Name:        "w1",
		Agent:       domain.AgentClaude,
		CurrentTask: "task-1",
		AssignedAt:  time.Now().UnixMilli(),
	}
	var idle []*domain.WorkerState
	markComplete(state, &idle)

	if state.CurrentTask != "" {
		t.Error("markComplete should clear CurrentTask")
	}
	if state.AssignedAt != 0 {
		t.Error("markComplete should clear AssignedAt")
	}
	if len(idle) != 1 || idle[0] != state {
		t.Error("markComplete should append to idle workers")
	}
}
