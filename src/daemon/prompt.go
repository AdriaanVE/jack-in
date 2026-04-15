package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/AdriaanVE/jack-in/src/agent"
	"github.com/AdriaanVE/jack-in/src/domain"
	"github.com/AdriaanVE/jack-in/src/marker"
	"github.com/AdriaanVE/jack-in/src/taskqueue"
)

const (
	promptDir        = ".jack-in/prompts"
	maxSendKeysBytes = 3500
)

var taskReminder = "Reminder: commit all changes before completing. Do not push. Approve any writes to .jack-in/signals/."

// FormatTaskPrompt builds the markdown prompt sent to a worker.
// Structure: general instructions first, then task-specific content.
func FormatTaskPrompt(task *taskqueue.Task, workerName string, agentType domain.AgentType, base string) string {
	var b strings.Builder

	// --- General instructions ---
	b.WriteString("# Instructions\n\n")
	b.WriteString(taskReminder)
	b.WriteString("\n\nDo not ask for confirmation before proceeding. Implement the changes directly. Only stop to ask if the task is ambiguous or you would need to make a destructive/irreversible change.\n\n")

	mkr := marker.CompletionMarker(task.ID)

	if agentType == domain.AgentClaude {
		b.WriteString("When you have fully completed this task, output exactly this on its own line as the last line of your final message:\n")
		b.WriteString(mkr)
	} else {
		esc := agent.ShellEscape
		shim := filepath.Join(base, ".jack-in", "notify-hook.sh")
		sigDir := filepath.Join(base, signalDir)
		fmt.Fprintf(&b, "When you are completely done with this task, run: %s done %s %s\n", shim, esc(sigDir), esc(workerName))
		fmt.Fprintf(&b, "Also output exactly this on its own line: %s\n", mkr)
		fmt.Fprintf(&b, "\nTip: If you encounter an error you can't resolve, run: %s error %s %s \"description\"\n", shim, esc(sigDir), esc(workerName))
	}

	// --- Task-specific content ---
	b.WriteString("\n\n---\n\n")
	fmt.Fprintf(&b, "# Task: %s\n\n", task.Summary)
	b.WriteString(task.Description)

	if len(task.Files) > 0 {
		b.WriteString("\n\n## Files likely involved\n")
		for _, f := range task.Files {
			fmt.Fprintf(&b, "- %s\n", f)
		}
	}
	if len(task.Acceptance) > 0 {
		b.WriteString("\n\n## Acceptance criteria\n")
		for _, a := range task.Acceptance {
			fmt.Fprintf(&b, "- %s\n", a)
		}
	}
	if task.Feedback != "" {
		b.WriteString("\n\n## Feedback from previous review\n")
		b.WriteString(task.Feedback)
	}

	b.WriteString("\n")
	return b.String()
}

// writePromptFile writes content to a prompt file and returns its path.
func writePromptFile(base, taskID, content string) (string, error) {
	dir := filepath.Join(base, promptDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, taskID+".md")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// TaskMessage builds the message to send to a worker via tmux send-keys.
// If the prompt is small enough, it returns it inline; otherwise writes to a
// file and returns a pointer to the file.
func TaskMessage(base string, task *taskqueue.Task, workerName string, agentType domain.AgentType) (string, error) {
	prompt := FormatTaskPrompt(task, workerName, agentType, base)
	if len([]byte(prompt)) <= maxSendKeysBytes {
		return prompt, nil
	}
	path, err := writePromptFile(base, task.ID, prompt)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Read and complete the task described in %s", path), nil
}
