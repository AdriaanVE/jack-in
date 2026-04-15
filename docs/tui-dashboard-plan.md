# TUI Dashboard Plan

Interactive terminal dashboard replacing the daemon's plain log output. Uses
[deno_tui](https://deno.land/x/tui@2.1.11) for rendering.

## Current state

- `jackin up` creates a tmux session with:
  - `dashboard-orchestrator` window: pane 0 = daemon logs, pane 1 = orchestrator
    agent
  - One hidden window per worker (e.g. `w1`, `w2`)
- Daemon (`src/daemon.ts`) runs the poll loop, prints status via logger
- `src/status.ts` already has `getStatus()` / `formatStatus()` for CLI
  `jackin status`

## Goal

Replace the daemon's log pane (pane 0 of `dashboard-orchestrator`) with an
interactive TUI that:

1. Renders live worker cards showing name, agent, state (idle/working/stuck),
   current task
2. Renders task counters (pending/current/review/complete/rejected)
3. Renders a scrollable log feed from the daemon
4. Handles mouse clicks on worker cards to swap the bottom pane

## Layout

```
+-------------------------------------------+
| [w1 ●working] [w2 ○idle] [orch ●active]  |  <- worker cards row
|                                           |
| Tasks: 2 pending  1 current  0 review    |  <- task counters
|        5 complete  1 rejected             |
|                                           |
| 14:32:01 [assign] task-001 -> w1: ...    |  <- log feed
| 14:32:05 [review] w2 finished task-002   |
| 14:32:08 [tier2-idle] w1: pane unchanged |
+-------------------------------------------+
| (selected worker/orchestrator pane)       |  <- tmux pane swap target
|                                           |
+-------------------------------------------+
```

## Architecture

### New files

- `src/tui.ts` — TUI rendering and event handling (~150-200 lines)

### Modified files

- `src/daemon.ts` — expose daemon state for TUI consumption
- `src/tmux.ts` — add `swapPane()` wrapper
- `src/cli/commands/up.ts` — launch TUI instead of raw daemon
- `deno.json` — add `deno_tui` import

### No new files needed for

- Config, task-queue, status — existing modules are sufficient

## Implementation plan

### Step 1: Add deno_tui dependency

Add to `deno.json` imports:

```json
"tui": "https://deno.land/x/tui@2.1.11/mod.ts",
"tui/components": "https://deno.land/x/tui@2.1.11/src/components/mod.ts"
```

Verify: `deno cache src/tui.ts` succeeds.

### Step 2: Add `swapPane` to tmux.ts

```typescript
export async function swapPane(source: string, target: string): Promise<void>;
```

Wraps `tmux swap-pane -s <source> -t <target>`. This is the primitive that makes
clicking a worker card show that worker's pane in the bottom split.

Verify: unit test with mock.

### Step 3: Extract daemon state as an observable

Currently the daemon loop owns `workers: Map<string, WorkerState>` and
`orchState` locally in `run()`. The TUI needs to read this state every render
tick.

Approach: create a `DaemonContext` object that holds the mutable state and pass
it to both the daemon loop and the TUI renderer.

```typescript
export interface DaemonContext {
  workers: Map<string, WorkerState>;
  orchState: OrchestratorState | null;
  taskCounts: TaskCounts;
  logs: string[]; // ring buffer, last N log lines
  approval: ApprovalMode;
  session: string;
}
```

The daemon loop updates `DaemonContext` on each tick. The TUI reads it on each
render frame. No pub/sub needed — deno_tui's `Signal` can wrap a poll:

```typescript
const ctx = new Signal<DaemonContext>(initialCtx);
// daemon tick updates ctx.value = { ...ctx.value, taskCounts: newCounts };
// TUI components read ctx.value reactively
```

Verify: daemon still passes all existing tests (DaemonContext is additive).

### Step 4: Build the TUI renderer (`src/tui.ts`)

Core structure:

```typescript
import {
  Computed,
  handleInput,
  handleKeyboardControls,
  handleMouseControls,
  Signal,
  Tui,
} from "tui";
import { Button } from "tui/components";

export function createDashboard(ctx: Signal<DaemonContext>): Tui {
  const tui = new Tui({ style: crayon.bgBlack, refreshRate: 1000 / 2 });

  handleInput(tui);
  handleMouseControls(tui);
  handleKeyboardControls(tui);

  // Worker card buttons — one per worker
  for (const [i, [name, state]] of [...ctx.value.workers].entries()) {
    const btn = new Button({
      parent: tui,
      theme: { base: crayon.white, focused: crayon.cyan, active: crayon.green },
      rectangle: { column: i * 20, row: 0, width: 18, height: 3 },
      label: new Computed(() => {
        const w = ctx.value.workers.get(name)!;
        const icon = w.currentTask ? "●" : "○";
        return `${icon} ${name}`;
      }),
    });

    btn.on("mousePress", () => {
      swapToWorker(ctx.value.session, name);
    });
  }

  // Task counters — static text component updated reactively
  // Log feed — text component showing last N lines from ctx.value.logs

  return tui;
}
```

Key decisions:

- **Refresh rate**: 500ms (2 fps) — fast enough for status, not wasteful
- **Worker cards**: `Button` components with computed labels
- **Log feed**: plain text area showing ring buffer tail
- **Quit**: `q` or `Ctrl-C` triggers `tui.destroy()` + daemon abort

Verify: TUI renders in a test terminal, worker cards clickable.

### Step 5: Wire TUI into daemon startup

In `src/cli/commands/up.ts`, the daemon pane currently runs:

```
jackin daemon --approval <mode>
```

Change to launch a combined daemon+TUI process. Two options:

**Option A (preferred)**: The daemon command itself renders the TUI when stdout
is a TTY.

- `jackin daemon` detects `Deno.stdin.isTerminal()` and creates the TUI
- Non-TTY mode (piped logs) falls back to plain text — backward compatible

**Option B**: New `jackin dashboard` command that wraps daemon + TUI.

Option A is simpler — no new command, no coordination between processes.

Verify: `jackin up` shows TUI in top pane, worker panes swap on click.

### Step 6: Pane swap mechanics

When a worker card is clicked:

1. Get the current "focused" worker name (default: first worker)
2. Run
   `tmux swap-pane -s {session}:{clicked_worker}.0 -t {session}:dashboard-orchestrator.1`
3. Update a `focusedWorker` signal so the card highlight changes

Edge cases:

- Clicking the already-focused worker: no-op
- Clicking "orchestrator" card: swap orchestrator pane back into the bottom
  split
- Worker pane dead: show message, don't swap

Verify: manual test — click w1, see w1 content; click w2, see w2 content.

### Step 7: Log feed integration

Replace direct logger calls in daemon.ts with writes to the ring buffer:

```typescript
const MAX_LOG_LINES = 200;

function pushLog(ctx: DaemonContext, line: string): void {
  ctx.logs.push(line);
  if (ctx.logs.length > MAX_LOG_LINES) ctx.logs.shift();
}
```

The existing `getJack-InLogger("daemon")` can be configured with a custom sink
that writes to both the ring buffer (for TUI) and stderr (for file logging).

Verify: logs appear in TUI log feed area.

## What this does NOT include

- Orchestrator-specific UI (reviewing diffs, approve/reject buttons) — future
  work
- Resizable panes within the TUI — tmux handles the split
- Persistent layout config — uses hardcoded positions for now
- Custom themes — single dark theme

## Risks

1. **deno_tui compatibility**: last release Jan 2024. If it breaks on current
   Deno, fallback to raw ANSI escape sequences for a simpler renderer (worker
   cards + counters only, no mouse — use keyboard navigation instead).

2. **tmux swap-pane race**: swapping panes while a worker is writing output
   could cause flicker. Mitigation: the swap is instantaneous (tmux internal),
   no visible race.

3. **Mouse passthrough**: tmux needs `set -g mouse on` for mouse events to reach
   the TUI. Document this as a requirement; `jackin up` can set it
   automatically via `tmux set-option`.

## Dependencies

- `deno_tui@2.1.11` (zero transitive deps)
- Existing: `@std/path`, `@std/yaml`, `@logtape/logtape`
