package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"github.com/AdriaanVE/jack-in/src/config"
	"github.com/AdriaanVE/jack-in/src/domain"
	"github.com/AdriaanVE/jack-in/src/taskqueue"
	"github.com/AdriaanVE/jack-in/src/tmux"
	"github.com/AdriaanVE/jack-in/src/usage"
	"github.com/AdriaanVE/jack-in/src/worktree"
	"github.com/fsnotify/fsnotify"
)

// Timing constants ported from the Deno implementation.
const (
	MinWorkDuration    = 10 * time.Second
	Tier2Timeout       = 60 * time.Second
	Tier3Timeout       = 120 * time.Second
	MaxLLMEvals        = 3
	HeartbeatFreshness = 30 * time.Second
	MaxTaskWallTime    = 15 * time.Minute
	NeedsInputGrace    = 5 * time.Second
	OrchStallTimeout   = 60 * time.Second
	StartupGrace       = 10 * time.Second
)

// Options configures the daemon loop.
type Options struct {
	Config *config.Config
	Base   string
	Events chan<- any // optional: receives StateMsg and LogMsg for TUI
}

// Run starts the daemon poll loop. It blocks until ctx is cancelled.
func Run(ctx context.Context, opts Options) error {
	cfg := opts.Config
	base := opts.Base
	session := config.SessionName(cfg.Project)
	interval := time.Duration(cfg.Orchestrator.PollInterval) * time.Millisecond
	approval := cfg.Orchestrator.Approval

	workers := make(map[string]*domain.WorkerState)
	for _, w := range cfg.Workers {
		if w.Role == domain.RoleExecutor {
			workers[w.Name] = &domain.WorkerState{
				Name:  w.Name,
				Agent: w.Agent,
			}
		}
	}

	if len(workers) == 0 {
		return fmt.Errorf("no executor workers configured")
	}

	var orchState *OrchestratorState
	if cfg.Orchestrator.OrchestratorEnabled() {
		orchState = &OrchestratorState{
			Agent:     domain.AgentType(cfg.Orchestrator.Agent),
			StartedAt: time.Now(),
		}
	}

	// Token usage tracker (Claude sessions only for now)
	// Use tmux session creation time as the anchor for filtering session files,
	// so we include orchestrator sessions that started before the daemon.
	sessionStart := time.Now()
	if epoch, err := tmux.SessionCreated(session); err == nil {
		sessionStart = time.Unix(epoch, 0)
	}
	usageTracker := usage.NewTracker(base, cfg.Project, sessionStart)

	// Init signal directories
	if err := InitSignalDirs(base); err != nil {
		return fmt.Errorf("init signals: %w", err)
	}

	// Setup logging (file always, console or TUI tee depending on Events)
	cleanup, logErr := SetupLogging(base, LoggingOptions{Events: opts.Events})
	if logErr != nil {
		slog.Warn("failed to setup file logging", "error", logErr)
	} else {
		defer cleanup()
	}

	// Reconcile in-flight tasks from a previous daemon run
	if err := reconcileTasks(session, base, workers); err != nil {
		slog.Warn("task reconciliation error", "error", err)
	}

	// Clear stale signals; for idle workers, clear non-done signals and
	// write the idle (.done) signal. For busy workers, clear everything.
	for name, state := range workers {
		if state.CurrentTask == "" {
			_ = ClearNeedsInput(base, name)
			_ = ClearExited(base, name)
			_ = ClearHeartbeat(base, name)
			_ = WriteIdleSignal(base, name)
		} else {
			ClearAllWorkerSignals(base, name)
		}
	}

	startupReady := false
	startupDeadline := time.Now().Add(StartupGrace)
	allCompleteNotified := false
	var lastStateHash string
	slog.Info("daemon started",
		"executors", len(workers),
		"poll_interval", interval,
		"approval", approval,
	)

	for {
		select {
		case <-ctx.Done():
			slog.Info("daemon stopped")
			return nil
		default:
		}

		// Poll for runtime approval mode changes
		if newMode := ReadApprovalMode(base); newMode != "" && newMode != approval {
			slog.Info("approval mode changed", "from", approval, "to", newMode)
			approval = newMode
			for _, state := range workers {
				if state.CurrentTask != "" {
					resetWatchdog(state)
				}
			}
			if orchState != nil {
				orchState.reset()
			}
		}

		if !startupReady && time.Now().After(startupDeadline) {
			startupReady = true
		}
		tick(session, base, cfg.Project, cfg.Branch, workers, approval, startupReady)

		if orchState != nil {
			checkOrchestrator(session, orchState, approval)
		}

		c, _ := taskqueue.Counts(base)
		workerNames := make([]string, 0, len(workers))
		for name := range workers {
			workerNames = append(workerNames, name)
		}
		usageTracker.Update(workerNames)
		stateMsg := buildStateMsg(base, session, approval, workers, orchState, c, usageTracker)
		if hash := stateHash(stateMsg); hash != lastStateHash {
			lastStateHash = hash
			sendEvent(opts.Events, stateMsg)
		}
		slog.Debug("tick done",
			"pending", c.Pending,
			"current", c.Current,
			"review", c.Review,
			"complete", c.Complete,
			"rejected", c.Rejected,
		)

		if c.Pending == 0 && c.Current == 0 && c.Review == 0 && c.Rejected == 0 {
			if !allCompleteNotified {
				slog.Info("all tasks complete, watching for new tasks",
					"complete", c.Complete, "rejected", c.Rejected)
				NotifyAllComplete(session, c.Complete, c.Rejected)
				allCompleteNotified = true
			}
			foundTask := waitForNewTask(ctx, base)
			if ctx.Err() != nil {
				break
			}
			if foundTask {
				slog.Info("new task detected, resuming poll loop")
				allCompleteNotified = false
				for name, state := range workers {
					if state.CurrentTask == "" {
						_ = WriteIdleSignal(base, name)
					}
				}
			}
			continue
		}

		select {
		case <-ctx.Done():
			slog.Info("daemon stopped")
			return nil
		case <-time.After(interval):
		}
	}

	return nil
}

// sendTaskToWorker sends a task prompt to a worker's tmux pane.
// Claude and Codex agents get a double-Enter to ensure prompt submission.
func sendTaskToWorker(target string, msg string, agentType domain.AgentType) error {
	if agentType == domain.AgentClaude || agentType == domain.AgentCodex {
		return tmux.SendKeysDoubleEnter(target, msg)
	}
	return tmux.SendKeys(target, msg, true)
}

func reconcileTasks(session, base string, workers map[string]*domain.WorkerState) error {
	currentTasks, err := taskqueue.List(base, domain.StateCurrent)
	if err != nil {
		return err
	}
	for _, entry := range currentTasks {
		assignee := entry.Task.Assignee
		state, ok := workers[assignee]
		if !ok {
			continue
		}
		state.CurrentTask = entry.Task.ID
		state.AssignedAt = time.Now().UnixMilli()
		_ = WriteCurrentTask(base, assignee, entry.Task.ID)

		msg, err := TaskMessage(base, &entry.Task, state.Name, state.Agent)
		if err != nil {
			slog.Warn("reconcile: failed to build message", "task", entry.Task.ID, "error", err)
			continue
		}
		target := tmux.PaneTarget(session, state.Name)
		if err := sendTaskToWorker(target, msg, state.Agent); err != nil {
			slog.Warn("reconcile: failed to re-send task", "task", entry.Task.ID, "worker", assignee, "error", err)
		} else {
			slog.Info("reconcile: re-sent task", "task", entry.Task.ID, "worker", assignee)
		}
	}

	// Restore ReviewTaskID for workers that had tasks in review or rejected
	reviewTasks, err := taskqueue.List(base, domain.StateReview)
	if err != nil {
		return err
	}
	for _, entry := range reviewTasks {
		assignee := entry.Task.Assignee
		state, ok := workers[assignee]
		if !ok || state.CurrentTask != "" {
			continue
		}
		state.ReviewTaskID = entry.Task.ID
		slog.Info("reconcile: restored review block", "task", entry.Task.ID, "worker", assignee)
	}

	// Also restore ReviewTaskID for rejected tasks (worker stays blocked until retry/drop)
	rejectedTasks, err := taskqueue.List(base, domain.StateRejected)
	if err != nil {
		return err
	}
	for _, entry := range rejectedTasks {
		assignee := entry.Task.Assignee
		state, ok := workers[assignee]
		if !ok || state.CurrentTask != "" || state.ReviewTaskID != "" {
			continue
		}
		state.ReviewTaskID = entry.Task.ID
		slog.Info("reconcile: restored rejected block", "task", entry.Task.ID, "worker", assignee)
	}

	// Handle retried tasks (in current with Retries > 0 and idle worker)
	for _, entry := range currentTasks {
		if entry.Task.Retries == 0 {
			continue // Not a retry, handled by normal flow
		}
		assignee := entry.Task.Assignee
		state, ok := workers[assignee]
		if !ok || state.CurrentTask != "" {
			continue
		}
		// This is a retried task waiting to be picked up - set ReviewTaskID so
		// the tick() loop will detect it and re-assign to the worker
		state.ReviewTaskID = entry.Task.ID
		slog.Info("reconcile: restored retry pending", "task", entry.Task.ID, "worker", assignee)
	}

	return nil
}

func tick(session, base, project, branch string, workers map[string]*domain.WorkerState, approval domain.ApprovalMode, startupReady bool) {
	now := time.Now()
	nowMs := now.UnixMilli()
	idleWorkers := make([]*domain.WorkerState, 0, len(workers))

	for _, state := range workers {
		// Check for cancel signal (highest priority)
		if HasCancel(base, state.Name) {
			payload := ReadCancelTaskID(base, state.Name)
			taskID, action := parseCancelPayload(payload)

			// Validate: cancel signal must match worker's current task
			if state.CurrentTask == "" {
				slog.Warn("stale cancel signal (worker idle)", "worker", state.Name, "signal_task", taskID)
				_ = ClearCancel(base, state.Name)
				// continue to idle worker handling below
			} else if taskID != state.CurrentTask {
				slog.Warn("stale cancel signal (task mismatch)", "worker", state.Name, "signal_task", taskID, "current_task", state.CurrentTask)
				_ = ClearCancel(base, state.Name)
				// continue with current task processing
			} else {
				slog.Info("task cancelled by user", "worker", state.Name, "task", taskID, "action", action)

				// Send cancellation message to worker
				target := tmux.PaneTarget(session, state.Name)
				cancelMsg := fmt.Sprintf("TASK CANCELLED: Task %s has been cancelled. Stop work immediately and await new assignment.", taskID)
				if err := sendTaskToWorker(target, cancelMsg, state.Agent); err != nil {
					slog.Warn("failed to send cancel message", "worker", state.Name, "error", err)
				}

				// Handle task based on action
				if action == "drop" {
					if _, err := taskqueue.Cancel(base, taskID); err != nil {
						slog.Warn("failed to delete cancelled task", "task", taskID, "error", err)
					} else {
						slog.Info("cancelled task deleted", "task", taskID)
					}
				} else {
					// Default: return to pending
					if _, err := taskqueue.Unclaim(base, taskID); err != nil {
						slog.Warn("failed to unclaim cancelled task", "task", taskID, "error", err)
					} else {
						slog.Info("cancelled task returned to pending", "task", taskID)
					}
				}

				// Clear worker state
				_ = ClearCurrentTask(base, state.Name)
				clearAssignment(state)
				ClearAllWorkerSignals(base, state.Name)
				_ = WriteIdleSignal(base, state.Name)
				continue
			}
		}

		if state.CurrentTask == "" {
			// Check if worker is blocked by a task still in review
			if state.ReviewTaskID != "" {
				entry, err := taskqueue.Get(base, state.ReviewTaskID)
				if err != nil {
					slog.Warn("failed to check review task", "worker", state.Name, "task", state.ReviewTaskID, "error", err)
					continue
				}
				if entry != nil && entry.State == domain.StateReview {
					slog.Debug("worker blocked by review", "worker", state.Name, "task", state.ReviewTaskID)
					continue
				}
				// Task left review -- check if orchestrator still needs the worktree
				if entry != nil && entry.State == domain.StateRejected {
					// Rejected: keep worker blocked so orchestrator can inspect worktree
					slog.Debug("worker blocked pending rejected task handling", "worker", state.Name, "task", state.ReviewTaskID)
					continue
				}
				// Retried task: re-assign to same worker without refreshing worktree
				if entry != nil && entry.State == domain.StateCurrent && entry.Task.Assignee == state.Name {
					slog.Info("re-assigning retried task", "worker", state.Name, "task", state.ReviewTaskID, "feedback", entry.Task.Feedback)
					ClearAllWorkerSignals(base, state.Name)
					_ = WriteCurrentTask(base, state.Name, entry.Task.ID)
					msg, err := TaskMessage(base, &entry.Task, state.Name, state.Agent)
					if err != nil {
						slog.Warn("failed to build retry message", "task", entry.Task.ID, "error", err)
						state.ReviewTaskID = ""
						continue
					}
					target := tmux.PaneTarget(session, state.Name)
					if err := sendTaskToWorker(target, msg, state.Agent); err != nil {
						slog.Warn("failed to send retry task", "worker", state.Name, "error", err)
						state.ReviewTaskID = ""
						continue
					}
					state.CurrentTask = entry.Task.ID
					state.AssignedAt = nowMs
					state.ReviewTaskID = ""
					resetWatchdog(state)
					continue
				}
				// Refresh worktree: approved (complete) or dropped (nil)
				if entry == nil || entry.State == domain.StateComplete {
					wtPath := worktree.Path(base, project, state.Name)
					slog.Info("refreshing worktree", "worker", state.Name, "branch", branch)
					if err := worktree.Refresh(wtPath, project, state.Name, branch); err != nil {
						slog.Warn("worktree refresh failed", "worker", state.Name, "error", err)
					}
				}
				state.ReviewTaskID = ""
			}
			slog.Debug("worker idle", "worker", state.Name)
			idleWorkers = append(idleWorkers, state)
			continue
		}

		// Batch signal checks
		exited := HasExited(base, state.Name)
		signaled := HasSignal(base, state.Name)
		needsInput := HasNeedsInput(base, state.Name)
		hbAge := HeartbeatAge(base, state.Name)

		// 1. Exited signal -- worker process is gone
		if exited {
			slog.Warn("worker exited", "worker", state.Name, "task", state.CurrentTask)
			if _, err := taskqueue.Unclaim(base, state.CurrentTask); err != nil {
				slog.Warn("could not unclaim task", "task", state.CurrentTask, "error", err)
			}
			_ = ClearCurrentTask(base, state.Name)
			clearAssignment(state)
			ClearAllWorkerSignals(base, state.Name)
			_ = tmux.DisplayMessage(session, fmt.Sprintf("JACKIN: %s exited -- task returned to queue", state.Name))
			continue
		}

		// 2. Done signal -- completion
		slog.Debug("worker status", "worker", state.Name, "task", state.CurrentTask, "signaled", signaled)
		if signaled {
			elapsed := nowMs - state.AssignedAt
			if state.AssignedAt > 0 && elapsed < MinWorkDuration.Milliseconds() {
				slog.Debug("ignoring early signal", "worker", state.Name, "elapsed_ms", elapsed)
				_ = ClearSignal(base, state.Name)
				continue
			}
			// Worker finished
			taskID := state.CurrentTask
			var taskSummary string
			if entry, err := taskqueue.Get(base, taskID); err == nil && entry != nil {
				taskSummary = entry.Task.Summary
			}
			if _, err := taskqueue.Review(base, taskID); err != nil {
				slog.Warn("could not move to review", "task", taskID, "error", err)
			} else {
				slog.Info("task sent to review", "worker", state.Name, "task", taskID)
				NotifyTaskReview(session, taskID, state.Name, taskSummary)
			}
			_ = ClearCurrentTask(base, state.Name)
			state.ReviewTaskID = taskID
			clearAssignment(state)
			continue
		}

		// 3. Needs-input signal
		if needsInput {
			if state.AssignedAt > 0 && nowMs-state.AssignedAt < NeedsInputGrace.Milliseconds() {
				slog.Debug("ignoring early needs-input", "worker", state.Name)
				_ = ClearNeedsInput(base, state.Name)
				continue
			}
			slog.Info("worker needs input", "worker", state.Name)
			target := tmux.PaneTarget(session, state.Name)
			switch approval {
			case domain.ApprovalYolo:
				slog.Info("yolo: sending Enter", "worker", state.Name)
				_ = tmux.SendKeys(target, "", true)
			case domain.ApprovalAuto:
				watchdog(session, state, base, approval, now)
			default:
				slog.Info("manual: notifying user", "worker", state.Name)
				_ = tmux.DisplayMessage(session, fmt.Sprintf("JACKIN: %s needs input -- check worker pane", state.Name))
				NotifyWorkerNeedsHelp(session, state.Name, state.CurrentTask)
			}
			_ = ClearNeedsInput(base, state.Name)
			continue
		}

		// 4. Heartbeat freshness check
		elapsed := time.Duration(0)
		if state.AssignedAt > 0 {
			elapsed = time.Duration(nowMs-state.AssignedAt) * time.Millisecond
		}

		// 5. Fresh heartbeat and under wall time: skip watchdog
		if hbAge >= 0 && hbAge < HeartbeatFreshness && elapsed < MaxTaskWallTime {
			slog.Debug("heartbeat fresh, skipping watchdog", "worker", state.Name, "hb_age", hbAge)
			continue
		}

		// 6. Over wall time: force watchdog
		if elapsed >= MaxTaskWallTime {
			slog.Info("wall time exceeded, forcing watchdog", "worker", state.Name, "elapsed", elapsed)
			watchdog(session, state, base, approval, now)
			continue
		}

		// 7. Normal Tier 2/3 logic
		if state.AssignedAt > 0 && elapsed > Tier2Timeout {
			watchdog(session, state, base, approval, now)
		}
	}

	// Assign ready tasks to idle workers
	if len(idleWorkers) == 0 {
		printStatus(workers)
		return
	}
	if !startupReady {
		slog.Debug("startup grace period, skipping task assignment")
		printStatus(workers)
		return
	}
	readyTasks, err := taskqueue.Ready(base)
	if err != nil {
		slog.Warn("failed to get ready tasks", "error", err)
		return
	}

	taskIdx := 0
	for _, state := range idleWorkers {
		if taskIdx >= len(readyTasks) {
			break
		}
		task := readyTasks[taskIdx]

		// Refresh worktree before assignment to ensure worker has latest code
		wtPath := worktree.Path(base, project, state.Name)
		slog.Info("refreshing worktree before task assignment", "worker", state.Name, "branch", branch)
		if err := worktree.Refresh(wtPath, project, state.Name, branch); err != nil {
			slog.Warn("worktree refresh failed, assigning anyway", "worker", state.Name, "error", err)
		}

		if _, err := taskqueue.Claim(base, task.ID, state.Name); err != nil {
			taskIdx++
			continue
		}

		ClearAllWorkerSignals(base, state.Name)
		_ = WriteCurrentTask(base, state.Name, task.ID)

		msg, err := TaskMessage(base, &task, state.Name, state.Agent)
		if err != nil {
			slog.Warn("failed to build task message", "task", task.ID, "error", err)
			_, _ = taskqueue.Unclaim(base, task.ID)
			_ = ClearCurrentTask(base, state.Name)
			taskIdx++
			continue
		}

		target := tmux.PaneTarget(session, state.Name)
		if err := sendTaskToWorker(target, msg, state.Agent); err != nil {
			slog.Warn("failed to send task", "worker", state.Name, "error", err)
			_, _ = taskqueue.Unclaim(base, task.ID)
			_ = ClearCurrentTask(base, state.Name)
			taskIdx++
			continue
		}

		state.CurrentTask = task.ID
		state.AssignedAt = nowMs
		resetWatchdog(state)
		slog.Info("task assigned", "task", task.ID, "worker", state.Name, "summary", task.Summary)
		taskIdx++
	}

	printStatus(workers)
}

func resetWatchdog(state *domain.WorkerState) {
	state.LastPaneSnapshot = ""
	state.LastSnapshotAt = 0
	state.LLMEvalCount = 0
	state.EscalatedToUser = false
}

// parseCancelPayload extracts taskID and action from "taskID:action" format.
// Returns taskID and action ("pending" or "drop"). Defaults to "pending" if no action.
func parseCancelPayload(payload string) (taskID, action string) {
	if idx := strings.LastIndex(payload, ":"); idx > 0 {
		taskID = payload[:idx]
		action = payload[idx+1:]
		if action != "drop" && action != "pending" {
			action = "pending"
		}
	} else {
		taskID = payload
		action = "pending"
	}
	return
}

func clearAssignment(state *domain.WorkerState) {
	state.CurrentTask = ""
	state.AssignedAt = 0
	resetWatchdog(state)
}

func markComplete(state *domain.WorkerState, idleWorkers *[]*domain.WorkerState) {
	clearAssignment(state)
	*idleWorkers = append(*idleWorkers, state)
}

func printStatus(workers map[string]*domain.WorkerState) {
	parts := make([]string, 0, len(workers))
	for name, state := range workers {
		label := "idle"
		if state.CurrentTask != "" {
			label = fmt.Sprintf("working(%s)", state.CurrentTask)
		}
		parts = append(parts, fmt.Sprintf("%s:%s", name, label))
	}
	now := time.Now().Format("15:04:05")
	slog.Debug("status", "time", now, "workers", parts)
}

// waitForNewTask blocks until a new task appears or timeout for orchestrator check.
// Returns true if a new task was detected, false if returning for periodic check.
func waitForNewTask(ctx context.Context, base string) bool {
	pendingDir := filepath.Join(base, ".jack-in/tasks/pending")
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		slog.Warn("failed to create fs watcher, falling back to poll", "error", err)
		select {
		case <-ctx.Done():
		case <-time.After(5 * time.Second):
		}
		return false
	}
	defer watcher.Close()

	if err := watcher.Add(pendingDir); err != nil {
		slog.Warn("failed to watch pending dir", "error", err)
		select {
		case <-ctx.Done():
		case <-time.After(5 * time.Second):
		}
		return false
	}

	// Return periodically so main loop can run orchestrator checks
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return false
		case <-ticker.C:
			// Return to main loop for orchestrator check
			return false
		case event, ok := <-watcher.Events:
			if !ok {
				return false
			}
			if event.Op&(fsnotify.Create|fsnotify.Write) != 0 {
				return true
			}
		case _, ok := <-watcher.Errors:
			if !ok {
				return false
			}
		}
	}
}
