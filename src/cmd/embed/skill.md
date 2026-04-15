---
name: jackin
description: "Interact with jackin, a tmux-native multi-agent swarm orchestrator. Use this skill when managing workers, tasks, or the orchestrator daemon."
version: 0.2.0
metadata:
  when_to_use: "Use when the user mentions jackin, multi-agent swarm, worker management, task queue, orchestrator daemon, or when a jack-in.yaml is present in the project."
---

# jackin CLI Reference

jackin is a tmux-native multi-agent swarm orchestrator. It spawns AI coding
agents in isolated git worktrees and coordinates them via a task queue. A
mechanical daemon handles task assignment and signal detection, while an LLM
orchestrator agent reviews work and manages the task lifecycle.

## Commands

### Setup

```bash
jackin init                    # Interactive LLM-driven setup (generates jack-in.yaml)
jackin init --agent codex      # Use a specific agent for setup
jackin init --template         # Generate a commented template without LLM
```

### Swarm lifecycle

```bash
jackin up                      # Spawn workers + daemon + orchestrator agent
jackin up --no-orchestrator    # Spawn workers only (no daemon)
jackin up --no-orchestrator-agent  # Spawn workers + daemon but no LLM orchestrator
jackin up --approval auto      # Override approval mode
jackin down                    # Kill session and clean up worktrees
jackin status                  # Show worker status, daemon state, task counts
jackin status --json           # Structured JSON output (for orchestrator agent)
```

### Approval mode

```bash
jackin approval                    # Show current approval mode
jackin approval auto               # Switch to auto mode (live, no restart)
jackin approval manual             # Switch to manual mode
jackin approval yolo               # Switch to yolo mode
```

Switches approval mode while the swarm is running. Updates Claude worker
settings immediately (Claude reads settings live). Non-Claude agents (codex,
opencode, gemini) keep their original approval behavior -- live switching is not
supported for them. The daemon picks up the change on the next tick.

### Worker interaction

```bash
jackin send <worker> <message> # Send a message to a worker via tmux send-keys
jackin capture <worker>        # Capture worker pane output (last 50 lines)
jackin capture <worker> -n 100 # Capture last 100 lines
jackin capture orchestrator    # Capture orchestrator pane
jackin attach <worker>         # Switch to a worker's tmux window
jackin attach orchestrator     # Switch to the orchestrator agent window
jackin reset <worker>          # Kill and respawn a worker (fresh agent, same prompt)
jackin refresh <worker>        # Reset worktree to target branch (discard changes)
```

### Task management

```bash
jackin tasks                   # List all tasks with state counts
jackin tasks init              # Create task queue directories
jackin tasks add <summary>     # Add a task to the pending queue
jackin tasks add "fix auth" --desc "Refactor the auth module to use JWT"
jackin tasks complete <id>     # Force current -> review (unstick a task)
jackin tasks approve <id>      # Approve a reviewed task (review -> complete)
jackin tasks reject <id> <feedback>  # Reject with feedback (review -> rejected)
jackin tasks retry <id>        # Re-assign to same worker with feedback (worktree preserved)
jackin tasks drop <id>         # Permanently delete a rejected task
```

### Daemon

```bash
jackin daemon                  # Start orchestrator (usually started by `up`)
jackin daemon --approval yolo  # Override approval mode
```

## Configuration (jack-in.yaml)

```yaml
project: my-app        # Required. Alphanumeric, hyphens, underscores.
branch: jackin-develop # Optional. Target branch for merging (default: jackin-develop).

workers: # Required. At least one worker.
  - agent: claude # claude | codex | opencode | gemini
    prompt: "implement features" # What this worker should do
    role: executor # executor | reviewer | planner
    # name: worker-1 # Optional. Auto-assigned as worker-1..worker-5 if omitted

orchestrator: # Optional
  poll_interval: 5000 # ms (default: 5000)
  max_retries: 2 # default: 2
  approval: auto # manual | auto | yolo (default: manual)
  agent: claude # LLM orchestrator agent type, or false to disable (default: claude)

tasks: # Optional. Seeded on `jackin up`.
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
`jackin tasks add` or programmatically, use these fields:

| Field         | Required | Description                                          |
| ------------- | -------- | ---------------------------------------------------- |
| `summary`     | yes      | One-line task description                            |
| `description` | no       | Detailed description (defaults to summary)           |
| `files`       | no       | Hint files/directories for the worker                |
| `acceptance`  | no       | Completion criteria (list of strings)                |
| `depends_on`  | no       | Task summaries this depends on (resolved to IDs)     |
| `feedback`    | no       | Rejection feedback (set by reject, cleared on retry) |

### Task examples in jack-in.yaml

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
jackin tasks add "Fix the login form validation"

# With description
jackin tasks add "Migrate database schema" --desc "Add created_at and updated_at columns to users table. Write up and down migrations."
```

## Architecture

- **Daemon** (mechanical): assigns pending tasks to idle workers, detects
  completion via signal files, moves tasks to review. Does not make judgments.
- **Orchestrator agent** (LLM): reviews diffs, approves/rejects tasks, creates
  follow-up tasks, unsticks workers when signal detection fails. Primary goals:
  preserve worker output (never lose completed work) and prevent duplicate effort.
- **Workers**: AI agents in isolated git worktrees, each in a tmux window.

### Task lifecycle

```
pending/ -> current/ -> review/ -> complete/
                          |
                          +-> rejected/ -+-> pending/ (retry)
                                         +-> [deleted] (drop)
```

Rejected tasks stay in `rejected/` until the user or orchestrator decides to
`retry` (back to pending) or `drop` (permanently delete). The worker that
produced the rejected work stays **blocked** so you can inspect its worktree.
Call `retry` or `drop` to unblock the worker and reset its worktree.

Task queue is filesystem-based:
`.jack-in/tasks/{pending,current,review,complete,rejected}/`

Signal files in `.jack-in/signals/` track worker completion.

## Worker environment

If you are working in a git worktree (your path contains `.w-`), keep in mind:

- **Your branch** is `jackin/<project>/<worker>`. Stay on it — don't switch
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
tmux attach -t jackin-<project>   # Attach to session
Ctrl-b w                           # List all windows
```

Window 0 is `dashboard-orchestrator` with two panes: the daemon (top, pane 0)
and the orchestrator agent (bottom, pane 1). Workers are windows 1+.

### Running applications and servers

Use the **CLI tmux window** to run applications, webservers, or long-running
processes. This keeps your worker pane free for agent interaction and prevents
blocking.

```bash
# Create/access the CLI window (from TUI: press 'c', or via tmux)
tmux select-window -t jackin-<project>:cli

# Run your app in the CLI window
tmux send-keys -t jackin-<project>:cli 'npm run dev' Enter

# Capture output if something fails (useful for debugging)
tmux capture-pane -t jackin-<project>:cli -p -S -100
```

If a build or server fails, capture the pane output to diagnose the error:

```bash
# Capture last 200 lines from CLI window
tmux capture-pane -t jackin-<project>:cli -p -S -200 > /tmp/cli-output.txt
```

**Best practices:**
- Run servers and watch processes in the CLI window, not in your worker pane
- Capture pane output when errors occur — it contains the full error trace
- Stop previous processes before starting new ones (`Ctrl-C` or kill the process)

## Monitoring

When the swarm is running with tasks, regularly run `jackin status` to check on
progress. Report worker states and task counts to the user, and flag anything
that looks stuck. Keep checking until all tasks are complete or the user says to
stop.
