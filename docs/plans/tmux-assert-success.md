# Plan: Extract `assertSuccess()` helper in `tmux.ts`

## Current state

`src/tmux.ts` contains 12 inline `if (!success) throw new Error(...)` blocks
that follow an identical pattern:

```ts
const { success, stderr } = await run([...]);
if (!success) throw new Error(`tmux <command> failed: ${stderr}`);
```

### Full catalog of instances

| #  | Line | Function           | Error label                     |
| -- | ---- | ------------------ | ------------------------------- |
| 1  | 16   | `createSession`    | `tmux new-session failed`       |
| 2  | 21   | `killSession`      | `tmux kill-session failed`      |
| 3  | 35   | `createWindow`     | `tmux new-window failed`        |
| 4  | 47   | `sendKeys` (text)  | `tmux send-keys failed`         |
| 5  | 56   | `sendKeys` (enter) | `tmux send-keys (enter) failed` |
| 6  | 72   | `capturePane`      | `tmux capture-pane failed`      |
| 7  | 90   | `listWindows`      | `tmux list-windows failed`      |
| 8  | 113  | `listPanes`        | `tmux list-panes failed`        |
| 9  | 144  | `selectWindow`     | `tmux select-window failed`     |
| 10 | 154  | `switchClient`     | `tmux switch-client failed`     |
| 11 | 168  | `displayMessage`   | `tmux display-message failed`   |
| 12 | 183  | `renameWindow`     | `tmux rename-window failed`     |

### Non-throwing usages of `success` (do NOT change)

- **`hasSession`** (line 10-11): returns `success` as a boolean -- intentionally
  non-throwing.
- **`sessionCreated`** (line 130): returns `null` on failure -- intentionally
  non-throwing.

## Proposed changes

### 1. Add `assertSuccess` helper

Add a module-private helper near the top of the file, after the existing `run()`
function:

```ts
function assertSuccess(
  result: { success: boolean; stderr: string },
  label: string,
): void {
  if (!result.success) throw new Error(`${label}: ${result.stderr}`);
}
```

**Signature rationale:**

- Takes the `result` object directly (not destructured), so callers can still
  destructure `stdout` separately.
- `label` is a free-form string to preserve the existing error message format
  (e.g. `"tmux new-session failed"`).
- Returns `void` -- purely a guard.
- Module-private (no `export`) since it's an internal concern.

### 2. Replace all 12 inline checks

Each instance transforms from:

```ts
const { success, stderr } = await run([...]);
if (!success) throw new Error(`tmux <cmd> failed: ${stderr}`);
```

to:

```ts
const result = await run([...]);
assertSuccess(result, "tmux <cmd> failed");
```

For functions that also use `stdout`, destructure after the assert:

```ts
const result = await run([...]);
assertSuccess(result, "tmux capture-pane failed");
return result.stdout;
```

### Alternative: wrap `run()` itself

An alternative approach would be to create a `runOrThrow(args, label)` that
calls `run()` and asserts internally. This would reduce each call site to a
single line:

```ts
const { stdout } = await runOrThrow(["capture-pane", ...], "tmux capture-pane failed");
```

This is slightly more concise but changes the return type semantics of the run
wrapper. The `assertSuccess` approach is more conservative -- it adds a helper
without changing `run()`.

**Recommendation:** Use `assertSuccess` for now. If the team later prefers
`runOrThrow`, it's a trivial follow-up.

## Step-by-step implementation

1. Add the `assertSuccess` function after `run()` (line 7-8 area).
2. Update each of the 12 call sites listed above.
3. Preserve exact error message strings (keep labels identical to current
   messages).
4. Leave `hasSession` and `sessionCreated` untouched.
5. Run `deno test:unit` to verify no regressions.
6. Run `deno fmt` and `deno lint`.

## Risks and edge cases

- **Error message format change**: The current format is
  `"tmux X failed: <stderr>"`. The helper must reproduce this exactly. The
  colon-space between label and stderr is part of the format.
- **No behavioral change**: This is a pure refactor. No new error types, no
  changed throw conditions.
- **`sendKeys` has two separate run calls**: Both get their own `assertSuccess`
  call with distinct labels (`"tmux send-keys failed"` vs
  `"tmux send-keys (enter) failed"`).

## Acceptance criteria

- [ ] All 12 inline `if (!success) throw` blocks are replaced with
      `assertSuccess()` calls
- [ ] `hasSession` and `sessionCreated` remain unchanged
- [ ] Error messages are identical to current behavior
- [ ] `deno task test:unit` passes
- [ ] `deno fmt` and `deno lint` pass with no new warnings
- [ ] No exported API changes -- all public function signatures unchanged
