# 5 cmux-Inspired Improvements for jackops

Reference repo: `~/Code/agentic-coding/cmux/` -- a tmux-alternative terminal
multiplexer with deep AI agent integration (Unix socket control plane, 7 Claude
Code hooks, session store, rich notifications, per-workspace metadata).

This plan proposes 5 improvements to jackops based on patterns observed in cmux.
Recommended implementation order: 1 -> 2 -> 3 -> 4 -> 5.

---

## Improvement 1: Expanded Claude Code Hook Coverage

### Problem

jackops uses only 2 of Claude Code's 21 hook events:

- **Stop**: transcript-aware completion marker detection
- **PermissionRequest**: auto/yolo approval modes

This means jackops has no idea when a worker is actively working, waiting for
user input, about to execute a dangerous tool, or has been asked a question. The
daemon relies on passive pane scraping with 60s/120s timeouts to detect these
states.

### cmux inspiration

cmux uses 7 hooks (session-start, active, stop, idle, notification, session-end,
pre-tool-use) to maintain per-second state awareness. When Claude asks a
question via AskUserQuestion, cmux knows within milliseconds. When Claude is
running tools, cmux shows "Running" status. When Claude goes idle, cmux sends a
desktop notification.

### What to build

Add hooks for:

- **Notification** (idle_prompt, permission_prompt): write
  `.jackops/signals/<worker>.needs-input`
- **PreToolUse** (*): touch `.jackops/signals/<worker>.active` (heartbeat)
- **UserPromptSubmit**: clear needs-input, confirm worker is active
- **PostToolUse** (*): update heartbeat timestamp
- **SessionEnd**: detect worker crash/exit immediately

Daemon reads heartbeat freshness: if `.active` mtime < 30s, worker is definitely
alive (skip Tier 2 entirely). Daemon reads needs-input signal: if set, escalate
immediately instead of waiting 120s for Tier 3.

Also introduce a **jackops-agent-shim** concept: a lightweight wrapper script
that non-Claude agents can use to emit the same signal files, giving the daemon
a unified interface regardless of agent type.

### Impact

**High** -- for Claude workers (primary use case), this eliminates most watchdog
delay. Permission prompts detected in <1s instead of 120s. Active workers never
get false stall alerts.

### Complexity

**Low-Medium** -- hook entries in `buildClaudeSettings()` + new signal file
types + daemon tick changes. No new dependencies.

### Detailed plan

See: `docs/expanded-hooks-plan.md`

---

## Improvement 2: Structured Worker Identity via Environment Variables

### Problem

jackops tracks workers by tmux window name. If a window is renamed, reordered,
or if the daemon restarts, tracking breaks. Recovery requires re-scanning tmux
state by name matching, which is fragile.

The stop-hook receives worker name as a CLI argument, but there's no way for
arbitrary scripts or the worker itself to know "who am I in the jackops swarm."

### cmux inspiration

cmux injects `CMUX_WORKSPACE_ID`, `CMUX_SURFACE_ID`, `CMUX_SOCKET_PATH` into
every child shell. These environment variables propagate to all subprocesses.
Hooks use them to self-identify without arguments. The session store maps
session IDs to workspace/surface pairs for cross-hook data sharing.

### What to build

On worker spawn, set environment variables via tmux:

```
JACKOPS_WORKER_ID=<unique-id>
JACKOPS_WORKER_NAME=<name>
JACKOPS_PROJECT=<project-name>
JACKOPS_SESSION=<tmux-session-name>
JACKOPS_BASE=<project-root>
```

Write `.jackops/workers/<id>.json` metadata file per worker:

```json
{
  "id": "w-abc123",
  "name": "worker-1",
  "agent": "claude",
  "tmuxPane": "%42",
  "pid": 12345,
  "worktreePath": "/path/to/worktree",
  "assignedTask": "task-abc",
  "spawnedAt": 1710500000,
  "lastHeartbeat": 1710500300
}
```

Hooks read `JACKOPS_WORKER_NAME` from env instead of CLI args. Daemon reconciles
metadata files on restart. `jackops status` reads metadata for richer output.

### Impact

**High** -- robust tracking survives renames/restarts, enables richer status,
simplifies hook scripts.

### Complexity

**Low-Medium** -- spawn changes (tmux `set-environment` or `send-keys export`),
metadata file writes, hook arg simplification.

---

## Improvement 3: `tmux pipe-pane` Streaming for Near-Real-Time Detection

### Problem

jackops polls pane content every 5s via `tmux capture-pane`. Completion marker
detection depends on the marker being visible in the last 50 lines of scrollback
at poll time. If the agent outputs more than 50 lines after the marker, it's
missed entirely.

The Tier 2 pane-snapshot-diff compares two 50-line snapshots separated by the
poll interval. This is fragile: if the pane scrolls between snapshots, the diff
is always "changed" even if the agent is stuck.

### cmux inspiration

cmux gets push-based events via its Unix socket -- agents push data, cmux reacts
immediately. We can't add a socket to tmux, but `tmux pipe-pane` provides a
continuous output stream from each pane, piped to a file or process.

### What to build

On worker spawn:

```bash
tmux pipe-pane -t <target> -o 'cat >> .jackops/streams/<worker>.log'
```

Daemon monitors stream files:

- Use `Deno.watchFs()` on `.jackops/streams/` for file-change notifications
- On change, scan new bytes (track last-read offset) for completion marker
- Detection drops from 60s polling to <5s (or near-instant with watchFs)

Replace Tier 2 pane-snapshot-diff with stream-idle detection:

- If no new bytes in stream for N seconds, worker is idle
- No more comparing 50-line snapshots

Keep `capturePane()` as fallback for Tier 3 LLM evaluation only (needs visual
context of what's on screen, not full log).

Add cleanup on worker teardown:

- `tmux pipe-pane -t <target>` (no -o flag stops piping)
- Remove stream log files

### Impact

**High** -- fixes the biggest reliability pain point. Markers never missed
regardless of scrollback position. Stall detection becomes "no output for N
seconds" instead of "snapshot unchanged at two arbitrary points."

### Complexity

**Medium** -- new stream manager, offset tracking, cleanup on teardown, handle
log rotation for long-running workers.

---

## Improvement 4: Desktop Notifications with Focus-Aware Suppression

### Problem

When jackops detects a stall, completion, or error, it shows a tmux
`display-message` that disappears after ~5 seconds. If the user is in another
application (editor, browser), they miss it entirely. There's no sound, no
badge, no persistent indicator.

### cmux inspiration

cmux sends macOS desktop notifications via `UNUserNotificationCenter`, with
smart suppression: if the user is already looking at the right pane in the right
workspace, cmux skips the desktop notification to avoid spam. It also supports
custom sounds, dock badges, and pane ring highlights.

### What to build

Add `osascript -e 'display notification ...'` calls for high-value events:

- **Task completed**: "Worker-1 finished: <task summary>"
- **All tasks done**: "Swarm finished - all N tasks complete"
- **Worker stalled**: "Worker-1 needs attention - stuck on permission prompt"
- **Worker error/crash**: "Worker-1 crashed - check tmux"

Focus-aware suppression:

```bash
# Check if user's active tmux window is the stalled worker
ACTIVE_WINDOW=$(tmux display-message -p '#{window_name}')
if [ "$ACTIVE_WINDOW" != "$WORKER_WINDOW" ]; then
  osascript -e 'display notification "..." with title "jackops"'
fi
```

Configuration in `jackops.yaml`:

```yaml
orchestrator:
  notifications: true # default: true on macOS, false elsewhere
  notification_sound: true # optional terminal bell
```

Optional terminal bell (`\a`) for non-macOS systems.

### Impact

**Medium-High** -- closes the feedback loop for unattended operation. Essential
for the "start swarm, go work on something else" workflow.

### Complexity

**Low** -- `osascript` one-liners + tmux focus check. No new dependencies.

---

## Improvement 5: Per-Worker Health State Machine

### Problem

jackops worker health is binary: "pane is running a non-shell command" or not.
The status output shows `working`, `waiting`, `stopped`, or `gone` -- but these
are point-in-time snapshots with no history or degradation tracking.

The watchdog uses raw timers (60s, 120s) instead of semantic states. A worker
that's been actively outputting for 119s gets the same treatment as one that
went silent at second 1.

### cmux inspiration

cmux tracks multiple health dimensions per-workspace: socket connectable, accept
loop alive, socket path exists, path matches, process running. Combined with the
session store and PID tracking, it has rich health context that drives different
UI states (Running, Idle, Needs Input, Error).

### What to build

Define health states with clear transition rules:

```
active   -- heartbeat < 30s OR stream has new bytes in last 30s
idle     -- no output for 30-60s, process alive, no needs-input signal
stale    -- no output for 60-120s, no heartbeat
stuck    -- needs-input signal set, OR LLM eval says permission_prompt
dead     -- pane process exited or pane gone
```

State transitions drive watchdog behavior instead of raw timers:

- `active` -> do nothing, reset counters
- `idle` -> nudge for completion marker
- `stale` -> LLM evaluation
- `stuck` -> escalate + desktop notification
- `dead` -> mark task failed, notify user

Display in `jackops status` with visual indicators:

```
Workers:
  worker-1  [ACTIVE]  task-abc  "Add user validation"  (2m 15s)
  worker-2  [STUCK]   task-def  "Fix login bug"        (5m 30s) -- permission prompt
  worker-3  [IDLE]    --        waiting for task
```

Store health history for the orchestrator agent to review:

- `.jackops/health/<worker>.json` with last 10 state transitions + timestamps
- Orchestrator can use this to decide whether to restart a chronically stuck
  worker

### Impact

**Medium-High** -- makes the watchdog smarter, status output actionable, and
lays groundwork for self-healing (orchestrator restarts stuck workers).

### Complexity

**Medium** -- state machine logic, integrates with improvements 1 (hooks provide
heartbeat/needs-input signals) and 3 (stream provides activity data).

---

## Dependency Graph

```
Improvement 1 (Hooks)  ----+
                            |
Improvement 2 (Identity) --+--> Improvement 5 (Health States)
                            |
Improvement 3 (Streaming) -+

Improvement 4 (Notifications) -- independent, can parallelize
```

## Implementation Timeline

| Phase | Improvement               | Depends On | Effort       |
| ----- | ------------------------- | ---------- | ------------ |
| 1     | Expanded hooks (1)        | None       | 1-2 sessions |
| 2     | Worker identity (2)       | None       | 1 session    |
| 3     | Pipe-pane streaming (3)   | None       | 1-2 sessions |
| 4     | Desktop notifications (4) | None       | 0.5 session  |
| 5     | Health state machine (5)  | 1, 2, 3    | 2 sessions   |
