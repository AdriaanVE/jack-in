# Plan: `jackops approval <mode>` CLI command

Switch approval mode (`manual | auto | yolo`) while the swarm is running.

## Problem

Approval mode is currently set at startup and baked into Claude
`settings.local.json` files. To change it, you must `jackops down` and
`jackops up` again with `--approval <mode>`. This kills all worker sessions and
loses context.

## 3 Options

### Option A: Rewrite settings + signal file (recommended)

```
jackops approval <mode>
```

1. Validate mode is `manual | auto | yolo`
2. Load config to discover workers and their agent types
3. For each Claude worker: `writeClaudeSettings(wt, base, name, newMode)`
4. For orchestrator (if Claude):
   `mergeClaudeSettings(base, base, "orchestrator", newMode)`
5. Write new mode to `.jackops/approval-mode`
6. Daemon reads `.jackops/approval-mode` at start of each tick; if changed,
   update internal state and log

Claude reads `settings.local.json` live -- hook changes take effect immediately
without restarting the agent.

**Non-Claude agents (codex, opencode, gemini) are NOT switched.** Their approval
behavior is set at process launch time via CLI flags (e.g. codex `--full-auto`)
and cannot be changed at runtime. Switching would require killing the agent
process, restarting with different flags, and re-sending the current task --
losing all conversation context. This is deferred to a future TODO (see below).

For now, `jackops approval` only switches:

- Claude workers (via settings.local.json rewrite)
- Claude orchestrator (via mergeClaudeSettings)
- Daemon watchdog tier 3 behavior (via .jackops/approval-mode file)

Non-Claude workers keep running with their original approval behavior.

**Pros**: Live switch, no restart, no state loss, daemon picks up naturally
**Cons**: Daemon needs small change to poll the file (~10 lines). Non-Claude
agents not switched.

### Option B: Restart daemon with new mode

1. Rewrite all Claude settings files
2. Kill daemon process in dashboard window
3. Re-send `jackops daemon --approval <mode>` to dashboard

**Pros**: Simple, daemon gets mode via CLI arg **Cons**: Loses in-memory worker
state (watchdog timers, LLM eval counts, pane snapshots). Brief monitoring gap
while daemon restarts.

### Option C: Edit jackops.yaml + reload

1. Programmatically edit `orchestrator.approval` in `jackops.yaml`
2. Rewrite all Claude settings files
3. Signal daemon to reload config

**Pros**: Config file stays in sync for next `jackops up` **Cons**: Most
complex, YAML editing is fragile (comments, formatting), still needs daemon
signal mechanism

## Decision: Option A

Option A is cleanest. The signal file pattern already exists in the codebase
(`.jackops/signals/`). No restart means no lost state. The daemon poll is
trivial -- one `readTextFile` per tick (every 5s).

## Implementation plan

### 1. CLI command (`src/cli.ts`)

Add `approval` command to the top-level switch:

```
jackops approval <mode>   # Switch approval mode live
jackops approval          # Show current mode
```

- Parse `<mode>`, validate with existing `parseApproval()` or similar
- Load config with `loadConfig()` to get worker list and agent types
- Discover worktree paths for each worker
- For each **Claude** worker: `writeClaudeSettings(wt, base, name, newMode)`
- For orchestrator (if **Claude**):
  `mergeClaudeSettings(base, base, "orchestrator", newMode)`
- Skip non-Claude workers (codex, opencode, gemini) -- print a note that they
  keep their original approval behavior
- Write mode string to `.jackops/approval-mode`
- Print confirmation: `Approval mode switched to <mode>`
- If non-Claude workers exist, print:
  `Note: <names> (codex/opencode/gemini)
  keep their original approval mode -- live switching requires restart (not yet
  supported)`
- If no mode arg: read `.jackops/approval-mode` (or fall back to config) and
  print current mode

### 2. Daemon poll (`src/daemon.ts`)

At the top of `tick()`, before the worker loop:

```typescript
const modeFile = join(base, ".jackops", "approval-mode");
try {
  const newMode = (await Deno.readTextFile(modeFile)).trim();
  if (isApprovalMode(newMode) && newMode !== approval) {
    log.info`Approval mode changed: ${approval} -> ${newMode}`;
    approval = newMode;
  }
} catch {
  // File doesn't exist yet -- use startup mode
}
```

This requires `approval` in the daemon loop to be `let` instead of passed as a
const parameter. Either:

- Make `approval` a mutable variable in the daemon loop scope
- Or wrap it in a simple `{ current: ApprovalMode }` object

### 3. Skill / docs update

- Add `jackops approval <mode>` to `.claude/skills/jackops/SKILL.md`
- Add to `docs/init-instructions.md` approval modes section (mention live
  switching)

### 4. Tests

- Unit test: `approval` command validates mode
- Unit test: daemon picks up mode change from file
- Unit test: `jackops approval` (no arg) prints current mode

## Issues flagged by Codex review

### 1. mergeClaudeSettings() won't remove old PermissionRequest hooks

When switching to `manual`, `buildClaudeSettings()` only emits a `Stop` hook.
But `mergeClaudeSettings()` only touches events that are present in the new
settings -- it won't remove the existing `PermissionRequest` hook left over from
`auto` or `yolo` mode.

**Fix**: When switching mode, `mergeClaudeSettings()` needs to explicitly remove
jackops `PermissionRequest` hooks if the new mode is `manual`. Either:

- Always overwrite the hooks section for jackops-managed events
- Or add a removal step for `PermissionRequest` when `manual`

### 2. Reset watchdog escalation state on mode change

If a task was escalated to user (`escalatedToUser: true`) under `manual` mode
and then mode switches to `auto`, the daemon won't re-evaluate because
`escalatedToUser` is still set. Same for `llmEvalCount`.

**Fix**: When mode changes, reset `escalatedToUser` and `llmEvalCount` for all
workers with current tasks.

### 3. Atomic settings writes

Current `writeClaudeSettings()` and `mergeClaudeSettings()` write directly to
the file. Claude could read partial JSON during the write.

**Fix**: Write to a temp file, then `Deno.rename()` (atomic on POSIX).

### 4. `jackops status` shows config mode, not runtime mode

Status reads approval mode from config, not from `.jackops/approval-mode`. After
a runtime switch, status would show the old mode.

**Fix**: `getStatus()` should check `.jackops/approval-mode` first, fall back to
config.

## Edge cases

- **Race condition**: CLI writes settings while daemon is mid-tick. Not a
  problem -- settings rewrite is atomic (write tmp + rename), and daemon reads
  approval-mode file at tick start before processing workers.
- **Non-Claude agents**: Not switched. They keep their original approval
  behavior. Daemon watchdog tier 3 behavior does change for them since it reads
  the `approval` variable.
- **Orchestrator agent**: Uses `mergeClaudeSettings()` (non-destructive merge)
  so existing user settings are preserved.
- **No swarm running**: Command should still work (just rewrites settings for
  next `jackops up`). Skip daemon signal file if no session exists.

## TODO: Non-Claude agent approval switching

Deferred. Switching approval mode for codex/opencode/gemini requires:

1. **Kill the agent process** in its tmux window
2. **Re-spawn with different flags** (e.g. codex without `--full-auto` for
   manual mode, or with `--full-auto` for yolo)
3. **Re-send the current task** (if any) to the new process

This loses all conversation context (chat history, in-memory state). There is no
save/restore mechanism for agent sessions today.

To implement this later:

- Add `spawnCommand` variants per approval mode in `src/agents.ts` (e.g. codex
  with and without `--full-auto`)
- Add `tmux.killWindow()` + re-create logic
- Re-claim and re-send the current task from `.jackops/tasks/current/`
- Warn user that conversation context will be lost
- Consider: only restart if the mode actually changes behavior for that agent
  (e.g. switching `auto` -> `yolo` doesn't matter for codex since it's always
  full-auto anyway)

## Files to modify

| File                              | Change                                              |
| --------------------------------- | --------------------------------------------------- |
| `src/cli.ts`                      | Add `approval` command                              |
| `src/daemon.ts`                   | Poll `.jackops/approval-mode` at tick start         |
| `src/config.ts`                   | Export `isApprovalMode()` type guard if not already |
| `.claude/skills/jackops/SKILL.md` | Document new command                                |
| `test/daemon_test.ts`             | Test mode file polling                              |
| `test/cli_test.ts` (or similar)   | Test approval command validation                    |
