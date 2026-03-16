# Plan: Orchestrator watchdog monitoring

## Problem

The daemon monitors workers via `tick()` -> `watchdog()` but completely ignores
the orchestrator agent. If the orchestrator crashes, gets stuck on a permission
prompt, or hangs, nobody notices. Workers keep running but no new task reviews
or assignments happen.

## Current architecture

- `WorkerState` tracks per-worker: current task, assigned-at timestamp, pane
  snapshots, heartbeat, LLM eval count, escalation flag
- `tick()` iterates `workers: Map<string, WorkerState>` (populated from
  `config.workers` with `role === "executor"`)
- `watchdog()` implements a 3-tier stall detection:
  - Tier 1 (0-60s): trust stop hook / heartbeat
  - Tier 2 (60-120s): pane snapshot diff, nudge if unchanged
  - Tier 3 (120s+): LLM eval (auto), Enter (yolo), or user notification (manual)
- The orchestrator agent runs in `${session}:dashboard-orchestrator.1` (bottom
  pane of the dashboard-orchestrator window) but has no `WorkerState` entry

## Design

Add the orchestrator as a special entry in the daemon's monitoring loop. Key
differences from workers:

| Aspect               | Worker                          | Orchestrator                    |
| -------------------- | ------------------------------- | ------------------------------- |
| Has a task queue     | Yes                             | No                              |
| Completion detection | Signal files + markers          | N/A                             |
| What "stalled" means | Not progressing on task         | Pane unchanged / process exited |
| Stall response       | Mode-dependent (see below)      | Mode-dependent (same)           |
| Tier 2 nudge         | "Output your completion marker" | N/A (no marker to output)       |

### What changes

1. **New `OrchestratorState` type** (subset of `WorkerState` fields):
   ```typescript
   interface OrchestratorState {
     name: "orchestrator";
     agent: string; // from config.orchestrator.agent
     lastPaneSnapshot: string | null;
     lastSnapshotAt: number | null;
     llmEvalCount: number;
     escalatedToUser: boolean;
     startedAt: number; // when daemon started (replaces assignedAt)
   }
   ```

2. **Initialize in `run()`** if `config.orchestrator.agent` is truthy:
   ```typescript
   const orchState: OrchestratorState | null = orchAgentType
     ? { name: "orchestrator", agent: orchAgentType, ... }
     : null;
   ```

3. **New `checkOrchestrator()` function** called from the main loop after
   `tick()`:
   - Captures pane `${session}:dashboard-orchestrator.1`
   - If capture fails (pane gone): log warning, notify user, set
     `escalatedToUser`
   - Pane snapshot diff: if unchanged for 2 consecutive ticks -> stall detected
   - No Tier 2 nudge (orchestrator has no completion marker to output)
   - Stall response follows approval mode:
     - **manual**: `tmux display-message` notifying user
     - **auto**: LLM eval on pane content — can approve permission prompts
     - **yolo**: send Enter blindly
   - Same `MAX_LLM_EVALS` / escalation-to-user guardrails as workers
   - Reset `escalatedToUser` when pane content changes (orchestrator recovered)

4. **Timing**: Use longer thresholds than workers since the orchestrator
   legitimately idles between reviews:
   - Skip watchdog entirely if pane changed since last snapshot (still active)
   - Start stall checks after `ORCH_STALL_TIMEOUT_MS` (e.g. 3 minutes) of no
     pane change — the orchestrator often waits for workers to finish
   - Wall time limit doesn't apply (orchestrator runs indefinitely)

5. **Agent-agnostic**: Works for Claude, Codex, or any orchestrator agent type.
   The only agent-specific behavior is in `auto` mode where the LLM evaluator
   already receives the agent type for context.

### What doesn't change

- Worker monitoring logic in `tick()` / `watchdog()` — untouched
- Task queue — orchestrator doesn't use it
- Signal files — orchestrator doesn't have `.done`/`.heartbeat`/`.needs-input`
- `WorkerState` interface — kept as-is for workers

### Call flow

```
run() main loop
  |
  +-> tick(session, base, workers, approval)       // existing worker monitoring
  |
  +-> checkOrchestrator(session, orchState, approval)  // new
  |
  +-> assign tasks to idle workers                 // existing
  |
  +-> sleep(interval)
```

## Scope

- `src/daemon.ts`: add `OrchestratorState`, `checkOrchestrator()`, wire into
  `run()` loop, add `ORCH_STALL_TIMEOUT_MS` constant
- `test/daemon_test.ts`: unit tests for `checkOrchestrator` (mocked pane
  capture)
- No changes to `up.ts`, `config.ts`, or hook scripts
