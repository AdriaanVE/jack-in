---
name: jackops
description: "Interact with jackops, a tmux-native multi-agent swarm orchestrator. Use this skill when managing workers, tasks, or the orchestrator daemon."
version: 0.1.0
metadata:
  when_to_use: "Use when the user mentions jackops, multi-agent swarm, worker management, task queue, orchestrator daemon, or when a jackops.yaml is present in the project."
---

# jackops CLI Reference

jackops is a tmux-native multi-agent swarm orchestrator. It spawns AI coding
agents in isolated git worktrees and coordinates them via a task queue.

## Commands

### Setup

```bash
jackops init                    # Interactive LLM-driven setup (generates jackops.yaml)
jackops init --agent codex      # Use a specific agent for setup
jackops init --template         # Generate a commented template without LLM
```

### Swarm lifecycle

```bash
jackops up                      # Spawn workers in tmux + start orchestrator daemon
jackops up --no-orchestrator    # Spawn workers only (manual approval)
jackops up --approval auto      # Override approval mode
jackops down                    # Kill session and clean up worktrees
jackops status                  # Show worker status, daemon state, task counts
```

### Worker interaction

```bash
jackops send <worker> <message> # Send a message to a worker via tmux send-keys
jackops attach <worker>         # Switch to a worker's tmux window
```

### Task management

```bash
jackops tasks                   # List all tasks with state counts
jackops tasks init              # Create task queue directories
jackops tasks add <summary>     # Add a task to the pending queue
jackops tasks add "fix auth" --desc "Refactor the auth module to use JWT"
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
  approval: manual # manual | auto | yolo (default: manual)

tasks: # Optional. Seeded on `jackops up`.
  - summary: "Explore the codebase"
  - summary: "Add tests"
    depends_on: ["Explore the codebase"]
```

## Approval modes

| Mode     | Behavior                                          |
| -------- | ------------------------------------------------- |
| `manual` | Workers pause on permission prompts. You approve. |
| `auto`   | LLM evaluates safety. Safe ops auto-approved.     |
| `yolo`   | Everything auto-approved. Workers never pause.    |

## Worker roles

| Role       | Purpose                                      |
| ---------- | -------------------------------------------- |
| `executor` | Picks up tasks and implements them.          |
| `planner`  | Breaks goals into tasks in the queue.        |
| `reviewer` | Reviews completed work. Approves or rejects. |

## Architecture

- Each worker runs in its own tmux window and git worktree
- Task queue is filesystem-based:
  `.jackops/tasks/{pending,current,complete,rejected}/`
- Signal files in `.jackops/signals/` track worker completion
- The daemon polls workers, assigns tasks, handles stalls and permissions

## tmux session

```
tmux attach -t jackops-<project>   # Attach to session
Ctrl-b w                           # List all windows
```

Window 0 is the dashboard (daemon runs here). Workers are windows 1+.

## Monitoring

When the swarm is running with tasks, regularly run `jackops status` to check on
progress. Report worker states and task counts to the user, and flag anything
that looks stuck. Keep checking until all tasks are complete or the user says to
stop.
