# Expanded Claude Code Hooks + Agent Shim -- Implementation Plan

## Summary

Expand jackin from 2 Claude Code hooks (Stop, PermissionRequest) to 7 hooks,
and introduce a lightweight `notify-hook.sh` shim that non-Claude agents can
call to emit the same signal files. This gives the daemon push-based, sub-second
awareness of worker state instead of relying on 60s/120s polling timeouts.

## Current State

### Hooks in use today

| Event             | Hook Script                                            | Purpose                                                                                  |
| ----------------- | ------------------------------------------------------ | ---------------------------------------------------------------------------------------- |
| Stop              | `stop-hook.ts`                                         | Reads transcript, checks for `JACKIN_TASK_COMPLETE:<id>` marker, touches `.done` signal |
| PermissionRequest | `permission-eval.sh` (auto) / `yolo-approve.sh` (yolo) | Auto-approve or blind-approve permission prompts                                         |

### Signal files today

| File                             | Written by                        | Read by      | Meaning                               |
| -------------------------------- | --------------------------------- | ------------ | ------------------------------------- |
| `.jack-in/signals/<worker>.done` | stop-hook.ts / daemon marker scan | daemon tick  | Worker finished its task              |
| `.jack-in/current-task/<worker>` | daemon on assignment              | stop-hook.ts | Current task ID for marker validation |
| `.jack-in/approval-mode`         | `jackin approval` CLI            | daemon tick  | Runtime approval mode override        |

### Pain points addressed

1. **Permission prompts take 120s to detect** -- daemon waits for Tier 3 LLM
   eval before noticing a worker is blocked on a permission prompt. The
   Notification hook fires instantly when Claude shows a permission dialog.

2. **No heartbeat** -- daemon can't distinguish "actively working" from
   "frozen/crashed." It waits 60s (Tier 2) before even checking. PreToolUse
   fires on every tool call, providing a natural heartbeat.

3. **Worker crash goes unnoticed** -- if Claude exits (segfault, OOM, user ^C),
   the daemon only notices via Tier 2/3 pane snapshot checks. SessionEnd fires
   immediately.

4. **Idle detection is slow** -- Notification(idle_prompt) fires the instant
   Claude finishes and goes idle. Today this takes 60s+ to detect.

5. **Non-Claude agents are second-class** -- Codex, OpenCode, Gemini have no
   hook system. They rely entirely on pane scraping and `touch <signal>`
   instructions. A simple shim script bridges this gap.

---

## Proposed New Hooks

### Hook 1: Notification (idle_prompt, permission_prompt)

**Claude Code event**: `Notification` -- fires when Claude sends a notification
to the user.

**Matchers**: `idle_prompt`, `permission_prompt`, `elicitation_dialog`

**What it does**: Writes a needs-input signal file so the daemon knows instantly
that the worker is blocked and needs human attention.

**Signal file**: `.jack-in/signals/<worker>.needs-input`

**Hook script**: `hooks/notification-hook.sh`

```bash
#!/usr/bin/env bash
# Notification hook -- signals that the worker needs input.
# Args: <signal_dir> <worker_name>
# Stdin: JSON with notification_type, message, etc.

SIGNAL_DIR="$1"
WORKER_NAME="$2"

if [ -z "$SIGNAL_DIR" ] || [ -z "$WORKER_NAME" ]; then
  exit 0
fi

# Write needs-input signal with notification type as content
INPUT=$(cat)
NOTIFICATION_TYPE=$(echo "$INPUT" | grep -o '"notification_type"[[:space:]]*:[[:space:]]*"[^"]*"' | head -1 | sed 's/.*"notification_type"[[:space:]]*:[[:space:]]*"//;s/".*//')

echo "${NOTIFICATION_TYPE:-unknown}" > "${SIGNAL_DIR}/${WORKER_NAME}.needs-input"
```

**Daemon behavior on `.needs-input` signal**:

- If approval == "auto": trigger immediate LLM pane evaluation (skip Tier 2
  wait)
- If approval == "yolo": send Enter keystroke immediately
- If approval == "manual": show tmux message + desktop notification immediately
- Clear the signal after handling

**Impact**: Permission prompts detected in <1s instead of 120s. Idle detection
in <1s instead of 60s.

### Hook 2: PreToolUse (*) -- Heartbeat

**Claude Code event**: `PreToolUse` -- fires before every tool execution.

**What it does**: Touches a heartbeat file so the daemon knows the worker is
actively running tools. This is a lightweight "I'm alive" signal.

**Signal file**: `.jack-in/signals/<worker>.heartbeat`

**Hook script**: `hooks/heartbeat-hook.sh`

```bash
#!/usr/bin/env bash
# Heartbeat hook -- touch file to prove worker is active.
# Args: <signal_dir> <worker_name>

SIGNAL_DIR="$1"
WORKER_NAME="$2"

if [ -z "$SIGNAL_DIR" ] || [ -z "$WORKER_NAME" ]; then
  exit 0
fi

touch "${SIGNAL_DIR}/${WORKER_NAME}.heartbeat"
```

**Daemon behavior**:

- On each tick, check heartbeat mtime:
  - If < 30s old: worker is confirmed active, **skip Tier 2 entirely**
  - If 30-60s old: worker may be idle but was recently active, proceed
    cautiously
  - If > 60s old or missing: fall through to normal Tier 2/3 behavior
- **Progress deadline**: even with fresh heartbeats, if total task wall time
  exceeds `MAX_TASK_WALL_MS` (default 15 min), Tier 3 LLM eval still fires.
  Prevents a worker that is alive but making no real progress from running
  forever.
- This prevents false stall alerts for workers actively running long tool
  sequences

**Impact**: Eliminates false positives from Tier 2 for active workers. Reduces
unnecessary nudges and LLM evaluations.

### Hook 3: UserPromptSubmit -- Activity Confirmation

**Claude Code event**: `UserPromptSubmit` -- fires when a prompt is submitted
(including daemon-sent prompts via send-keys).

**What it does**: Clears the needs-input signal and refreshes the heartbeat.
Confirms the worker is now processing a new prompt.

**Hook script**: `hooks/prompt-hook.sh`

```bash
#!/usr/bin/env bash
# Prompt submit hook -- clear needs-input and refresh heartbeat.
# Args: <signal_dir> <worker_name>

SIGNAL_DIR="$1"
WORKER_NAME="$2"

if [ -z "$SIGNAL_DIR" ] || [ -z "$WORKER_NAME" ]; then
  exit 0
fi

rm -f "${SIGNAL_DIR}/${WORKER_NAME}.needs-input"
touch "${SIGNAL_DIR}/${WORKER_NAME}.heartbeat"
```

**Impact**: Cleans up stale needs-input signals automatically when the worker
resumes. Prevents the daemon from re-escalating a prompt that was already
answered.

### Hook 4: PostToolUse (*) -- Post-Execution Heartbeat

**Claude Code event**: `PostToolUse` -- fires after every tool completes.

**What it does**: Same as PreToolUse heartbeat. Ensures heartbeat stays fresh
even during long tool executions (e.g., a 30-second test run).

**Hook script**: Reuses `hooks/heartbeat-hook.sh` (same script, different
event).

**Impact**: Prevents heartbeat from going stale during long-running individual
tools.

### Hook 5: SessionEnd -- Crash Detection

**Claude Code event**: `SessionEnd` -- fires when Claude Code process exits (any
reason).

**What it does**: Writes a `.exited` signal so the daemon knows the worker
process is gone. Includes the exit reason for diagnostics.

**Signal file**: `.jack-in/signals/<worker>.exited`

**Hook script**: `hooks/session-end-hook.sh`

```bash
#!/usr/bin/env bash
# Session end hook -- signal that the worker process exited.
# Args: <signal_dir> <worker_name>
# Stdin: JSON with reason field

SIGNAL_DIR="$1"
WORKER_NAME="$2"

if [ -z "$SIGNAL_DIR" ] || [ -z "$WORKER_NAME" ]; then
  exit 0
fi

INPUT=$(cat)
REASON=$(echo "$INPUT" | grep -o '"reason"[[:space:]]*:[[:space:]]*"[^"]*"' | head -1 | sed 's/.*"reason"[[:space:]]*:[[:space:]]*"//;s/".*//')

# Atomic write: tmp file + rename for payload integrity
TMPFILE="${SIGNAL_DIR}/${WORKER_NAME}.exited.tmp"
echo "${REASON:-unknown}" > "$TMPFILE"
mv "$TMPFILE" "${SIGNAL_DIR}/${WORKER_NAME}.exited"
```

**Daemon behavior on `.exited` signal**:

- If worker had a current task:
  - Log error: "Worker exited while task in progress"
  - Move task back to pending (unclaim) for retry
  - Desktop notification: "worker-1 exited -- task returned to queue"
- Clear all signal files for this worker (.done, .needs-input, .heartbeat,
  .exited)
- Mark worker as dead in state

**Impact**: Instant crash detection instead of waiting for Tier 2/3 timeouts.

---

## Non-Claude Agent Shim: `notify-hook.sh`

### Problem

Codex, OpenCode, and Gemini don't have a hook system like Claude Code. They can
only be told to `touch <signal_file>` in the task prompt. This gives us
completion detection but nothing for heartbeats, needs-input, or crash signals.

### Solution: `notify-hook.sh`

A simple script that non-Claude agents can call from their terminal to emit
jackin signals. It's included in the task prompt as an available command.

**Script**: `hooks/notify-hook.sh`

```bash
#!/usr/bin/env bash
# jackin notify-hook -- emit signals from non-Claude agents.
# Usage: .jack-in/notify-hook.sh <event> [<data>]
#
# Events:
#   active      -- worker is actively working (heartbeat)
#   done        -- task complete (same as touch .done)
#   needs-input -- worker is blocked on user input
#   error       -- worker hit an error
#
# Reads JACK-IN_WORKER_NAME and JACK-IN_SIGNAL_DIR from environment,
# or falls back to args: notify-hook.sh <event> <signal_dir> <worker_name>

EVENT="$1"
SIGNAL_DIR="${JACK-IN_SIGNAL_DIR:-$2}"
WORKER_NAME="${JACK-IN_WORKER_NAME:-$3}"

if [ -z "$EVENT" ] || [ -z "$SIGNAL_DIR" ] || [ -z "$WORKER_NAME" ]; then
  echo "Usage: notify-hook.sh <event> [signal_dir] [worker_name]" >&2
  exit 1
fi

case "$EVENT" in
  active)
    touch "${SIGNAL_DIR}/${WORKER_NAME}.heartbeat"
    ;;
  done)
    touch "${SIGNAL_DIR}/${WORKER_NAME}.done"
    ;;
  needs-input)
    echo "${4:-unknown}" > "${SIGNAL_DIR}/${WORKER_NAME}.needs-input"
    ;;
  error)
    echo "${4:-unknown}" > "${SIGNAL_DIR}/${WORKER_NAME}.error"
    ;;
  *)
    echo "Unknown event: $EVENT" >&2
    exit 1
    ;;
esac
```

### Integration with task prompts

Update `formatTaskPrompt()` for non-Claude agents to include shim instructions:

```
IMPORTANT: When you are completely done with this task, run: .jack-in/notify-hook.sh done
Also output exactly this on its own line: JACKIN_TASK_COMPLETE:<taskId>

Tip: If you encounter an error you can't resolve, run: .jack-in/notify-hook.sh error "description"
```

### Environment variables for shim

Set via `tmux set-environment` on worker spawn:

- `JACK-IN_SIGNAL_DIR` = absolute path to `.jack-in/signals/`
- `JACK-IN_WORKER_NAME` = worker name

This lets the shim work without positional args when called from within the tmux
pane.

### Viability assessment

**Will agents actually call the shim?** Not reliably on their own initiative,
but:

1. **Completion**: Already works today via `touch <signal>` -- the shim is just
   a nicer wrapper. The daemon-side marker scan in pane content is the real
   fallback.

2. **Heartbeat**: Agents won't call this proactively. But that's OK -- the
   heartbeat from Claude's hooks covers the primary use case. For non-Claude
   agents, the daemon continues to rely on pane snapshot diffs (Tier 2). The
   shim is there for agents smart enough to use it.

3. **Error reporting**: Agents sometimes output error messages. The shim gives
   them a structured way to signal this, but the daemon should not depend on it.

**Conclusion**: The shim adds value as an optional signal channel. The daemon
MUST NOT require it -- all non-Claude detection paths must continue to work via
pane scraping. The shim is a "fast path" optimization, not a hard dependency.

---

## Daemon Changes

### New signal file types

| Signal File    | Written By                                          | Meaning                    | Daemon Action                         |
| -------------- | --------------------------------------------------- | -------------------------- | ------------------------------------- |
| `.heartbeat`   | heartbeat-hook.sh / prompt-hook.sh / notify-hook.sh | Worker is actively running | Skip Tier 2 if fresh (<30s)           |
| `.needs-input` | notification-hook.sh / notify-hook.sh               | Worker blocked on input    | Immediate escalation (mode-dependent) |
| `.exited`      | session-end-hook.sh                                 | Worker process exited      | Unclaim task, mark dead, notify       |

### Modified WorkerState interface

```typescript
interface WorkerState {
  name: string;
  agent: string;
  currentTask: string | null;
  assignedAt: number | null;
  lastPaneSnapshot: string | null;
  lastSnapshotAt: number | null;
  llmEvalCount: number;
  escalatedToUser: boolean;
  // New fields:
  lastHeartbeat: number | null; // mtime of .heartbeat file (epoch ms)
  needsInput: boolean; // .needs-input signal present
  exited: boolean; // .exited signal present
}
```

### Modified tick() flow

```
for each worker with a task:
  1. Check .exited signal -> if set: unclaim task, mark dead, notify, continue
  2. Check .done signal -> if set: mark complete (existing logic)
  3. Check .needs-input signal -> if set:
       - Set state.needsInput = true
       - Immediately handle based on approval mode:
         - auto: trigger LLM pane eval (same as Tier 3 but immediate)
         - yolo: send Enter keystroke
         - manual: display message + desktop notification
       - Clear .needs-input signal after handling
       - Continue (skip Tier 2/3)
  4. Check .heartbeat mtime -> update state.lastHeartbeat
  5. If heartbeat is fresh (<30s) AND task wall time < MAX_TASK_WALL_MS:
       log "active", skip Tier 2/3
  6. If task wall time >= MAX_TASK_WALL_MS (15 min): force Tier 3 eval
       regardless of heartbeat (progress deadline)
  7. Else: existing Tier 2/3 logic (unchanged)
```

### Stale signal race condition mitigations (from Codex review)

**Problem**: Signal files from a previous task or session can be consumed by the
next assignment, causing false positives.

**Mitigations**:

1. **Clear ALL per-worker signals at assignment boundary**: when a new task is
   assigned, clear `.done`, `.heartbeat`, `.needs-input`, `.exited` for that
   worker. This is an extension of the existing `clearSignal()` call at line 644
   of daemon.ts.
2. **Existing early-signal suppression**: the `MIN_WORK_MS` (10s) grace period
   already handles `.done` signals from previous turns. The same pattern applies
   to `.needs-input` -- ignore it if the task was assigned < 5s ago.
3. **Hook events are not guaranteed**: PreToolUse may not fire in all edge cases
   (background Agent calls). SessionEnd may not fire on `kill -9`. The daemon
   MUST NOT depend on any hook signal as the sole detection path -- pane
   scraping fallbacks remain authoritative.

### Modified buildClaudeSettings()

Add new hook entries. All hooks are always active (not approval-mode-dependent):

```typescript
export function buildClaudeSettings(
  base: string,
  workerName: string,
  approval: ApprovalMode = "manual",
) {
  const jackinDir = join(base, ".jack-in");
  const signalDir = join(base, SIGNAL_DIR);
  const currentTaskDir = join(base, CURRENT_TASK_DIR);

  // Existing hooks
  const stopHook = join(jackinDir, "stop-hook.ts");
  const hooks: Record<string, any[]> = {
    Stop: [{
      matcher: "*",
      hooks: [{
        type: "command",
        command: `${esc(stopHook)} ${esc(signalDir)} ${esc(workerName)} ${
          esc(currentTaskDir)
        }`,
      }],
    }],
  };

  // NEW: Notification hook (always active)
  const notificationHook = join(jackinDir, "notification-hook.sh");
  hooks.Notification = [
    {
      matcher: "idle_prompt",
      hooks: [{
        type: "command",
        command: `${esc(notificationHook)} ${esc(signalDir)} ${
          esc(workerName)
        }`,
      }],
    },
    {
      matcher: "permission_prompt",
      hooks: [{
        type: "command",
        command: `${esc(notificationHook)} ${esc(signalDir)} ${
          esc(workerName)
        }`,
      }],
    },
    {
      matcher: "elicitation_dialog",
      hooks: [{
        type: "command",
        command: `${esc(notificationHook)} ${esc(signalDir)} ${
          esc(workerName)
        }`,
      }],
    },
  ];

  // NEW: PreToolUse heartbeat (always active)
  const heartbeatHook = join(jackinDir, "heartbeat-hook.sh");
  hooks.PreToolUse = [{
    matcher: "*",
    hooks: [{
      type: "command",
      command: `${esc(heartbeatHook)} ${esc(signalDir)} ${esc(workerName)}`,
    }],
  }];

  // NEW: PostToolUse heartbeat (always active)
  hooks.PostToolUse = [{
    matcher: "*",
    hooks: [{
      type: "command",
      command: `${esc(heartbeatHook)} ${esc(signalDir)} ${esc(workerName)}`,
    }],
  }];

  // NEW: UserPromptSubmit (always active)
  const promptHook = join(jackinDir, "prompt-hook.sh");
  hooks.UserPromptSubmit = [{
    matcher: "*", // Note: UserPromptSubmit has no matchers per the docs
    hooks: [{
      type: "command",
      command: `${esc(promptHook)} ${esc(signalDir)} ${esc(workerName)}`,
    }],
  }];

  // NEW: SessionEnd crash detection (always active)
  const sessionEndHook = join(jackinDir, "session-end-hook.sh");
  hooks.SessionEnd = [{
    matcher: "*",
    hooks: [{
      type: "command",
      command: `${esc(sessionEndHook)} ${esc(signalDir)} ${esc(workerName)}`,
    }],
  }];

  // Existing: PermissionRequest (approval-mode-dependent)
  if (approval === "auto") {
    // ... existing permission-eval.sh config
  } else if (approval === "yolo") {
    // ... existing yolo-approve.sh config
  }

  return { permissions: { allow: ["Bash(jackin *)"] }, hooks };
}
```

### Modified initSignals()

Add new hook scripts to the copy list:

```typescript
for (
  const name of [
    "stop-hook.ts",
    "stop-hook.sh",
    "permission-eval.sh",
    "yolo-approve.sh",
    // New hooks:
    "notification-hook.sh",
    "heartbeat-hook.sh",
    "prompt-hook.sh",
    "session-end-hook.sh",
    "notify-hook.sh", // Non-Claude shim
  ]
) {
  const src = join(repoRoot, "hooks", name);
  const dst = join(jackinDir, name);
  await Deno.copyFile(src, dst);
  await Deno.chmod(dst, 0o755);
}
```

### Modified mergeClaudeSettings()

The existing merge logic identifies jackin hooks by checking if the command
contains `.jack-in/`. This naturally handles the new hooks -- they'll be added
alongside existing ones and cleaned up when approval mode changes.

However, the new hooks (Notification, PreToolUse, PostToolUse, UserPromptSubmit,
SessionEnd) are **always active** regardless of approval mode. The merge
function's removal logic (lines 241-248) should only remove hooks for events
whose hook list becomes empty in the new config. Since the new hooks are always
present, they won't be accidentally removed.

No changes needed to the merge logic itself.

---

## New Hook Signal Helpers

Add constants and helpers to daemon.ts alongside existing signal helpers:

```typescript
const HEARTBEAT_FRESH_MS = 30_000; // Heartbeat younger than this = active
const MAX_TASK_WALL_MS = 15 * 60_000; // 15 min progress deadline
const NEEDS_INPUT_GRACE_MS = 5_000; // Ignore needs-input signals within 5s of assignment

export function heartbeatPath(base: string, workerName: string): string {
  return join(base, SIGNAL_DIR, `${workerName}.heartbeat`);
}

export async function heartbeatAge(
  base: string,
  workerName: string,
): Promise<number | null> {
  try {
    const stat = await Deno.stat(heartbeatPath(base, workerName));
    if (!stat.mtime) return null;
    return Date.now() - stat.mtime.getTime();
  } catch {
    return null; // No heartbeat file
  }
}

export function needsInputPath(base: string, workerName: string): string {
  return join(base, SIGNAL_DIR, `${workerName}.needs-input`);
}

export async function hasNeedsInput(
  base: string,
  workerName: string,
): Promise<boolean> {
  try {
    await Deno.stat(needsInputPath(base, workerName));
    return true;
  } catch {
    return false;
  }
}

export async function clearNeedsInput(
  base: string,
  workerName: string,
): Promise<void> {
  try {
    await Deno.remove(needsInputPath(base, workerName));
  } catch (e) {
    if (e instanceof Deno.errors.NotFound) return;
    throw e;
  }
}

export function exitedPath(base: string, workerName: string): string {
  return join(base, SIGNAL_DIR, `${workerName}.exited`);
}

export async function hasExited(
  base: string,
  workerName: string,
): Promise<boolean> {
  try {
    await Deno.stat(exitedPath(base, workerName));
    return true;
  } catch {
    return false;
  }
}

export async function clearExited(
  base: string,
  workerName: string,
): Promise<void> {
  try {
    await Deno.remove(exitedPath(base, workerName));
  } catch (e) {
    if (e instanceof Deno.errors.NotFound) return;
    throw e;
  }
}

/** Clear ALL signal files for a worker (used at task assignment boundary). */
export async function clearAllWorkerSignals(
  base: string,
  workerName: string,
): Promise<void> {
  await clearSignal(base, workerName); // .done
  await clearNeedsInput(base, workerName); // .needs-input
  await clearExited(base, workerName); // .exited
  // .heartbeat: remove so stale heartbeat from previous task
  // doesn't cause false "active" on new task
  try {
    await Deno.remove(heartbeatPath(base, workerName));
  } catch (e) {
    if (e instanceof Deno.errors.NotFound) return;
    throw e;
  }
}
```

---

## Non-Claude Task Prompt Update

Update `formatTaskPrompt()` in daemon.ts for non-Claude agents:

```typescript
} else {
  // Non-Claude agents: use notify-hook shim + marker for daemon-side scan
  const shim = join(base, ".jack-in", "notify-hook.sh");
  const marker = completionMarker(task.id);
  lines.push("");
  lines.push(
    `IMPORTANT: When you are completely done with this task, run: ${shim} done ${shellEscape(signalDir)} ${shellEscape(workerName)}`,
  );
  lines.push(
    `Also output exactly this on its own line: ${marker}`,
  );
}
```

Optionally, if env vars are set (via improvement 2 -- worker identity):

```
IMPORTANT: When you are completely done with this task, run: .jack-in/notify-hook.sh done
```

---

## Environment Variables for Non-Claude Workers

Set via tmux before spawning the agent:

```typescript
// In worker spawn code:
await tmux.sendKeys(target, `export JACK-IN_SIGNAL_DIR='${signalDir}'`);
await tmux.sendKeys(target, `export JACK-IN_WORKER_NAME='${workerName}'`);
// Then spawn the agent
await tmux.sendKeys(target, spawnCommand(agent, prompt, startup));
```

This enables the shim to work without positional args.

---

## Test Plan

### Unit tests (test/daemon_test.ts)

1. **heartbeatAge()**: fresh file returns small number, missing file returns
   null
2. **hasNeedsInput()**: create file returns true, missing returns false
3. **hasExited()**: create file returns true, missing returns false
4. **buildClaudeSettings()**: verify all 7 hook events present in output
5. **buildClaudeSettings()**: Notification hooks present for all approval modes
6. **tick() with heartbeat**: fresh heartbeat skips Tier 2
7. **tick() with needs-input**: immediate escalation per approval mode
8. **tick() with exited**: task unclaimed, worker marked dead

### Integration tests (test/hooks_integration_test.ts)

1. **notification-hook.sh**: writes .needs-input file with correct content
2. **heartbeat-hook.sh**: touches .heartbeat file
3. **prompt-hook.sh**: clears .needs-input and refreshes .heartbeat
4. **session-end-hook.sh**: writes .exited file with reason
5. **notify-hook.sh**: all events (active, done, needs-input, error)

### Manual smoke test

1. Start jackin swarm with 1 Claude worker
2. Assign a task that requires file edits (triggers PreToolUse)
3. Verify `.heartbeat` file is being updated (ls -la .jack-in/signals/)
4. Ask Claude a question that triggers AskUserQuestion -> verify `.needs-input`
   appears
5. Answer the question -> verify `.needs-input` is cleared
6. Let task complete -> verify `.done` signal fires as before
7. Kill Claude process -> verify `.exited` signal appears

---

## Files to Create

| File                         | Type | Purpose                                                  |
| ---------------------------- | ---- | -------------------------------------------------------- |
| `hooks/notification-hook.sh` | New  | Notification event -> .needs-input signal                |
| `hooks/heartbeat-hook.sh`    | New  | PreToolUse/PostToolUse -> .heartbeat signal              |
| `hooks/prompt-hook.sh`       | New  | UserPromptSubmit -> clear needs-input, refresh heartbeat |
| `hooks/session-end-hook.sh`  | New  | SessionEnd -> .exited signal                             |
| `hooks/notify-hook.sh`       | New  | Non-Claude agent shim for signal emission                |

## Files to Modify

| File                  | Changes                                                                                                                                        |
| --------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------- |
| `src/daemon.ts`       | New signal helpers, modified WorkerState, modified tick(), modified buildClaudeSettings(), modified initSignals(), modified formatTaskPrompt() |
| `test/daemon_test.ts` | New tests for heartbeat, needs-input, exited signal handling                                                                                   |

## Files Unchanged

| File                       | Reason                                                |
| -------------------------- | ----------------------------------------------------- |
| `hooks/stop-hook.ts`       | Existing completion detection stays as-is             |
| `hooks/permission-eval.sh` | Existing auto-approval stays as-is                    |
| `hooks/yolo-approve.sh`    | Existing yolo-approval stays as-is                    |
| `src/tmux.ts`              | No tmux changes needed                                |
| `src/llm.ts`               | LLM eval logic unchanged                              |
| `src/config.ts`            | No config schema changes                              |
| `src/agents.ts`            | No agent changes (env vars set in daemon spawn logic) |

---

## Performance Considerations

### Hook execution overhead

Each Claude Code tool use now triggers 2 hooks (PreToolUse + PostToolUse) that
each do a `touch` on a file. This is ~1ms per hook call. Claude typically runs
5-20 tools per task, so overhead is 10-40ms total per task -- negligible.

The Notification hook is even rarer -- only fires on idle/permission events.

### Daemon tick overhead

Each tick now checks 3 additional `stat()` calls per worker (.heartbeat,
.needs-input, .exited). With 4 workers, that's 12 extra stat calls per tick. At
5s poll interval, this is trivial.

### Signal file cleanup

All signal files are cleaned up when:

- Task completes (clearSignal already handles .done; extend to clear .heartbeat,
  .needs-input)
- Worker exits (clear all signals for that worker)
- Daemon restarts (existing stale signal cleanup in initSignals)

---

## Risk Assessment

| Risk                                     | Likelihood | Mitigation                                                                                          |
| ---------------------------------------- | ---------- | --------------------------------------------------------------------------------------------------- |
| Hook script crashes block Claude         | Low        | All hooks use `exit 0` on error; Claude Code has timeout fallback                                   |
| Stale signals from previous task         | Medium     | `clearAllWorkerSignals()` at assignment boundary; `NEEDS_INPUT_GRACE_MS` suppression                |
| Stale .needs-input not cleared           | Medium     | UserPromptSubmit hook clears it; daemon clears on task assignment                                   |
| .heartbeat causes false "active" forever | Low        | `MAX_TASK_WALL_MS` (15 min) progress deadline forces Tier 3 eval regardless                         |
| Hooks not guaranteed delivery (kill -9)  | Medium     | Pane scraping fallbacks remain authoritative; hooks are fast-path only                              |
| PreToolUse skipped for background Agents | Low        | Daemon falls back to Tier 2/3 after heartbeat goes stale (>30s)                                     |
| Non-Claude shim not called by agent      | Expected   | Daemon does NOT depend on it; pane scraping remains the fallback                                    |
| Too many hook scripts slow Claude        | Low        | Hooks are <5 lines of bash; ~1ms each; use `#!/bin/sh` not `#!/usr/bin/env bash` for faster startup |
| SessionEnd hook timeout                  | Low        | Default 1.5s timeout; our hook is a single atomic file write                                        |
| Notification race with UserPromptSubmit  | Low        | `NEEDS_INPUT_GRACE_MS` debounce; daemon checks needs-input before heartbeat                         |

---

## Implementation Order

1. **Create hook scripts** (5 new files) -- can be done in parallel
2. **Add signal helpers** to daemon.ts -- heartbeatAge, hasNeedsInput,
   hasExited, clear*
3. **Extend WorkerState** with new fields
4. **Modify tick()** -- add new signal checks before existing Tier 2/3 logic
5. **Modify buildClaudeSettings()** -- add new hook entries
6. **Modify initSignals()** -- add new scripts to copy list
7. **Update formatTaskPrompt()** -- use notify-hook.sh for non-Claude agents
8. **Write tests** -- unit + integration
9. **Manual smoke test** -- verify end-to-end with real Claude worker
