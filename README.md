# JACKOPS

tmux-native multi-agent swarm orchestrator.

Spawns and coordinates AI coding agents (Claude Code, Codex, Opencode, Gemini) across git worktrees. Dashboard, task queue, review cycle, and optional mobile remote control -- all from the terminal.

## Why

Existing multi-agent tools either require a web UI (unleashd), are tightly coupled to one framework (pi-mono), or shell out to CLIs without visibility (oompa). JACKOPS stays in tmux where the work happens. You see every agent, can intervene anytime, and mix any CLI agent in the same swarm.

## Architecture

```
tmux session: jackops-<project>
+------------------------------------------------------------------+
|                                                                  |
|  [window 0: dashboard]   [window 1: w1]   [window 2: w2]  ...  |
|  +--------------------+  +--------------+  +--------------+     |
|  | task queue status  |  | claude       |  | codex        |     |
|  | worker status      |  | worktree: .w1|  | worktree: .w2|     |
|  | recent activity    |  |              |  |              |     |
|  +--------------------+  +--------------+  +--------------+     |
|                                |                |                |
|                          hooks/IPC        send-keys/capture      |
|                                |                |                |
|                          +-----+----------------+-----+          |
|                          |    orchestrator daemon     |          |
|                          |  - worker lifecycle       |          |
|                          |  - task assignment        |          |
|                          |  - event handling         |          |
|                          |  - review cycle           |          |
|                          +----------------------------+          |
|                                                                  |
+------------------------------------------------------------------+

Optional:
  orchestrator daemon <---> messaging bridge (Matrix/Telegram/Slack)
                            <---> your phone
```

### Key design decisions

- **tmux windows, not panes.** Each worker gets a named tmux window. Scale to 20+ workers without visual clutter. Attach to any worker with a keystroke.
- **Agent-agnostic.** Anything with a CLI works. No SDK lock-in.
- **Hooks for Claude, polling for others.** Claude Code hooks (via `--settings`) give reliable event-driven detection. Other agents use `tmux capture-pane` polling with completion markers.
- **Filesystem task queue.** `tasks/{pending,current,complete}/` -- simple, debuggable, works across worktrees. Inspired by oompa.
- **Git worktree isolation.** Each worker operates in its own worktree. Merge conflicts are resolved at review time, not during work.

## Phases

### Phase 1: Worker management

Spawn named tmux windows with agents in isolated git worktrees. Send prompts via `tmux send-keys`. Detect completion via Claude Code hooks (IPC socket) or `capture-pane` polling (other agents). Configuration file defines workers, their agent type, and role.

```yaml
# jackops.yaml
project: my-app
workers:
  - name: planner
    agent: claude
    model: opus
    role: planner
    count: 1
  - name: impl
    agent: codex
    model: codex-mini
    role: executor
    count: 2
  - name: reviewer
    agent: claude
    model: sonnet
    role: reviewer
    count: 1
```

### Phase 2: Task system

Filesystem-based task queue shared across worktrees:

```
tasks/
  pending/     # unclaimed tasks
  current/     # in progress (claimed by a worker)
  complete/    # done
  rejected/    # failed review
```

Workers claim tasks by moving files. Planners create tasks. Executors consume them. Simple, no database, inspectable with `ls`.

### Phase 3: Review cycle

Dedicated reviewer worker(s). After an executor completes a task:

1. Reviewer gets the diff (via temp file or git diff in the worktree)
2. Reviewer approves -> merge to main branch
3. Reviewer rejects -> task moves to `rejected/` with feedback, executor retries

### Phase 4: Dashboard and session reader

A tmux dashboard window showing:

- Worker status (alive/idle/working, current task, agent type)
- Task queue summary (pending/current/complete counts)
- Recent activity feed (last N events across all workers)

Session reader discovers past conversations from agent disk formats:

| Agent | Session path |
|-------|-------------|
| Claude Code | `~/.claude/projects/` |
| Codex | `~/.codex/sessions/` |
| Opencode | `~/.local/share/opencode/` |
| Gemini | `~/.gemini/tmp/` |

### Phase 5: Remote control (nice-to-have)

Messaging bridge for mobile notifications and remote input. When a worker needs human input (permission, question, stuck), send a notification. Reply from your phone, message gets injected back via `tmux send-keys`.

Platform TBD (Matrix, Telegram, or Slack).

## Reference projects

| Project | What we take from it | Repo |
|---------|---------------------|------|
| **oompa** | Task queue pattern, worktree isolation, worker roles (planner/executor/reviewer), swarm lifecycle | [nbardy/oompa](https://github.com/nbardy/oompa) |
| **unleashd** | Session discovery from disk, multi-provider adapter registry, project-based organization | [nbardy/unleashd](https://github.com/nbardy/unleashd) |
| **jackpoint** | Claude Code hooks for event detection, Unix socket IPC, `tmux send-keys` input injection, messaging bridge pattern | [artpi/jackpoint](https://github.com/artpi/jackpoint) |
| **pi-mono** | Clean agent SDK design, AGENTS.md conventions, multi-provider model abstraction | [badlogic/pi-mono](https://github.com/badlogic/pi-mono) |
| **orchestrate skill** | Basic tmux impl-review loop, temp file passing between agents, DONE-marker polling | Personal skill |

## Tech stack

Deno (TypeScript, no build step). See [ARCHITECTURE.md](ARCHITECTURE.md) for details.

## License

MIT
