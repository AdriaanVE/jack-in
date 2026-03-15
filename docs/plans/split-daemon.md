# Plan: Split daemon.ts into multiple files

## Current state

`src/daemon.ts` is 887 lines and handles several distinct concerns:

1. **Signal file helpers** (lines 45-104): `signalPath`, `hasSignal`,
   `clearSignal`, `initSignals`
2. **Claude settings** (lines 106-263): `buildClaudeSettings`,
   `writeClaudeSettings`, `mergeClaudeSettings`, `atomicWriteJson`
3. **Approval mode** (lines 265-287): `readApprovalMode`, `writeApprovalMode`
4. **Current-task file helpers** (lines 295-321): `currentTaskPath`,
   `writeCurrentTask`, `clearCurrentTask`
5. **Task prompt formatting and delivery** (lines 323-411): `formatTaskPrompt`,
   `writeTaskPrompt`, `taskMessage`, `MAX_SENDKEYS_BYTES`
6. **Daemon loop and tick** (lines 435-675): `DaemonOptions`, `run`, `tick`,
   `waitForNewTask`
7. **Watchdog** (lines 700-872): `watchdog`, `resetWatchdog`, `markComplete`,
   `paneContainsMarker`
8. **Status printing** (lines 874-886): `printStatus`

There is also a `WorkerState` interface (lines 34-43) and several constants
(lines 19-32).

## Proposed file structure

```
src/
  daemon.ts           # Main loop only: run(), tick(), DaemonOptions, WorkerState
  daemon/
    signals.ts        # Signal file helpers + initSignals
    settings.ts       # Claude settings (build, write, merge, atomicWriteJson)
    approval.ts       # readApprovalMode, writeApprovalMode, APPROVAL_MODE_FILE
    current-task.ts   # currentTaskPath, writeCurrentTask, clearCurrentTask
    task-prompt.ts    # formatTaskPrompt, writeTaskPrompt, taskMessage, MAX_SENDKEYS_BYTES
    watchdog.ts       # watchdog, resetWatchdog, markComplete, paneContainsMarker, tier constants
```

### File-by-file breakdown

#### `src/daemon/signals.ts`

Exports: `signalPath`, `hasSignal`, `clearSignal`, `initSignals`, `SIGNAL_DIR`

Dependencies: `@std/path`, `./agents.ts` (for `shellEscape` used in
`initSignals`)

Note: `initSignals` copies hook scripts and creates directories. It also needs
`CURRENT_TASK_DIR` -- either import from `current-task.ts` or inline the
constant.

#### `src/daemon/settings.ts`

Exports: `buildClaudeSettings`, `writeClaudeSettings`, `mergeClaudeSettings`

Internal: `atomicWriteJson` (keep private)

Dependencies: `@std/path`, `./config.ts` (ApprovalMode), `./agents.ts`
(shellEscape), `./signals.ts` (SIGNAL_DIR, signalPath is not needed here -- only
the dir path)

#### `src/daemon/approval.ts`

Exports: `readApprovalMode`, `writeApprovalMode`, `APPROVAL_MODE_FILE`

Dependencies: `@std/path`, `./config.ts` (ApprovalMode, isApprovalMode)

Smallest module. Could merge with signals.ts, but approval mode is conceptually
distinct from completion signals.

#### `src/daemon/current-task.ts`

Exports: `currentTaskPath`, `writeCurrentTask`, `clearCurrentTask`,
`CURRENT_TASK_DIR`

Dependencies: `@std/path`

#### `src/daemon/task-prompt.ts`

Exports: `formatTaskPrompt`, `writeTaskPrompt`, `taskMessage`,
`MAX_SENDKEYS_BYTES`

Dependencies: `@std/path`, `./task-queue.ts`, `./marker.ts`, `./signals.ts`
(signalPath)

#### `src/daemon/watchdog.ts`

Exports: `watchdog`, `resetWatchdog`, `markComplete`, `paneContainsMarker`,
`TIER2_TIMEOUT_MS`, `TIER3_TIMEOUT_MS`, `MAX_LLM_EVALS`

Needs `WorkerState` -- import from `daemon.ts` or define in a shared types file.

Dependencies: `./tmux.ts`, `./llm.ts`, `./task-queue.ts`, `./marker.ts`,
`./signals.ts`, `./config.ts` (ApprovalMode), `./log.ts`

#### `src/daemon.ts` (slimmed down)

Keeps: `DaemonOptions`, `run()`, `tick()`, `waitForNewTask()`, `printStatus()`,
`WorkerState`

Re-exports from submodules for backward compatibility (existing importers):

```ts
export {
  clearSignal,
  hasSignal,
  initSignals,
  signalPath,
} from "./daemon/signals.ts";
export {
  buildClaudeSettings,
  mergeClaudeSettings,
  writeClaudeSettings,
} from "./daemon/settings.ts";
// etc.
```

## Implementation approach

1. Create `src/daemon/` directory.
2. Extract `signals.ts` first -- it has no daemon-internal dependencies.
3. Extract `current-task.ts` -- similarly standalone.
4. Extract `approval.ts`.
5. Extract `settings.ts` -- depends on signals for directory paths.
6. Extract `task-prompt.ts` -- depends on signals and marker.
7. Extract `watchdog.ts` -- depends on most other modules.
8. Update `daemon.ts` to import from submodules and re-export for compatibility.
9. Update all external importers to verify they still resolve.
10. Run `deno task test:unit` and `deno task test:integration` after each
    extraction.
11. Run `deno fmt` and `deno lint`.

### Shared constants strategy

`SIGNAL_DIR`, `CURRENT_TASK_DIR`, `PROMPT_DIR` are string constants used across
modules. Each lives in the module that owns that directory:

- `SIGNAL_DIR` in `signals.ts`
- `CURRENT_TASK_DIR` in `current-task.ts`
- `PROMPT_DIR` in `task-prompt.ts`

### WorkerState location

`WorkerState` is used by `daemon.ts` (tick, run) and `watchdog.ts`. Two options:

1. **Keep in `daemon.ts`**, import into `watchdog.ts` -- creates a mild circular
   dependency risk but is fine since it's just a type import.
2. **Move to `src/daemon/types.ts`** -- cleanest, avoids any circular concern.

**Recommendation:** Option 2 (types.ts) if more than `WorkerState` is shared.
Option 1 if it's the only shared type (it is currently).

## Re-export strategy

The current `daemon.ts` exports many symbols used by `cli.ts`, tests, and other
modules. To avoid a large-scale import rewrite in one PR:

- `daemon.ts` re-exports everything from its submodules.
- Future PRs can update importers to use direct paths (e.g.,
  `import { signalPath } from "./daemon/signals.ts"`).

## Risks and edge cases

- **Circular imports**: `watchdog.ts` needs `WorkerState` from `daemon.ts`, and
  `daemon.ts` imports `watchdog` from `watchdog.ts`. Solve by putting
  `WorkerState` in a types file, or by using type-only imports.
- **initSignals coupling**: `initSignals` creates both signal and current-task
  directories. After the split, it imports `CURRENT_TASK_DIR` from
  `current-task.ts`. This cross-module dependency is acceptable.
- **Re-export bloat**: Temporary. The re-exports ensure no breaking changes for
  external importers during the transition.
- **Test imports**: Tests that import from `daemon.ts` continue to work via
  re-exports. No test changes needed in the initial PR.
- **atomicWriteJson**: Currently private to `daemon.ts`. Stays private in
  `settings.ts`. If other modules later need it, promote to a shared utility.

## Acceptance criteria

- [ ] Each new file under `src/daemon/` has a single concern
- [ ] `src/daemon.ts` is under 200 lines (loop + tick + status + re-exports)
- [ ] All existing exports from `daemon.ts` still resolve (re-export layer)
- [ ] No circular import errors at runtime
- [ ] `deno task test:unit` passes
- [ ] `deno task test:integration` passes
- [ ] `deno fmt` and `deno lint` pass
- [ ] No behavioral changes -- pure refactor
