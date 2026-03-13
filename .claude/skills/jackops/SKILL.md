---
name: jackops
description: "Interact with jackops, a tmux-native multi-agent swarm orchestrator. Use this skill when managing workers, tasks, or the orchestrator daemon."
version: 0.2.0
metadata:
  when_to_use: "Use when the user mentions jackops, multi-agent swarm, worker management, task queue, orchestrator daemon, or when a jackops.yaml is present in the project."
---

# jackops CLI Reference

jackops is a tmux-native multi-agent swarm orchestrator. It spawns AI coding
agents in isolated git worktrees and coordinates them via a task queue. A
mechanical daemon handles task assignment and signal detection, while an LLM
orchestrator agent reviews work and manages the task lifecycle.

## Commands

### Setup

```bash
jackops init                    # Interactive LLM-driven setup (generates jackops.yaml)
jackops init --agent codex      # Use a specific agent for setup
jackops init --template         # Generate a commented template without LLM
```

### Swarm lifecycle

```bash
jackops up                      # Spawn workers + daemon + orchestrator agent
jackops up --no-orchestrator    # Spawn workers only (no daemon)
jackops up --no-orchestrator-agent  # Spawn workers + daemon but no LLM orchestrator
jackops up --approval auto      # Override approval mode
jackops down                    # Kill session and clean up worktrees
jackops status                  # Show worker status, daemon state, task counts
jackops status --json           # Structured JSON output (for orchestrator agent)
```

### Worker interaction

```bash
jackops send <worker> <message> # Send a message to a worker via tmux send-keys
jackops attach <worker>         # Switch to a worker's tmux window
jackops attach orchestrator     # Switch to the orchestrator agent window
```

### Task management

```bash
jackops tasks                   # List all tasks with state counts
jackops tasks init              # Create task queue directories
jackops tasks add <summary>     # Add a task to the pending queue
jackops tasks add "fix auth" --desc "Refactor the auth module to use JWT"
jackops tasks complete <id>     # Force current -> review (unstick a task)
jackops tasks approve <id>      # Approve a reviewed task (review -> complete)
jackops tasks reject <id> <feedback>  # Reject with feedback (review -> rejected)
```

### Daemon

```bash
jackops daemon                  # Start orchestrator (usually started by `up`)
jackops daemon --approval yolo  # Override approval mode
```

## Configuration (jackops.yaml)

```yaml
project: my-app # Required. Alphanumeric, hyphens, underscores.

workers: # Required. At least one worker.
  - name: coder # Unique name per worker
    agent: claude # claude | codex | opencode | gemini
    prompt: "implement features" # What this worker should do
    role: executor # executor | reviewer | planner

orchestrator: # Optional
  poll_interval: 5000 # ms (default: 5000)
  max_retries: 2 # default: 2
  approval: auto # manual | auto | yolo (default: manual)
  agent: claude # LLM orchestrator agent type, or false to disable (default: claude)

tasks: # Optional. Seeded on `jackops up`.
  - summary: "Explore the codebase"
  - summary: "Add tests"
    depends_on: ["Explore the codebase"]
```

## Approval modes

| Mode     | Behavior                                          |
| -------- | ------------------------------------------------- |
| `auto`   | LLM evaluates safety. Safe ops auto-approved.     |
| `manual` | Workers pause on permission prompts. You approve. |
| `yolo`   | Everything auto-approved. Workers never pause.    |

## Worker roles

| Role       | Purpose                                      |
| ---------- | -------------------------------------------- |
| `executor` | Picks up tasks and implements them.          |
| `planner`  | Breaks goals into tasks in the queue.        |
| `reviewer` | Reviews completed work. Approves or rejects. |

## Task format

Tasks are JSON files in the filesystem queue. When creating tasks via
`jackops tasks add` or programmatically, use these fields:

| Field         | Required | Description                                          |
| ------------- | -------- | ---------------------------------------------------- |
| `summary`     | yes      | One-line task description                            |
| `description` | no       | Detailed description (defaults to summary)           |
| `files`       | no       | Hint files/directories for the worker                |
| `acceptance`  | no       | Completion criteria (list of strings)                |
| `depends_on`  | no       | Task summaries this depends on (resolved to IDs)     |
| `feedback`    | no       | Rejection feedback (set by reject, cleared on retry) |

### Task examples in jackops.yaml

```yaml
tasks:
  # Simple task — just a summary
  - summary: "Explore the codebase and document architecture"

  # Task with description and files hint
  - summary: "Add input validation to API endpoints"
    description: "Add zod schemas for request bodies in all POST/PUT handlers. Return 400 with field-level errors."
    files:
      - src/routes/
      - src/schemas/

  # Task with acceptance criteria
  - summary: "Add unit tests for auth module"
    acceptance:
      - "All public functions have tests"
      - "Edge cases: expired tokens, malformed JWTs, missing claims"
      - "Tests pass with deno test"

  # Task with dependency (uses summary string, resolved to ID at seed time)
  - summary: "Refactor database queries to use connection pool"
    depends_on: ["Explore the codebase and document architecture"]

  # Full task
  - summary: "Implement rate limiting middleware"
    description: "Add sliding-window rate limiting. 100 req/min per IP. Use Redis if available, fall back to in-memory."
    files:
      - src/middleware/
      - src/config.ts
    acceptance:
      - "Rate limit headers in responses (X-RateLimit-*)"
      - "429 response when limit exceeded"
      - "Tests cover both Redis and in-memory backends"
    depends_on: ["Add input validation to API endpoints"]
```

### Adding tasks via CLI

```bash
# Minimal
jackops tasks add "Fix the login form validation"

# With description
jackops tasks add "Migrate database schema" --desc "Add created_at and updated_at columns to users table. Write up and down migrations."
```

## Architecture

- **Daemon** (mechanical): assigns pending tasks to idle workers, detects
  completion via signal files, moves tasks to review. Does not make judgments.
- **Orchestrator agent** (LLM): reviews diffs, approves/rejects tasks, creates
  follow-up tasks, unsticks workers when signal detection fails.
- **Workers**: AI agents in isolated git worktrees, each in a tmux window.

### Task lifecycle

```
pending/ -> current/ -> review/ -> complete/
                          |
                          +-> rejected/ -> pending/ (retry)
```

Task queue is filesystem-based:
`.jackops/tasks/{pending,current,review,complete,rejected}/`

Signal files in `.jackops/signals/` track worker completion.

## Worker environment

If you are working in a git worktree (your path contains `.w-`), keep in mind:

- **Your branch** is `jackops/<project>/<worker>`. Stay on it — don't switch
  branches. The daemon and orchestrator expect you here.
- **Only committed files are present.** Untracked files from the main checkout
  (`.env`, build artifacts, generated code) won't exist here.
- **Dependencies aren't shared.** `node_modules/`, `.venv/`, etc. need to be
  installed in this worktree. If builds or tests fail, run the project's install
  command first (check `README.md`).
- **Commit your work** to your branch. The orchestrator reviews via
  `git diff main` from your worktree.
- **Other workers can't see your changes** until they're merged. Each worker has
  its own worktree and branch.

## tmux session

```
tmux attach -t jackops-<project>   # Attach to session
Ctrl-b w                           # List all windows
```

Window 0 is the dashboard (daemon runs here). Workers are windows 1+. The
orchestrator agent runs in the `orchestrator` window.

## Monitoring

When the swarm is running with tasks, regularly run `jackops status` to check on
progress. Report worker states and task counts to the user, and flag anything
that looks stuck. Keep checking until all tasks are complete or the user says to
stop.
