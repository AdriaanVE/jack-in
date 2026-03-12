# LLM Orchestrator Agent — Implementation Plan

## Overview

Add an LLM orchestrator agent that runs in a dedicated tmux window alongside
workers. It acts as the brain of the swarm — reviewing completed work, rejecting
with feedback, creating follow-up tasks, and reporting progress.

## Architecture

```
daemon (mechanical)                 LLM orchestrator agent (judgment)
  - signal detection                  [tmux window: orchestrator]
  - task queue file moves             - polls jackops status --json
  - stall detection                   - reviews diffs in worker worktrees
  - permission evaluation             - approves or rejects tasks
  - moves current/ -> review/         - creates follow-up tasks
                                      - reports progress to user
```

### New task state: review/

Current: `pending/ -> current/ -> complete/` New:
`pending/ -> current/ -> review/ -> complete/ or rejected/`

The daemon moves completed tasks to `review/` instead of `complete/`. The
orchestrator agent picks up tasks from `review/`, verifies the work, and calls
`jackops tasks approve <id>` or `jackops tasks reject <id> <feedback>`.

## Implementation

### Phase 1: Task queue changes

#### New state: `review/`

Add `review` to `TASK_STATES` in `src/task-queue.ts`.

New transitions:

- `review(base, taskId)` — move `current/ -> review/`
- `approve(base, taskId)` — move `review/ -> complete/`
- `reject(base, taskId, feedback)` — update: move `review/ -> rejected/`

Update `init()` to create the `review/` directory. Update `counts()` and
`list()` to include `review` state.

#### Daemon change

In `src/daemon.ts` tick loop, when a task completes:

- Currently: `tq.complete(base, taskId)`
- New: `tq.review(base, taskId)`

One line change. The orchestrator agent handles the rest.

#### New CLI commands

```bash
jackops tasks approve <id>              # review/ -> complete/
jackops tasks reject <id> <feedback>    # review/ -> rejected/ with feedback
jackops status --json                   # structured output for orchestrator
```

### Phase 2: Structured status output

Add `--json` flag to `jackops status` that outputs:

```json
{
  "session": "jackops-my-app",
  "daemon": { "running": true },
  "workers": [
    {
      "name": "coder",
      "agent": "claude",
      "state": "working",
      "worktree": "/path/to/.w-my-app-coder",
      "branch": "jackops/my-app/coder",
      "currentTask": "task-1234"
    }
  ],
  "tasks": {
    "pending": 2,
    "current": 1,
    "review": 1,
    "complete": 3,
    "rejected": 0
  }
}
```

### Phase 3: Orchestrator agent

#### Instructions document: `docs/orchestrator-instructions.md`

The orchestrator agent receives:

- Static: orchestrator instructions (review process, how to approve/reject, when
  to create follow-up tasks)
- Dynamic: `jackops status --json` output, worktree paths, task queue state

#### Agent behavior

1. Run `jackops status --json` to get current state
2. Check for tasks in `review/` state
3. For each review task: a. Read the task file to get summary, description,
   acceptance criteria b. `cd` to the worker's worktree, run `git diff main` c.
   Optionally run tests (timeboxed, read-only) d. Evaluate: does the diff
   satisfy the acceptance criteria? e. `jackops tasks approve <id>` or
   `jackops tasks reject <id> "<feedback>"`
4. Check for stalled or stuck workers, report to user
5. Sleep/backoff, repeat

#### Backoff strategy

- Check every 30 seconds when tasks are in review/
- Back off to 2 minutes when nothing is in review/
- Wake immediately on new review/ task (via file watcher or version file)

#### Guardrails

- Token budget: orchestrator should be concise, not verbose
- Command allowlist: git diff, git log, test runners, jackops CLI only
- Context reset: if conversation gets too long, summarize and restart
- Read-only in worktrees: never modify worker worktree files

### Phase 4: Spawn in jackops up

In `src/cli.ts` `up()` function:

- After spawning workers, create an `orchestrator` tmux window
- Write the orchestrator prompt to `.jackops/orchestrator-prompt.md`
- Spawn the orchestrator agent (using same `initCommand()` pattern)
- Use the first available claude agent (orchestrator needs good reasoning)

Config option to disable:

```yaml
orchestrator:
  agent: true # spawn LLM orchestrator (default: true when tasks exist)
```

Or `--no-orchestrator-agent` flag on `jackops up`.

## Files to create/modify

### New files

| File                                | Purpose                       |
| ----------------------------------- | ----------------------------- |
| `docs/orchestrator-instructions.md` | LLM orchestrator instructions |

### Modified files

| File                      | Change                                                                                   |
| ------------------------- | ---------------------------------------------------------------------------------------- |
| `src/task-queue.ts`       | Add `review` state, `approve()`, update `reject()`                                       |
| `src/daemon.ts`           | Change `tq.complete()` to `tq.review()` on task completion                               |
| `src/cli.ts`              | Add `tasks approve`, `tasks reject` commands, `status --json`, spawn orchestrator window |
| `src/status.ts`           | Add `formatJsonStatus()` for `--json` output                                             |
| `test/task_queue_test.ts` | Tests for review state transitions                                                       |
| `test/daemon_test.ts`     | Update completion tests for review state                                                 |
| `test/status_test.ts`     | Tests for JSON status output                                                             |

## Milestones

1. Task queue: add `review/` state + `approve()`/`reject()` transitions + tests
2. Daemon: one-line change `complete()` -> `review()`
3. CLI: `tasks approve`, `tasks reject` subcommands
4. Status: `--json` flag with structured output
5. Orchestrator instructions document
6. Spawn orchestrator in `jackops up`
7. Integration test: full review cycle

## Edge cases

- **No orchestrator agent available**: if no claude is installed, skip
  orchestrator spawn and fall back to current behavior (auto-complete)
- **Orchestrator crashes**: daemon keeps running, tasks pile up in review/. Add
  a timeout: if task sits in review/ > 10 minutes, auto-approve with warning
- **All tasks already complete**: orchestrator has nothing to do, enters idle
  backoff loop
- **Rejection loop**: max_retries config prevents infinite reject/retry cycles
- **Large diffs**: truncate git diff output to avoid blowing context window
