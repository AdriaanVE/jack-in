# Orchestration design spec

## Problem

JACKOPS currently spawns agents in parallel but they work independently. There
is no task assignment, no review cycle, no shared context. Each worker is a
standalone session with no awareness of the others.

## Approach: hybrid orchestration

Split orchestration into two layers:

1. **Daemon** (deterministic code) -- handles lifecycle, task routing,
   completion detection, merge mechanics. Reliable, predictable, debuggable.
2. **LLM agents** (planner, executor, reviewer roles) -- handle creative work:
   task decomposition, implementation, code review.

The daemon is the backbone. It never hallucinates, never loses track of state,
never forgets a task. LLM agents plug into it as workers with defined roles.

```
            jackops daemon
           /      |       \
          /       |        \
   planner    executor(s)    reviewer
   (LLM)       (LLM)         (LLM)
     |            |             |
     v            v             v
create tasks   claim+code    review diffs
     |            |             |
     v            v             v
tasks/pending  tasks/current  approve/reject
          \       |       /
           \      |      /
            jackops daemon
            (routes, merges, retries)
```

## Daemon responsibilities

### Task routing

The daemon watches the task queue filesystem and assigns work:

1. **Poll for idle workers** -- check tmux pane state (`pane_current_command`)
   or hook events. A worker is idle when its agent process has finished.
2. **Match tasks to workers** -- pending tasks go to idle executors. Completed
   tasks go to idle reviewers. Rejected tasks go back to executors (with
   feedback).
3. **Deliver tasks** -- `tmux send-keys` with a prompt that includes the task
   description, relevant files, and acceptance criteria. For multi-line prompts,
   write to a temp file and tell the agent to read it.

### Completion detection

Two strategies based on agent type:

**Event-driven (Claude Code):**

- Configure Claude Code hooks via `--settings` to fire on `Stop` events
- Hook script pings a Unix domain socket (`/tmp/jackops-<project>.sock`)
- Daemon listens on the socket, resolves the completion for that worker
- Most reliable -- no polling, no timing races

**Polling-based (Codex, Opencode, Gemini):**

- Instruct agents to echo a marker on completion (e.g.
  `echo JACKOPS_DONE_<task-id>`)
- Daemon polls `tmux capture-pane` on an interval (e.g. every 3s)
- Detect the marker in the output
- Less reliable -- buffer truncation, agents may not follow instructions

**Hybrid fallback:**

- If no marker detected within a timeout, check if the agent process has exited
  (pane is dead or shell is idle)
- Treat as "probably done" and prompt the reviewer with whatever was produced

### Merge mechanics

When a reviewer approves a task:

1. Daemon runs `git diff` in the executor's worktree to capture changes
2. Apply the diff to main branch (or a staging branch)
3. If conflicts: route back to executor with conflict context
4. If clean: commit, move task to `complete/`
5. Notify other workers that main has updated (optional: `git pull` in their
   worktrees)

When a reviewer rejects:

1. Daemon moves task to `rejected/` with reviewer feedback
2. Re-routes to an idle executor (same or different) with the original task +
   feedback
3. Retry counter prevents infinite loops (configurable max retries, default 2)

### State tracking

The daemon maintains an in-memory state object (persisted to
`.jackops/state.json` on change):

```typescript
interface DaemonState {
  project: string;
  session: string;
  startedAt: string;
  workers: WorkerState[];
  taskStats: {
    pending: number;
    current: number;
    complete: number;
    rejected: number;
  };
}

interface WorkerState {
  name: string;
  role: "planner" | "executor" | "reviewer";
  agent: AgentType;
  status: "idle" | "working" | "reviewing" | "gone";
  currentTask: string | null;
  completedTasks: number;
}
```

This powers the dashboard and lets the daemon resume after a restart.

## Worker roles

### Planner

- Receives the project spec (from `spec` field in config or initial prompt)
- Decomposes work into discrete tasks with clear acceptance criteria
- Writes tasks as JSON files to `tasks/pending/`
- Can create follow-up tasks as work progresses
- Only one planner (multiple would conflict on task decomposition)

The planner's prompt template:

```
You are a software architect. Your job is to decompose the following spec into
discrete, independently-implementable tasks.

For each task, create a JSON file in tasks/pending/ with this format:
{
  "id": "<short-id>",
  "summary": "<one line>",
  "description": "<detailed description>",
  "files": ["<likely files to change>"],
  "acceptance": ["<criteria for reviewer>"],
  "depends_on": []
}

Spec:
<contents of spec.md>
```

### Executor

- Claims tasks from `tasks/pending/` (daemon assigns, or self-serve)
- Implements the task in its own worktree
- Signals completion (marker echo or hook event)
- Multiple executors can work in parallel on different tasks

The executor's prompt template:

```
You are an implementation engineer. Complete the following task in this worktree.

Task: <summary>
Description: <description>
Files likely involved: <files>
Acceptance criteria:
<acceptance list>

When done, ensure all changes are committed and echo:
JACKOPS_DONE_<task-id>
```

### Reviewer

- Receives completed tasks with a `git diff` of the changes
- Evaluates against acceptance criteria
- Approves (daemon merges) or rejects (daemon re-routes with feedback)
- One reviewer is typical, but multiple can work for high throughput

The reviewer's prompt template:

```
You are a code reviewer. Review the following diff against the acceptance
criteria.

Task: <summary>
Acceptance criteria:
<acceptance list>

Diff:
<git diff output>

Respond with exactly one of:
APPROVED -- changes meet all criteria
REJECTED: <feedback> -- what needs to change
```

## Task queue

Filesystem-based, same as ARCHITECTURE.md. Tasks are JSON files moved between
directories via `Deno.rename` (atomic on POSIX).

```
tasks/
  pending/       # unclaimed, waiting for executor
  current/       # claimed, in progress
  complete/      # approved and merged
  rejected/      # failed review, waiting for retry
```

### Task lifecycle

```
planner creates -> pending/
                      |
              daemon assigns to executor
                      |
                   current/  (assignee set)
                      |
              executor completes
                      |
              daemon routes to reviewer
                      |
         approved?----+----rejected?
            |                   |
        complete/          rejected/  (feedback set)
        (daemon merges)         |
                        daemon re-routes
                                |
                           pending/  (retry)
```

### Task dependencies

Optional `depends_on` field. Daemon holds a task in `pending/` until all
dependencies are in `complete/`. Simple topological check on each routing pass.

## Configuration

Extends the current `jackops.yaml`:

```yaml
project: my-app
spec: spec.md # fed to planner as initial context

orchestrator:
  mode: hybrid # "manual" (current behavior) | "hybrid" (daemon)
  poll_interval: 3000 # ms, for polling-based completion detection
  max_retries: 2 # max review rejections before flagging for human
  auto_merge: true # merge approved tasks automatically
  hook_socket: /tmp/jackops-{project}.sock

workers:
  - name: planner
    agent: claude
    role: planner
    prompt: "decompose the spec into tasks"

  - name: impl-1
    agent: codex
    role: executor
    prompt: "implement assigned tasks"

  - name: impl-2
    agent: claude
    role: executor
    prompt: "implement assigned tasks"

  - name: reviewer
    agent: claude
    role: reviewer
    prompt: "review diffs against acceptance criteria"
```

When `orchestrator.mode` is `manual` (or omitted), JACKOPS behaves as it does
today -- just spawns workers with no daemon. This keeps the MVP functional.

## CLI changes

```bash
jackops up                   # spawn workers (manual mode: current behavior)
jackops up --orchestrate     # spawn workers + start daemon (hybrid mode)
jackops tasks                # list tasks and their status
jackops tasks add <summary>  # manually add a task to pending/
jackops logs <worker>        # tail a worker's recent activity
```

The daemon runs in the dashboard tmux window (window 0). Its stdout is the
dashboard view. Ctrl-C in the dashboard stops the daemon but leaves workers
running.

## Implementation order

1. **Task queue** (`src/task-queue.ts`) -- CRUD on JSON files, atomic moves,
   list/filter by state. Pure filesystem, no daemon needed yet.
2. **Completion detection** -- polling-based first (capture-pane + marker), hook
   IPC later. Add to existing `src/status.ts` or new `src/detect.ts`.
3. **Daemon loop** (`src/daemon.ts`) -- watch task queue, poll worker status,
   route tasks, trigger merges. Runs in dashboard window.
4. **Prompt templates** -- planner/executor/reviewer prompts with task context
   injection. Possibly `prompts/` directory with mustache-style templates.
5. **Merge mechanics** -- `git diff`, `git apply`, conflict detection. Extend
   `src/worktree.ts`.
6. **Hook IPC** -- Unix socket listener for Claude Code hooks. Upgrades
   polling-based detection to event-driven for Claude workers.

Each step is independently useful and testable. Step 1-3 gives a working hybrid
orchestrator. Steps 4-6 polish it.
