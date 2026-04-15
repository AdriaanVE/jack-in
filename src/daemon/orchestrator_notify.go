package daemon

import (
	"fmt"
	"log/slog"

	"github.com/AdriaanVE/jack-in/src/tmux"
)

// OrchestratorEvent types for notifications.
const (
	EventTaskReview     = "TASK_REVIEW"
	EventAllComplete    = "ALL_COMPLETE"
	EventWorkerStalled  = "WORKER_STALLED"
	EventWorkerNeedHelp = "WORKER_NEEDS_HELP"
)

// notifyOrchestrator sends an event message to the orchestrator agent pane.
// Messages are prefixed with JACKIN_EVENT: for easy parsing by the agent.
// Runs async to avoid blocking the daemon tick loop (SendKeysDoubleEnter has 500ms sleep).
func notifyOrchestrator(session, event string) {
	go func() {
		target := tmux.PaneTarget(session, "orchestrator")
		msg := "JACKIN_EVENT: " + event
		if err := tmux.SendKeysDoubleEnter(target, msg); err != nil {
			slog.Debug("failed to notify orchestrator", "target", target, "event", event, "error", err)
		}
	}()
}

// NotifyTaskReview notifies the orchestrator that a task is ready for review.
func NotifyTaskReview(session, taskID, worker, summary string) {
	event := fmt.Sprintf("%s task=%s worker=%s summary=%s", EventTaskReview, taskID, worker, truncate(summary, 50))
	notifyOrchestrator(session, event)
}

// NotifyAllComplete notifies the orchestrator that all tasks are done.
func NotifyAllComplete(session string, complete, rejected int) {
	event := fmt.Sprintf("%s complete=%d rejected=%d", EventAllComplete, complete, rejected)
	notifyOrchestrator(session, event)
}

// NotifyWorkerStalled notifies the orchestrator that a worker appears stuck.
func NotifyWorkerStalled(session, worker, taskID, reason string) {
	event := fmt.Sprintf("%s worker=%s task=%s reason=%s", EventWorkerStalled, worker, taskID, truncate(reason, 50))
	notifyOrchestrator(session, event)
}

// NotifyWorkerNeedsHelp notifies the orchestrator that a worker needs manual input.
func NotifyWorkerNeedsHelp(session, worker, taskID string) {
	event := fmt.Sprintf("%s worker=%s task=%s", EventWorkerNeedHelp, worker, taskID)
	notifyOrchestrator(session, event)
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}
