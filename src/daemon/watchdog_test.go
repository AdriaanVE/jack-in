package daemon

import (
	"testing"
	"time"

	"github.com/AdriaanVE/jack-in/src/marker"
)

func TestHasCompletionMarkerInPane(t *testing.T) {
	tests := []struct {
		name    string
		content string
		taskID  string
		want    bool
	}{
		{
			name:    "marker present",
			content: "output\nJACKIN_TASK_COMPLETE:task-1\n",
			taskID:  "task-1",
			want:    true,
		},
		{
			name:    "marker absent",
			content: "output\nno marker here\n",
			taskID:  "task-1",
			want:    false,
		},
		{
			name:    "wrong task",
			content: "JACKIN_TASK_COMPLETE:task-2\n",
			taskID:  "task-1",
			want:    false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := marker.HasCompletionMarker(tt.content, tt.taskID); got != tt.want {
				t.Errorf("HasCompletionMarker() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestOrchestratorState_Reset(t *testing.T) {
	s := &OrchestratorState{
		LastPaneSnapshot: "snapshot",
		LastSnapshotAt:   12345,
		LLMEvalCount:     2,
		EscalatedToUser:  true,
		StartedAt:        time.Now(),
	}
	s.reset()
	if s.LastPaneSnapshot != "" || s.LastSnapshotAt != 0 || s.LLMEvalCount != 0 || s.EscalatedToUser {
		t.Error("reset did not clear all fields")
	}
	// StartedAt should be preserved
	if s.StartedAt.IsZero() {
		t.Error("reset should not clear StartedAt")
	}
}

func TestFirstNonEmpty(t *testing.T) {
	if got := firstNonEmpty("", "", "c"); got != "c" {
		t.Errorf("firstNonEmpty = %q, want %q", got, "c")
	}
	if got := firstNonEmpty("a", "b"); got != "a" {
		t.Errorf("firstNonEmpty = %q, want %q", got, "a")
	}
	if got := firstNonEmpty("", ""); got != "" {
		t.Errorf("firstNonEmpty = %q, want empty", got)
	}
}
