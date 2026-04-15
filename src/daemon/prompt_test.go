package daemon

import (
	"os"
	"strings"
	"testing"

	"github.com/AdriaanVE/jack-in/src/domain"
	"github.com/AdriaanVE/jack-in/src/marker"
	"github.com/AdriaanVE/jack-in/src/taskqueue"
)

func TestFormatTaskPrompt_Claude(t *testing.T) {
	task := &taskqueue.Task{
		ID:          "task-1",
		Summary:     "Fix the bug",
		Description: "There is a bug in main.go",
		Files:       []string{"main.go"},
		Acceptance:  []string{"Tests pass"},
	}

	prompt := FormatTaskPrompt(task, "w1", domain.AgentClaude, "/tmp/base")

	if !strings.Contains(prompt, "# Task: Fix the bug") {
		t.Error("missing task summary")
	}
	if !strings.Contains(prompt, "There is a bug in main.go") {
		t.Error("missing description")
	}
	if !strings.Contains(prompt, "## Files likely involved") {
		t.Error("missing files section")
	}
	if !strings.Contains(prompt, "- main.go") {
		t.Error("missing file entry")
	}
	if !strings.Contains(prompt, "## Acceptance criteria") {
		t.Error("missing acceptance section")
	}
	if !strings.Contains(prompt, marker.CompletionMarker("task-1")) {
		t.Error("missing completion marker")
	}
	if !strings.Contains(prompt, taskReminder) {
		t.Error("missing task reminder")
	}
	// Claude should NOT have the notify-hook shim
	if strings.Contains(prompt, "notify-hook.sh") {
		t.Error("Claude prompt should not reference notify-hook.sh")
	}
}

func TestFormatTaskPrompt_NonClaude(t *testing.T) {
	task := &taskqueue.Task{
		ID:          "task-2",
		Summary:     "Add feature",
		Description: "Add a new feature",
	}

	prompt := FormatTaskPrompt(task, "w1", domain.AgentCodex, "/tmp/base")

	if !strings.Contains(prompt, "notify-hook.sh") {
		t.Error("non-Claude prompt should reference notify-hook.sh")
	}
	if !strings.Contains(prompt, marker.CompletionMarker("task-2")) {
		t.Error("missing completion marker")
	}
}

func TestFormatTaskPrompt_WithFeedback(t *testing.T) {
	task := &taskqueue.Task{
		ID:          "task-3",
		Summary:     "Retry task",
		Description: "Retry this",
		Feedback:    "Missing error handling",
	}

	prompt := FormatTaskPrompt(task, "w1", domain.AgentClaude, "/tmp/base")
	if !strings.Contains(prompt, "## Feedback from previous review") {
		t.Error("missing feedback section")
	}
	if !strings.Contains(prompt, "Missing error handling") {
		t.Error("missing feedback content")
	}
}

func TestTaskMessage_Inline(t *testing.T) {
	task := &taskqueue.Task{
		ID:          "task-4",
		Summary:     "Small task",
		Description: "Do something small",
	}

	base := t.TempDir()
	msg, err := TaskMessage(base, task, "w1", domain.AgentClaude)
	if err != nil {
		t.Fatal(err)
	}
	// Small prompt should be inline
	if strings.HasPrefix(msg, "Read and complete") {
		t.Error("small prompt should be inline, not file-based")
	}
}

func TestTaskMessage_FileBased(t *testing.T) {
	// Create a task with a huge description to exceed maxSendKeysBytes
	task := &taskqueue.Task{
		ID:          "task-5",
		Summary:     "Large task",
		Description: strings.Repeat("x", maxSendKeysBytes+1000),
	}

	base := t.TempDir()
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}

	msg, err := TaskMessage(base, task, "w1", domain.AgentClaude)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(msg, "Read and complete the task described in") {
		t.Errorf("large prompt should be file-based, got: %s", msg[:60])
	}
}
