package daemon

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/AdriaanVE/jack-in/src/domain"
	"github.com/AdriaanVE/jack-in/src/taskqueue"
	"github.com/AdriaanVE/jack-in/src/usage"
)

// buildStateMsg creates a snapshot from the daemon's internal state.
// It fetches current tasks once in batch to avoid N+1 file reads.
func buildStateMsg(base, session string, approval domain.ApprovalMode, workers map[string]*domain.WorkerState, orchState *OrchestratorState, counts domain.TaskCounts, tracker *usage.Tracker) domain.StateEvent {
	// Build task summary lookup from current tasks (single directory read)
	taskSummary := make(map[string]string)
	if currentTasks, err := taskqueue.List(base, domain.StateCurrent); err == nil {
		for _, entry := range currentTasks {
			taskSummary[entry.Task.ID] = entry.Task.Summary
		}
	}

	snaps := make([]domain.WorkerSnapshot, 0, len(workers))
	for _, w := range workers {
		snap := domain.WorkerSnapshot{
			Name:               w.Name,
			Agent:              w.Agent,
			State:              w.StateLabel(),
			CurrentTask:        w.CurrentTask,
			CurrentTaskSummary: taskSummary[w.CurrentTask],
		}
		// Add per-worker token usage (Claude only)
		if w.Agent == domain.AgentClaude && tracker != nil {
			snap.Tokens = tracker.GetWorkerUsage(w.Name).ToTokenUsage()
		}
		snaps = append(snaps, snap)
	}
	sort.Slice(snaps, func(i, j int) bool { return snaps[i].Name < snaps[j].Name })

	// Total tokens = daemon watchdog (Sonnet) + session usage (Opus)
	totalTokens := GetTokenUsage()
	if tracker != nil {
		sessionUsage := tracker.GetTotalUsage().ToTokenUsage()
		totalTokens.InputTokens += sessionUsage.InputTokens
		totalTokens.OutputTokens += sessionUsage.OutputTokens
		totalTokens.CostUSD += sessionUsage.CostUSD
	}

	msg := domain.StateEvent{
		Workers:  snaps,
		Tasks:    counts,
		Tokens:   totalTokens,
		Approval: approval,
		Session:  session,
	}

	if orchState != nil {
		msg.Orchestrator = &domain.WorkerSnapshot{
			Name:  "orchestrator",
			Agent: orchState.Agent,
			State: orchState.StateLabel(),
		}
		// Add orchestrator token usage (Claude only)
		if orchState.Agent == domain.AgentClaude && tracker != nil {
			msg.Orchestrator.Tokens = tracker.GetOrchestratorUsage().ToTokenUsage()
		}
	}

	return msg
}

// stateHash computes a simple hash of the state event for change detection.
// Includes a 10-second time bucket to force periodic TUI refreshes for token updates.
func stateHash(e domain.StateEvent) string {
	var b strings.Builder
	// Time bucket (10s) ensures TUI refreshes periodically even if only tokens change
	timeBucket := time.Now().Unix() / 10
	b.WriteString(fmt.Sprintf("a=%s;t=%d/%d/%d/%d/%d;tok=%d/%d/%.4f;tb=%d;",
		e.Approval, e.Tasks.Pending, e.Tasks.Current, e.Tasks.Review, e.Tasks.Complete, e.Tasks.Rejected,
		e.Tokens.InputTokens, e.Tokens.OutputTokens, e.Tokens.CostUSD, timeBucket))
	for _, w := range e.Workers {
		b.WriteString(fmt.Sprintf("w=%s:%s:%s:%s:%d/%d/%.4f;", w.Name, w.State, w.CurrentTask, w.CurrentTaskSummary,
			w.Tokens.InputTokens, w.Tokens.OutputTokens, w.Tokens.CostUSD))
	}
	if e.Orchestrator != nil {
		b.WriteString(fmt.Sprintf("o=%s:%d/%d/%.4f;", e.Orchestrator.State,
			e.Orchestrator.Tokens.InputTokens, e.Orchestrator.Tokens.OutputTokens, e.Orchestrator.Tokens.CostUSD))
	}
	return b.String()
}

// sendEvent sends a message to the events channel without blocking.
func sendEvent(ch chan<- any, msg any) {
	if ch == nil {
		return
	}
	select {
	case ch <- msg:
	default:
	}
}
