package daemon

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/AdriaanVE/jack-in/src/domain"
	"github.com/AdriaanVE/jack-in/src/marker"
	"github.com/AdriaanVE/jack-in/src/taskqueue"
	"github.com/AdriaanVE/jack-in/src/tmux"
)

// OrchestratorState tracks the orchestrator agent's watchdog state.
type OrchestratorState struct {
	Agent            domain.AgentType
	LastPaneSnapshot string
	LastSnapshotAt   int64
	LLMEvalCount     int
	EscalatedToUser  bool
	StartedAt        time.Time
}

func (o *OrchestratorState) StateLabel() string {
	if o.EscalatedToUser {
		return domain.LabelStuck
	}
	return domain.LabelWorking
}

func (o *OrchestratorState) reset() {
	o.LastPaneSnapshot = ""
	o.LastSnapshotAt = 0
	o.LLMEvalCount = 0
	o.EscalatedToUser = false
}

// watchdog implements the tiered watchdog: cheap pane check first, then LLM eval.
func watchdog(session string, state *domain.WorkerState, base string, approval domain.ApprovalMode, now time.Time) {
	target := tmux.PaneTarget(session, state.Name)
	paneContent, err := tmux.CapturePane(target, 50)
	if err != nil {
		return // pane gone or inaccessible
	}

	nowMs := now.UnixMilli()

	// Daemon-side marker scan (non-Claude agents only).
	// Claude uses the transcript-aware stop hook; scanning the pane would
	// produce false positives because the marker appears in the instructions.
	if state.Agent != domain.AgentClaude && state.CurrentTask != "" && marker.HasCompletionMarker(paneContent, state.CurrentTask) {
		slog.Info("marker found in pane", "worker", state.Name, "task", state.CurrentTask)
		_ = WriteIdleSignal(base, state.Name)
		return
	}

	// Tier 2: pane snapshot diff
	elapsed := time.Duration(nowMs-state.AssignedAt) * time.Millisecond
	if elapsed < Tier3Timeout {
		prev := state.LastPaneSnapshot
		state.LastPaneSnapshot = paneContent
		state.LastSnapshotAt = nowMs

		if prev == "" {
			slog.Debug("tier2: first snapshot", "worker", state.Name)
			return
		}

		if prev == paneContent {
			slog.Info("tier2: pane unchanged, nudging", "worker", state.Name)
			mkr := marker.CompletionMarker(state.CurrentTask)
			nudge := fmt.Sprintf("If you have completed your task, output exactly this on its own line: %s", mkr)
			_ = tmux.SendKeys(target, nudge, true)
		} else {
			slog.Debug("tier2: pane changed, agent active", "worker", state.Name)
		}
		return
	}

	// Tier 3: LLM evaluation with guardrails
	prev := state.LastPaneSnapshot
	state.LastPaneSnapshot = paneContent
	state.LastSnapshotAt = nowMs

	// If escalated but pane changed, worker recovered - reset escalation
	if state.EscalatedToUser {
		if prev != "" && prev != paneContent {
			slog.Info("tier3: worker recovered, clearing escalation", "worker", state.Name)
			state.EscalatedToUser = false
			state.LLMEvalCount = 0
		} else {
			slog.Debug("tier3: already escalated", "worker", state.Name)
			return
		}
	}

	if state.LLMEvalCount >= MaxLLMEvals {
		slog.Warn("tier3: max evals exhausted, escalating", "worker", state.Name)
		state.EscalatedToUser = true
		_ = tmux.DisplayMessage(session,
			fmt.Sprintf("JACKIN: %s may be stuck - %d checks failed, needs manual attention", state.Name, MaxLLMEvals))
		NotifyWorkerStalled(session, state.Name, state.CurrentTask, "max watchdog checks exhausted")
		return
	}

	switch approval {
	case domain.ApprovalYolo:
		slog.Info("yolo: sending Enter", "worker", state.Name)
		_ = tmux.SendKeys(target, "", true)
		state.LLMEvalCount++
		return

	case domain.ApprovalManual:
		slog.Info("manual: worker may be stuck", "worker", state.Name)
		state.EscalatedToUser = true
		_ = tmux.DisplayMessage(session, fmt.Sprintf("JACKIN: %s may be stuck - check manually", state.Name))
		NotifyWorkerStalled(session, state.Name, state.CurrentTask, "manual mode escalation")
		return
	}

	// approval == "auto" -- LLM evaluation
	state.LLMEvalCount++
	slog.Info("tier3: LLM eval", "worker", state.Name, "eval", state.LLMEvalCount, "max", MaxLLMEvals)

	taskSummary := "unknown task"
	if state.CurrentTask != "" {
		if entry, err := taskqueue.Get(base, state.CurrentTask); err == nil && entry != nil {
			taskSummary = entry.Task.Summary
		}
	}

	result, err := EvaluatePane(paneContent, taskSummary, string(state.Agent))
	if err != nil {
		slog.Error("LLM eval failed", "worker", state.Name, "error", err)
		return
	}

	slog.Info("tier3: LLM result",
		"worker", state.Name,
		"status", result.Status,
		"safe", result.SafeToApprove,
		"action", firstNonEmpty(result.ApprovalKeystroke, result.ResponseText, "none"),
		"reason", result.Reason,
	)

	switch result.Status {
	case PaneStatusPermission:
		if result.SafeToApprove && result.ApprovalKeystroke != "" {
			slog.Info("auto-approve", "worker", state.Name, "reason", result.Reason)
			if result.ApprovalKeystroke == "Enter" {
				_ = tmux.SendKeys(target, "", true)
			} else {
				_ = tmux.SendKeys(target, result.ApprovalKeystroke, true)
			}
			// Reset watchdog so we don't immediately escalate on next tick
			resetWatchdog(state)
		} else {
			slog.Info("needs manual attention", "worker", state.Name, "reason", result.Reason)
			state.EscalatedToUser = true
			_ = tmux.DisplayMessage(session, fmt.Sprintf("JACKIN: %s needs approval - %s", state.Name, result.Reason))
			NotifyWorkerNeedsHelp(session, state.Name, state.CurrentTask)
		}

	case PaneStatusWaitingInput:
		if result.ResponseText != "" {
			slog.Info("auto-respond", "worker", state.Name, "text", result.ResponseText)
			_ = tmux.SendKeys(target, result.ResponseText, true)
			resetWatchdog(state)
		}

	case PaneStatusError:
		slog.Info("error detected", "worker", state.Name, "reason", result.Reason)
		state.EscalatedToUser = true
		_ = tmux.DisplayMessage(session, fmt.Sprintf("JACKIN: %s hit an error - %s", state.Name, result.Reason))
		NotifyWorkerStalled(session, state.Name, state.CurrentTask, result.Reason)

	default:
		slog.Debug("tier3: no action needed", "worker", state.Name, "status", result.Status)
	}
}

// checkOrchestrator monitors the orchestrator agent for stalls.
func checkOrchestrator(session string, state *OrchestratorState, approval domain.ApprovalMode) {
	target := tmux.PaneTarget(session, "orchestrator")
	paneContent, err := tmux.CapturePane(target, 50)
	if err != nil {
		if !state.EscalatedToUser {
			slog.Warn("orchestrator pane capture failed")
			state.EscalatedToUser = true
			_ = tmux.DisplayMessage(session, "JACKIN: orchestrator pane gone -- may have crashed")
		}
		return
	}

	// Pane changed -- active
	if state.LastPaneSnapshot != "" && paneContent != state.LastPaneSnapshot {
		state.LastPaneSnapshot = paneContent
		state.LastSnapshotAt = time.Now().UnixMilli()
		state.LLMEvalCount = 0
		state.EscalatedToUser = false
		slog.Debug("orchestrator active")
		return
	}

	// First snapshot
	if state.LastPaneSnapshot == "" {
		state.LastPaneSnapshot = paneContent
		state.LastSnapshotAt = time.Now().UnixMilli()
		slog.Debug("orchestrator: first snapshot")
		return
	}

	// Pane unchanged -- check stall
	stalledFor := time.Duration(time.Now().UnixMilli()-state.LastSnapshotAt) * time.Millisecond
	if stalledFor < OrchStallTimeout {
		slog.Debug("orchestrator: pane unchanged", "stalled_for", stalledFor, "threshold", OrchStallTimeout)
		return
	}

	if state.EscalatedToUser {
		return
	}

	if state.LLMEvalCount >= MaxLLMEvals {
		slog.Warn("orchestrator: max evals exhausted")
		state.EscalatedToUser = true
		_ = tmux.DisplayMessage(session, fmt.Sprintf("JACKIN: orchestrator may be stuck - %d checks failed", MaxLLMEvals))
		return
	}

	switch approval {
	case domain.ApprovalYolo:
		slog.Info("orchestrator: stalled, sending Enter (yolo)")
		_ = tmux.SendKeys(target, "", true)
		state.LLMEvalCount++
		return

	case domain.ApprovalManual:
		slog.Info("orchestrator: stalled, notifying user")
		state.EscalatedToUser = true
		_ = tmux.DisplayMessage(session, "JACKIN: orchestrator may be stuck - check manually")
		return
	}

	// auto mode -- LLM eval
	state.LLMEvalCount++
	slog.Info("orchestrator: LLM eval", "eval", state.LLMEvalCount, "max", MaxLLMEvals)

	result, err := EvaluatePane(paneContent, "orchestrator agent reviewing tasks", string(state.Agent))
	if err != nil {
		slog.Error("orchestrator LLM eval failed", "error", err)
		return
	}

	slog.Info("orchestrator: LLM result",
		"status", result.Status,
		"safe", result.SafeToApprove,
		"reason", result.Reason,
	)

	switch result.Status {
	case PaneStatusPermission:
		if result.SafeToApprove && result.ApprovalKeystroke != "" {
			slog.Info("orchestrator: auto-approve", "reason", result.Reason)
			if result.ApprovalKeystroke == "Enter" {
				_ = tmux.SendKeys(target, "", true)
			} else {
				_ = tmux.SendKeys(target, result.ApprovalKeystroke, true)
			}
		} else {
			slog.Info("orchestrator: needs manual attention", "reason", result.Reason)
			state.EscalatedToUser = true
			_ = tmux.DisplayMessage(session, fmt.Sprintf("JACKIN: orchestrator needs approval - %s", result.Reason))
		}

	case PaneStatusError:
		slog.Info("orchestrator: error detected", "reason", result.Reason)
		state.EscalatedToUser = true
		_ = tmux.DisplayMessage(session, fmt.Sprintf("JACKIN: orchestrator hit an error - %s", result.Reason))
	}
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}
