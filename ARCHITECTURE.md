# Architecture

## Tech stack

**Deno** (TypeScript, no build step, `deno compile` for single-binary distribution).

Why Deno:
- Native TypeScript -- no tsc/tsx/tsup pipeline
- No `node_modules` -- URL imports or `deno.json` import map
- `Deno.Command` for process spawning (tmux, git, agent CLIs)
- YAML parser in stdlib (`@std/yaml`)
- `deno compile` produces a self-contained binary
- Explicit permissions (`--allow-run`, `--allow-read`, `--allow-write`) -- good hygiene for a tool that spawns arbitrary processes

## Project structure

```
jackops/
  src/
    cli.ts                  # Entry point, argument parsing
    config.ts               # Parse jackops.yaml
    orchestrator.ts         # Main loop: spawn workers, assign tasks, handle events
    tmux.ts                 # tmux primitives (new-session, send-keys, capture-pane, etc.)
    worktree.ts             # git worktree create/remove/list
    task-queue.ts           # Filesystem task queue (read/write/move task files)
    dashboard.ts            # Render dashboard content to tmux window
    session-reader.ts       # Read agent session files from disk
    harnesses/
      types.ts              # Common harness interface
      claude.ts             # Claude Code: hooks + IPC socket
      codex.ts              # Codex: send-keys + capture-pane polling
      opencode.ts           # Opencode: send-keys + capture-pane polling
      gemini.ts             # Gemini: send-keys + capture-pane polling
    bridge/                 # Phase 5: messaging bridge (optional)
      types.ts
      matrix.ts
      telegram.ts
  tasks/                    # Runtime task queue (created per-project, not committed)
  deno.json                 # Import map, tasks, permissions
  jackops.example.yaml      # Example configuration
```

## Component details

### tmux.ts -- tmux primitives

Thin typed wrapper around tmux commands. Everything goes through `Deno.Command`.

```
tmux.createSession(name)           -> tmux new-session -d -s <name>
tmux.createWindow(session, name)   -> tmux new-window -t <session> -n <name>
tmux.sendKeys(target, text)        -> tmux send-keys -t <target> '<text>' C-m
tmux.capturePane(target, lines)    -> tmux capture-pane -t <target> -p -S -<lines>
tmux.killWindow(target)            -> tmux kill-window -t <target>
tmux.listWindows(session)          -> tmux list-windows -t <session> -F '...'
tmux.hasSession(name)              -> tmux has-session -t <name>
```

No abstraction beyond typing and error handling. One function per tmux command.

### worktree.ts -- git worktree management

```
worktree.create(base, name)        -> git worktree add .w-<name> -b jackops/<name>
worktree.remove(name)              -> git worktree remove .w-<name>
worktree.list()                    -> git worktree list --porcelain
worktree.cleanup()                 -> remove all .w-* worktrees
```

Worktrees are named `.w-<worker-name>` and live in the project root. Branches are prefixed `jackops/` to avoid collisions.

### harnesses/ -- agent communication

Each agent CLI has different capabilities. The harness interface normalizes them:

```typescript
interface Harness {
  // Spawn the agent CLI in a tmux window
  spawn(window: string, cwd: string, prompt: string): Promise<void>;

  // Send a follow-up message to a running agent
  send(window: string, message: string): Promise<void>;

  // Wait for the agent to finish its current turn
  waitForCompletion(window: string, signal: AbortSignal): Promise<CompletionEvent>;

  // Read the agent's last response
  readResponse(window: string): Promise<string>;
}
```

**Claude Code harness** (event-driven):
- Spawns `claude` with `--settings` pointing to hook config
- Hooks fire on `Stop`, `PreToolUse/AskUserQuestion`, `Notification`
- Hooks ping a Unix domain socket (IPC) -- same pattern as jackpoint
- `waitForCompletion` resolves when a `Stop` event arrives on the socket
- Most reliable harness -- no polling needed

**Codex / Opencode / Gemini harness** (polling-based):
- Spawns agent CLI via `tmux send-keys`
- Workers are instructed to echo a completion marker (e.g. `JACKOPS_DONE_<task-id>`)
- `waitForCompletion` polls `tmux capture-pane` for the marker
- `readResponse` captures pane content between start/end markers
- Less reliable -- buffer truncation, special chars, timing races

### task-queue.ts -- filesystem task queue

Tasks are JSON files in `tasks/{pending,current,complete,rejected}/`.

```typescript
interface Task {
  id: string;
  summary: string;
  description: string;
  files?: string[];          // hint: which files are likely involved
  acceptance?: string[];     // criteria for reviewer to check
  assignee?: string;         // worker name (set on claim)
  feedback?: string;         // reviewer feedback (set on rejection)
  createdBy?: string;        // worker name that created the task
}
```

Operations are atomic file moves (`Deno.rename`):
- **Claim**: `pending/<id>.json` -> `current/<id>.json` (set assignee)
- **Complete**: `current/<id>.json` -> `complete/<id>.json`
- **Reject**: `current/<id>.json` -> `rejected/<id>.json` (set feedback)
- **Retry**: `rejected/<id>.json` -> `pending/<id>.json`

Race condition on claim: two workers move the same file. One gets `ENOENT`. That worker picks the next task. Simple, no locks needed.

### orchestrator.ts -- main loop

The orchestrator is the brain. It runs as a long-lived Deno process.

```
1. Parse jackops.yaml
2. Create tmux session: jackops-<project>
3. For each worker in config:
   a. Create git worktree
   b. Create tmux window
   c. Spawn agent via harness
4. Enter main loop:
   a. Check for idle workers (completed their task)
   b. Assign pending tasks to idle executors
   c. Route completed tasks to reviewer
   d. Handle reviewer verdicts (merge or reject)
   e. Refresh dashboard window
   f. Handle external events (IPC from hooks, messaging bridge)
5. Exit when: all tasks complete, or user sends kill signal
```

### session-reader.ts -- agent session discovery

Reads past conversations from each agent's disk format. Inspired by unleashd's disk adapter pattern, but simpler -- read-only, no WebSocket, just parse and display.

| Agent | Format | Path |
|-------|--------|------|
| Claude Code | JSONL | `~/.claude/projects/<hash>/*.jsonl` |
| Codex | JSONL | `~/.codex/sessions/YYYY/MM/DD/*.jsonl` |
| Opencode | JSON | `~/.local/share/opencode/sessions/*/` |
| Gemini | JSON | `~/.gemini/tmp/session-*.json` |

Used by the dashboard to show recent activity and by reviewers to understand worker history.

### dashboard.ts -- tmux dashboard

The dashboard is window 0 in the jackops tmux session. It renders a plain-text status view that refreshes on a timer or on events.

```
JACKOPS -- my-app                              uptime: 12m
================================================================

WORKERS
  planner    claude/opus     idle        --
  impl-1     codex/mini      working     task-003: Add auth middleware
  impl-2     codex/mini      working     task-004: Write auth tests
  reviewer   claude/sonnet   reviewing   task-002: Setup database schema

TASKS
  pending: 3    current: 3    complete: 5    rejected: 1

RECENT ACTIVITY
  11:42  impl-1     completed   task-001: Initialize project structure
  11:43  reviewer   approved    task-001 -> merged to main
  11:44  impl-2     claimed     task-004: Write auth tests
  11:45  planner    created     task-006: Add rate limiting
```

Rendered by writing to a temp file and `cat`-ing it in the dashboard tmux window, or by using ANSI escape codes directly via `send-keys`.

## Data flow

```
                    jackops.yaml
                         |
                         v
                   orchestrator
                   /    |    \
                  /     |     \
           planner   executor(s)   reviewer
              |         |             |
              v         v             v
         tasks/     worktree      git diff
        pending/    send-keys     approve/reject
                    capture-pane
                         |
                         v
                    tasks/complete/
                    git merge
```

## IPC protocol (Claude Code hooks)

Same pattern as jackpoint. Claude Code hooks are configured via `--settings` to run a small script that forwards events over a Unix domain socket.

```
Claude Code  -->  hook script  -->  Unix socket  -->  orchestrator
                  (hook-ping)       /tmp/jackops-<pid>.sock
```

Hook events:

| Event | Meaning |
|-------|---------|
| `Stop` | Agent finished its turn, waiting for input |
| `PreToolUse:AskUserQuestion` | Agent is asking the user a question |
| `Notification:idle_prompt` | Agent has been idle |

The orchestrator listens on the socket and resolves the corresponding `waitForCompletion` promise.

## Configuration

```yaml
# jackops.yaml
project: my-app
spec: spec.md                    # optional: fed to planner as initial context

workers:
  - name: planner
    agent: claude
    model: opus
    role: planner
    count: 1
    prompt: prompts/planner.md   # optional: custom system prompt

  - name: impl
    agent: codex
    model: codex-mini
    role: executor
    count: 2
    prompt: prompts/executor.md

  - name: reviewer
    agent: claude
    model: sonnet
    role: reviewer
    count: 1
    prompt: prompts/reviewer.md

# Optional: messaging bridge for remote control
bridge:
  platform: telegram             # or matrix, slack
  config: ~/.jackops/bridge.json
```
