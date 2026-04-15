package marker

import "testing"

func TestCompletionMarker(t *testing.T) {
	got := CompletionMarker("task-123")
	want := "JACKIN_TASK_COMPLETE:task-123"
	if got != want {
		t.Errorf("CompletionMarker = %q, want %q", got, want)
	}
}

func TestStripFormatting(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"JACKIN_TASK_COMPLETE:task-1", "JACKIN_TASK_COMPLETE:task-1"},
		{"`JACKIN_TASK_COMPLETE:task-1`", "JACKIN_TASK_COMPLETE:task-1"},
		{"```JACKIN_TASK_COMPLETE:task-1```", "JACKIN_TASK_COMPLETE:task-1"},
		{"```\nJACKIN_TASK_COMPLETE:task-1", "JACKIN_TASK_COMPLETE:task-1"},
		{"JACKIN_TASK_COMPLETE:task-1```", "JACKIN_TASK_COMPLETE:task-1"},
		{`"JACKIN_TASK_COMPLETE:task-1"`, "JACKIN_TASK_COMPLETE:task-1"},
		{"'JACKIN_TASK_COMPLETE:task-1'", "JACKIN_TASK_COMPLETE:task-1"},
		{"JACKIN_TASK_COMPLETE:task-1.", "JACKIN_TASK_COMPLETE:task-1"},
		{"  JACKIN_TASK_COMPLETE:task-1  ", "JACKIN_TASK_COMPLETE:task-1"},
	}
	for _, tt := range tests {
		got := StripFormatting(tt.input)
		if got != tt.want {
			t.Errorf("StripFormatting(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestHasCompletionMarker(t *testing.T) {
	tests := []struct {
		name   string
		text   string
		taskID string
		want   bool
	}{
		{
			name:   "exact match on own line",
			text:   "some output\nJACKIN_TASK_COMPLETE:task-1\n",
			taskID: "task-1",
			want:   true,
		},
		{
			name:   "wrapped in backticks",
			text:   "output\n`JACKIN_TASK_COMPLETE:task-1`\n",
			taskID: "task-1",
			want:   true,
		},
		{
			name:   "not present",
			text:   "some output\nnothing here\n",
			taskID: "task-1",
			want:   false,
		},
		{
			name:   "wrong task ID",
			text:   "JACKIN_TASK_COMPLETE:task-2\n",
			taskID: "task-1",
			want:   false,
		},
		{
			name:   "marker deep in output beyond 10 lines",
			text:   "JACKIN_TASK_COMPLETE:task-1\n1\n2\n3\n4\n5\n6\n7\n8\n9\n10\n11\n",
			taskID: "task-1",
			want:   false,
		},
		{
			name:   "marker in last 10 non-empty lines",
			text:   "a\nb\nJACKIN_TASK_COMPLETE:task-1\nc\nd\ne\nf\ng\nh\ni\n",
			taskID: "task-1",
			want:   true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := HasCompletionMarker(tt.text, tt.taskID)
			if got != tt.want {
				t.Errorf("HasCompletionMarker() = %v, want %v", got, tt.want)
			}
		})
	}
}
