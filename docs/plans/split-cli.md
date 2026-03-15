# Plan: Split `src/cli.ts` into `src/cli/` modules

## Goal

Refactor the monolithic `src/cli.ts` into a small entry point plus focused
modules under `src/cli/`, with no CLI behavior changes.

## Constraints

- Preserve all existing commands, flags, output text, and exit codes.
- Keep current CLI invocation (`deno run ... src/cli.ts ...`) working.
- Avoid source-level behavior changes during the split.

## Current `src/cli.ts` logical groupings

`src/cli.ts` currently contains five distinct concerns:

1. Entry-point orchestration: `main()` argument parsing + command dispatch.
2. Command handlers:
   - `init`, `up`, `down`, `status`, `approval`, `send`, `attach`, `tasks`,
     `daemon`.
3. Shared CLI utilities:
   - `prompt`, `attachSession`, `findConfig`, `parseApproval`,
     `checkUnknownFlags`.
4. Daemon-env helpers:
   - `LLM_ENV_KEYS`, `writeDaemonEnv`, `daemonCommand`.
5. Static usage/help text:
   - `USAGE`.

These map cleanly to command-group modules and a small shared utilities layer.

## Proposed file structure

```text
src/
  cli.ts                      # preserved entry point (thin wrapper)
  cli/
    main.ts                   # runCli()/dispatch logic (formerly main)
    usage.ts                  # USAGE text
    context.ts                # shared utility helpers (prompt/config/flags/session checks)
    daemon-env.ts             # LLM_ENV_KEYS + writeDaemonEnv + daemonCommand
    commands/
      init.ts                 # init command handler
      up.ts                   # up command handler
      down.ts                 # down command handler
      status.ts               # status command handler
      approval.ts             # approval command handler
      worker.ts               # send + attach handlers (shared worker/session checks)
      tasks.ts                # tasks command handler + subcommand parsing
      daemon.ts               # daemon command handler
```

## Module responsibilities

### `src/cli.ts` (compatibility shim)

- Keep as executable entry file.
- Contains only:
  - `import { runCli } from "./cli/main.ts";`
  - `if (import.meta.main) runCli();`
- Purpose: preserve existing scripts, aliases, and docs that reference
  `src/cli.ts`.

### `src/cli/main.ts`

- Owns top-level parsing and switch dispatch.
- Imports USAGE, flag utilities, and command handlers.
- Keeps current error wrapper (`try/catch` + `console.error` + `Deno.exit(1)`).

### `src/cli/context.ts`

- Shared helpers currently reused across command groups:
  - config lookup/loading helper(s)
  - `parseApproval`
  - `checkUnknownFlags`
  - interactive prompt helper(s)
  - optional session/worker guard helpers used by worker commands
- Keeps exit behavior centralized to avoid drift.

### `src/cli/daemon-env.ts`

- Encapsulates daemon env-file creation and command-string generation used by
  `up`.
- Keeps these implementation details out of command-dispatch code.

### `src/cli/commands/*`

- One module per command group.
- `worker.ts` groups `send` + `attach` because both share worker lookup and
  session-existence checks.
- `tasks.ts` keeps all `tasks` subcommand logic together.

## Extraction sequence (safe, incremental)

1. Create `src/cli/usage.ts` and move `USAGE` constant first.
   - Verify: `jackops --help` output unchanged.
2. Create `src/cli/context.ts` and move pure helpers (`findConfig`,
   `parseApproval`, `checkUnknownFlags`, `prompt`).
   - Verify: typecheck passes.
3. Create `src/cli/daemon-env.ts` and move `LLM_ENV_KEYS`, `writeDaemonEnv`,
   `daemonCommand`.
   - Verify: `up` still compiles.
4. Move command handlers one-by-one into `src/cli/commands/*`:
   - Suggested order: `tasks`, `status`, `approval`, `worker`, `down`, `daemon`,
     `init`, `up` (largest last).
   - After each move, rewire imports and run tests/typecheck.
5. Create `src/cli/main.ts` and migrate the `main()` switch there.
6. Replace body of `src/cli.ts` with thin compatibility wrapper calling
   `runCli()`.
7. Final verification pass.

## Preserving CLI entry point behavior

To keep all existing integrations working:

- Keep `src/cli.ts` path and executable semantics intact.
- Preserve command parsing order and default/unknown-command handling.
- Preserve exact usage and error messages (especially user-visible strings
  checked by tests).
- Preserve `Deno.exit(code)` behavior in all command/error paths.

## Imports and dependency boundaries

- Command modules may import domain modules directly (`config.ts`, `tmux.ts`,
  `worktree.ts`, `daemon.ts`, etc.).
- Cross-command coupling should be avoided; shared logic lives in `context.ts`.
- `main.ts` depends on command modules; command modules must not depend on
  `main.ts`.

## Edge cases to protect

- Missing config behavior in commands that require config.
- `down()` fallback behavior when config is absent.
- Worker validation and session checks in `send`/`attach` remain identical.
- Signal handling in `daemon` command (`SIGINT` + abort controller).
- `--help`, `-h`, unknown flag, and unknown command exit codes.

## Simpler alternatives considered

1. Two-file split (`cli.ts` + `cli-commands.ts`):
   - Simpler migration but still leaves large mixed modules.
2. One file per command only (no shared `context.ts`):
   - Increases duplication risk during extraction.

Chosen structure balances maintainability with minimal churn.

## Verification plan

1. `deno task test:unit`
2. `deno lint`
3. Targeted command smoke checks:
   - `deno run --allow-run --allow-read --allow-write --allow-env --allow-net src/cli.ts --help`
   - invalid flag and invalid command exit behavior
   - `send/attach` error paths with missing session/unknown worker
4. Optional (if tmux available): one lifecycle smoke test (`up` + `status` +
   `down`) in a temp repo.

## Success criteria

- `src/cli.ts` remains the entry point but only delegates to `src/cli/main.ts`.
- Command handlers are split into focused modules under `src/cli/commands/`.
- Behavior and user-facing output remain unchanged.
- Tests/lint pass after refactor.
