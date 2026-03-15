# Plan: Extract config/session/worker helpers in `src/cli.ts`

## Goal

Remove repeated CLI patterns in `src/cli.ts` without changing behavior:

1. Repeated `findConfig()` + `loadConfig()` flow
2. Repeated worker lookup/validation (`send`, `attach`)
3. Repeated tmux session existence check (`send`, `attach`)

## Scope

- Modify only `src/cli.ts`
- No behavior or error-message regressions intended
- No new features

## Current duplication inventory

### 1) `findConfig()` + `loadConfig()` repetition

The pair appears in 7 places (the prompt mentions 5, but current file has 7):

- `up()` at `src/cli.ts:170-171`
- `down()` at `src/cli.ts:406-407` (inside `try`)
- `status()` at `src/cli.ts:461-462`
- `send()` at `src/cli.ts:496-497`
- `attach()` at `src/cli.ts:520-521`
- `approval()` at `src/cli.ts:647-648`
- `main()` -> `case "daemon"` at `src/cli.ts:786-787`

### 2) Worker validation duplication (`send`, `attach`)

Same lookup + error path appears twice:

- `send()` at `src/cli.ts:500-508`
- `attach()` at `src/cli.ts:524-532`

### 3) Session existence check duplication (`send`, `attach`)

Same `tmux.hasSession` + error appears twice:

- `send()` at `src/cli.ts:510-513`
- `attach()` at `src/cli.ts:534-537`

## Proposed extractions

### A) Config-loading helper

Add a helper near existing utility functions:

- `async function loadCliConfig(): Promise<{ configPath: string; config: ConfigType }>`

Behavior:

- Calls `findConfig()`
- Calls `loadConfig(configPath)`
- Returns both values so callers that print path (`up`) keep current output

Notes:

- Prefer deriving return type from `loadConfig` to avoid guessing exported
  config types. Example:
  `type LoadedConfig = Awaited<ReturnType<typeof loadConfig>>`
- In `down()`, keep current tolerant behavior by wrapping `loadCliConfig()` in
  the existing `try/catch`.

### B) Worker resolution helper

Add helper:

- `function requireWorker(config: LoadedConfig, workerName: string): LoadedConfig["workers"][number]`

Behavior:

- Finds worker by name
- On missing worker, preserves current error text and exits (`Deno.exit(1)`)
- Returns worker object for potential future use

Usage:

- Replace duplicate blocks in `send()` and `attach()`

### C) Session assertion helper

Add helper:

- `async function requireActiveSession(session: string): Promise<void>`

Behavior:

- Checks `tmux.hasSession(session)`
- On missing session, preserves current message and exits

Usage:

- Replace duplicate checks in `send()` and `attach()`

## Implementation steps

1. Add shared types and helper functions in `src/cli.ts` (near existing small
   utilities).
2. Refactor call sites to use `loadCliConfig()`:
   - `up`, `down` (inside `try`), `status`, `send`, `attach`, `approval`,
     `daemon` command case.
3. Refactor `send()` and `attach()` to use:
   - `requireWorker(config, workerName)`
   - `await requireActiveSession(session)`
4. Ensure no output text changes for existing user-facing errors/logging.
5. Run targeted checks.

## Edge cases to preserve

- Missing config still throws `No jackops.yaml found in current directory` via
  existing path.
- `down()` must continue to work without config (current `try/catch` fallback
  behavior).
- Unknown worker should still print available names and exit non-zero.
- Missing tmux session should still print `Run 'jackops up' first.` and exit
  non-zero.

## Simpler alternative considered

- Inline helper just for `send/attach` (leave config duplication untouched).
  Rejected because the stated task explicitly includes extracting the
  config-loading repetition.

## Verification plan

1. `deno task test:unit`
2. Manual smoke checks:
   - `jackops send <bad-worker> "msg"` in a configured repo: validate unchanged
     error
   - `jackops attach <bad-worker>`: validate unchanged error
   - `jackops send <valid-worker> "msg"` with no active session: validate
     unchanged error
3. `deno lint` (only if this repo expects lint in workflow after edits)

## Success criteria

- All three duplication categories are reduced to single helpers.
- No command behavior changes aside from internal refactor.
- Tests/lint (if run) pass.
