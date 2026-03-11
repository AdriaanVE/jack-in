# TODO

## Code review findings (deferred)

Issues identified during review that are not worth fixing in the MVP but should
be revisited as the codebase grows.

### From internal review (Claude + Codex)

**cli.ts**

- [ ] Extract config loading helper -- `findConfig()` + `loadConfig()` repeated
      in 5 functions
- [ ] Extract worker validation helper -- lookup + error message duplicated in
      `send()` and `attach()`
- [ ] Extract session existence check -- same guard in `send()` and `attach()`
- [ ] Stdin prompt in `down()` reads 4-byte buffer -- fragile, replace with
      readline when adding more prompts
- [ ] Partial startup failure leaves orphaned tmux session -- consider rollback
      or clearer recovery guidance

**tmux.ts**

- [ ] 9x inline `if (!success) throw new Error(...)` -- extract
      `assertSuccess()` helper when adding more commands
- [ ] Tab-split parsing of tmux output has no validation on field count
- [ ] `capturePane` default of 50 lines is a magic number

**worktree.ts**

- [ ] Git retry logic for "branch already exists" is hardcoded in `create()` --
      consider cleaner recovery
- [ ] Sequential worktree removal in `cleanup()` -- could parallelize if git
      supports it safely

**status.ts**

- [ ] Shell detection list `["bash", "zsh", "fish", "sh"]` should be a constant
- [ ] `hasSession()` and `listPanes()` called sequentially -- could parallel
      with `Promise.all()`

**General**

- [ ] `attach()` uses raw `Deno.Command` instead of tmux module -- intentional
      (needs inherited stdio), but add an interactive variant to `tmux.ts` if
      more interactive commands are added
- [x] Integration + e2e tests added (38 tests across 7 files)

### From Codex review

- [ ] `down` requires config file for session cleanup -- works without it for
      worktrees, but can't kill tmux session without knowing project name.
      Consider storing project name in tmux session environment variable.
- [ ] Status shows "idle" when agent exited but shell is still open -- accurate
      but might confuse users expecting "done"

### From Codex test suite review

**Fixed:**

- [x] `isAgentType` accepted prototype keys (`toString`, `hasOwnProperty`) --
      switched to `Object.hasOwn()`
- [x] Integration/e2e tests leaked resources on failure -- added `try/finally`
      cleanup
- [x] `makeTempGitRepo()` didn't validate `exec()` results -- git failures now
      throw immediately
- [x] Stale comment in e2e_test.ts said "echo fake agent" but config uses
      claude/codex

**Deferred (missing coverage, not bugs):**

- [ ] `getStatus()` liveness classification (running/idle/gone) has no unit
      tests -- only `formatStatus` is tested
- [ ] Untested CLI branches: `attach`, `down` with "N" confirmation, `down`
      without config fallback, unknown command
- [ ] Config parser edge cases: invalid YAML root type, case sensitivity of
      agent names, empty worker name
- [ ] Flaky fixed sleeps (300ms/500ms) for tmux output -- could use retry loop
      with timeout
- [ ] Hardcoded test session names could collide with real sessions -- low risk
      but could randomize
- [ ] Weak assertions: heavy reliance on `includes()` and `>= N` checks
- [ ] Worktree/tmux error path coverage: parse failures, `removeByPath` errors,
      tmux wrapper failures

## Distribution

- [ ] `deno install` -- creates wrapper script in `~/.deno/bin/jackops`,
      requires Deno on target machine
- [ ] `deno compile` -- standalone binary (~80-100MB), no runtime needed,
      supports cross-compilation (macOS arm/x64, Linux)
- [ ] GitHub Releases -- host compiled binaries + install script (`curl | bash`)
- [ ] JSR package -- publish to Deno registry, install via
      `deno install jsr:@adriaanve/jackops`
- [ ] Homebrew tap -- formula pointing at GitHub Release binaries

## Future: Dashboard (inspired by agentsview)

- [ ] Web UI for viewing active worker sessions, task queue, and activity feed
- [ ] Session history -- index past agent conversations from disk into SQLite
      with FTS5 search
- [ ] Analytics -- activity heatmaps, tool usage, per-worker metrics
- [ ] Live updates via SSE as workers produce output
- [ ] Reference: [wesm/agentsview](https://github.com/wesm/agentsview) --
      Go/SQLite/Svelte 5 stack, supports 11 agents
