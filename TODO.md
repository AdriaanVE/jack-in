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
- [x] Integration + e2e tests added (69 tests across 9 files)

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

## tmux input delivery

- [ ] Send large prompts via local file instead of tmux send-keys -- send-keys
      has length limits and encoding issues with special characters. Write
      prompt to a temp file and send a short command like `cat /path | agent` or
      use tmux load-buffer + paste-buffer

## Task queue

- [ ] Filesystem `claim()` is not atomic -- two workers polling simultaneously
      could race on the same task. Consider flock or atomic rename strategy.

## Daemon

- [ ] Compare polling vs Deno.watchFs for all daemon reactions (task assignment,
      completion detection, stall checks) -- currently hybrid: watchFs for idle
      waiting, polling for active monitoring
- [ ] Integration tests for daemon idle-watch resume and stall detection
      auto-respond (waiting_for_input) -- needs tmux + mock LLM endpoint

## Permission evaluation

- [ ] Make evaluator model configurable in `jackops.yaml` (currently hardcoded
      to sonnet via `ANTHROPIC_DEFAULT_SONNET_MODEL` env var)
- [ ] Support other API providers (OpenAI, Azure OpenAI, local models) --
      currently Anthropic Foundry only
- [ ] Stall detection timeout (60s) and LLM fetch timeout (30s) configurable in
      orchestrator config
- [ ] Permission-eval.sh uses prompt-based JSON extraction -- migrate to
      tool_use for structured output like the daemon's pane evaluator
- [x] Configurable approval mode: manual / auto / yolo in orchestrator config
- [ ] Integration tests for permission-eval.sh with mocked curl -- test
      safe/unsafe/API-failure response paths

## Distribution

- [ ] `deno install` -- creates wrapper script in `~/.deno/bin/jackops`,
      requires Deno on target machine
- [ ] `deno compile` -- standalone binary (~80-100MB), no runtime needed,
      supports cross-compilation (macOS arm/x64, Linux)
- [ ] GitHub Releases -- host compiled binaries + install script (`curl | bash`)
- [ ] JSR package -- publish to Deno registry, install via
      `deno install jsr:@adriaanve/jackops`
- [ ] Homebrew tap -- formula pointing at GitHub Release binaries

## Future: Terminal UI

- [ ] Interactive status with live-updating worker states, task progress, and
      log output in the dashboard tmux pane
- [ ] Worker selection -- navigate to a worker's tmux window from the TUI
- [ ] Library: [Im-Beast/deno_tui](https://github.com/Im-Beast/deno_tui) --
      Deno-native TUI framework with components, input handling, and styling

## Future: Dashboard (inspired by agentsview)

- [ ] Web UI for viewing active worker sessions, task queue, and activity feed
- [ ] Session history -- index past agent conversations from disk into SQLite
      with FTS5 search
- [ ] Analytics -- activity heatmaps, tool usage, per-worker metrics
- [ ] Live updates via SSE as workers produce output
- [ ] Reference: [wesm/agentsview](https://github.com/wesm/agentsview) --
      Go/SQLite/Svelte 5 stack, supports 11 agents

- [ ] Split up CLI.ts into multiple files (e.g. `cli/send.ts`, `cli/attach.ts`)
- [ ] split up daemon.ts into multiple files

- [] DO workers need the skill locally? I dont think so, only orchestrators do,
  check this in init
