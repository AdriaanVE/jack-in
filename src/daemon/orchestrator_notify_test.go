package daemon

import (
	"fmt"
	"testing"
)

func TestTruncate(t *testing.T) {
	tests := []struct {
		input string
		max   int
		want  string
	}{
		{"short", 10, "short"},
		{"exactly10!", 10, "exactly10!"},
		{"this is a longer string", 10, "this is..."},
		{"", 5, ""},
		{"abc", 3, "abc"},
		{"abcd", 3, "..."},
	}
	for _, tc := range tests {
		got := truncate(tc.input, tc.max)
		if got != tc.want {
			t.Errorf("truncate(%q, %d) = %q; want %q", tc.input, tc.max, got, tc.want)
		}
	}
}

func TestEventConstants(t *testing.T) {
	// Verify event constants are non-empty and distinct
	events := []string{EventTaskReview, EventAllComplete, EventWorkerStalled, EventWorkerNeedHelp}
	seen := make(map[string]bool)
	for _, e := range events {
		if e == "" {
			t.Error("event constant is empty")
		}
		if seen[e] {
			t.Errorf("duplicate event constant: %s", e)
		}
		seen[e] = true
	}
}

func TestEventFormatting(t *testing.T) {
	tests := []struct {
		name   string
		format func() string
		check  func(string) bool
	}{
		{
			name: "TaskReview",
			format: func() string {
				return fmt.Sprintf("%s task=%s worker=%s summary=%s", EventTaskReview, "task-123", "worker-1", truncate("Fix bug", 50))
			},
			check: func(s string) bool {
				return len(s) > 0 && s[:len(EventTaskReview)] == EventTaskReview
			},
		},
		{
			name: "AllComplete",
			format: func() string {
				return fmt.Sprintf("%s complete=%d rejected=%d", EventAllComplete, 5, 2)
			},
			check: func(s string) bool {
				return len(s) > 0 && s[:len(EventAllComplete)] == EventAllComplete
			},
		},
		{
			name: "WorkerStalled",
			format: func() string {
				return fmt.Sprintf("%s worker=%s task=%s reason=%s", EventWorkerStalled, "worker-1", "task-123", truncate("stuck", 50))
			},
			check: func(s string) bool {
				return len(s) > 0 && s[:len(EventWorkerStalled)] == EventWorkerStalled
			},
		},
		{
			name: "WorkerNeedsHelp",
			format: func() string {
				return fmt.Sprintf("%s worker=%s task=%s", EventWorkerNeedHelp, "worker-1", "task-123")
			},
			check: func(s string) bool {
				return len(s) > 0 && s[:len(EventWorkerNeedHelp)] == EventWorkerNeedHelp
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := tc.format()
			if !tc.check(result) {
				t.Errorf("format check failed for %s: %q", tc.name, result)
			}
		})
	}
}
