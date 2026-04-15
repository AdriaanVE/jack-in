package domain

import "testing"

func TestIsAgentType(t *testing.T) {
	for _, a := range AgentNames {
		if !IsAgentType(string(a)) {
			t.Errorf("IsAgentType(%q) = false, want true", a)
		}
	}
	if IsAgentType("gpt") {
		t.Error("IsAgentType(gpt) = true, want false")
	}
	if IsAgentType("") {
		t.Error("IsAgentType('') = true, want false")
	}
}

func TestIsApprovalMode(t *testing.T) {
	for _, m := range []string{"manual", "auto", "yolo"} {
		if !IsApprovalMode(m) {
			t.Errorf("IsApprovalMode(%q) = false, want true", m)
		}
	}
	if IsApprovalMode("unknown") {
		t.Error("IsApprovalMode(unknown) = true, want false")
	}
	if IsApprovalMode("") {
		t.Error("IsApprovalMode('') = true, want false")
	}
}

func TestIsIdle(t *testing.T) {
	idle := WorkerState{Name: "w1"}
	if !idle.IsIdle() {
		t.Error("expected idle")
	}
	busy := WorkerState{Name: "w1", CurrentTask: "task-1"}
	if busy.IsIdle() {
		t.Error("expected not idle")
	}
}

func TestStateLabelBlocked(t *testing.T) {
	tests := []struct {
		name  string
		state WorkerState
		want  string
	}{
		{
			name:  "idle worker",
			state: WorkerState{Name: "w1"},
			want:  LabelIdle,
		},
		{
			name:  "working worker",
			state: WorkerState{Name: "w1", CurrentTask: "task-1"},
			want:  LabelWorking,
		},
		{
			name:  "blocked by review",
			state: WorkerState{Name: "w1", ReviewTaskID: "task-1"},
			want:  LabelBlocked,
		},
		{
			name:  "stuck takes priority over working",
			state: WorkerState{Name: "w1", CurrentTask: "task-1", EscalatedToUser: true},
			want:  LabelStuck,
		},
		{
			name:  "working takes priority over blocked",
			state: WorkerState{Name: "w1", CurrentTask: "task-1", ReviewTaskID: "task-2"},
			want:  LabelWorking,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.state.StateLabel()
			if got != tt.want {
				t.Errorf("StateLabel() = %q, want %q", got, tt.want)
			}
		})
	}
}
