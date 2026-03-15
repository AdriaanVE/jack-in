# Plan: Constants and `Promise.all` cleanup in `src/status.ts`

## Goal

Address two targeted cleanups in `src/status.ts`:

1. Extract shell detection values (`"bash"`, `"zsh"`, `"fish"`, `"sh"`) into a
   named constant.
2. Parallelize session existence and pane listing work in `getStatus()`.

## Current state (from `src/status.ts`)

- Shell detection list is already centralized as:
  - `const SHELLS = new Set(["bash", "zsh", "fish", "sh"]);`
- `getStatus()` currently does:
  1. `await tmux.hasSession(session)`
  2. early return if false
  3. `await tmux.listPanes(session)`

So item (1) is effectively present already, while item (2) remains.

## Proposed changes

### 1) Shell list constant

- Keep the shell names centralized in one constant and ensure `paneIsRunning()`
  references only that constant.
- If needed for readability, split into:
  - `const SHELL_COMMANDS = ["bash", "zsh", "fish", "sh"] as const;`
  - `const SHELLS = new Set(SHELL_COMMANDS);`
- No behavior change intended.

### 2) Parallelize `hasSession()` + `listPanes()`

Refactor `getStatus()` to start both operations together and await them with
`Promise.all`.

Planned shape:

1. Build `session` as today.
2. Start both async calls immediately:
   - `hasSessionPromise = tmux.hasSession(session)`
   - `panesPromise = tmux.listPanes(session)`
3. Await with `Promise.all` and handle the "session missing" path safely.
4. Preserve current output contract:
   - if session is absent -> `{ workers: [], daemon: { running: false } }`

## Edge cases and correctness notes

- **No-session behavior:** Parallelization must not surface an error when there
  is simply no tmux session.
- **Race condition:** Session may disappear between checks; keep behavior stable
  by treating missing-session pane-list errors as non-fatal and returning the
  same "not running" status.
- **Unexpected tmux errors:** Non-missing-session errors should still propagate.

## Implementation steps

1. Update shell command constants only if needed (keep behavior identical).
2. Refactor `getStatus()` to run session check and pane listing concurrently via
   `Promise.all`.
3. Add/adjust minimal error handling around pane listing so missing-session
   remains a clean "not running" result.
4. Keep worker/daemon status computation unchanged once panes are available.

## Verification

1. Run unit tests (`deno task test:unit`).
2. Add/update unit tests for `getStatus()` to cover:
   - session absent path
   - session present path
   - session disappears/race-like missing-session error from pane listing
3. Optional lint/typecheck (`deno lint`) to ensure no typing regressions.

## Success criteria

- Shell detection values are defined in one explicit constant location.
- `getStatus()` starts `hasSession()` and `listPanes()` concurrently and awaits
  via `Promise.all`.
- Behavior for no active session remains unchanged.
