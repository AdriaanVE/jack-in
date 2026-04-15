# Go Rewrite TODO

Missing features and gaps identified by cross-comparing the Deno implementation with the Go implementation.

## High Priority (functional gaps)

- [x] **`status` command** -- CLI-queryable worker/daemon/task status with `--json` output. Queries tmux panes for worker state (working/waiting/stopped/gone), reads current-task files, detects daemon running state, shows session uptime and approval mode.
- [x] **`approval` command** -- Read/switch approval mode at runtime. No args shows current mode and source. With mode arg, rewrites `settings.local.json` for each Claude worker worktree and the orchestrator, writes runtime mode file.
- [x] **`send <worker> <message>` command** -- Send a message directly to a worker's tmux pane. Validates worker exists and session is active.
- [x] **`attach <worker>` command** -- Jump to a specific worker's tmux window. Handles both in-tmux (`select-window`) and external terminal (`tmux attach`).

## Medium Priority (operational gaps)

- [x] **Structured logging** -- Dual slog handlers: console (info+) and buffered file sink to `.jack-in/daemon.log` (debug+). Log file truncated on daemon restart.
- [x] **LLM API flexibility** -- Auto-detects provider: direct Anthropic API (`ANTHROPIC_API_KEY`) with priority over Azure Foundry (`ANTHROPIC_FOUNDRY_RESOURCE` + `ANTHROPIC_FOUNDRY_API_KEY`).

## Low Priority (Go already improves on Deno in other areas)

- [x] **TUI dashboard** -- Go has a full Bubbletea v2 dashboard that Deno lacks.
- [x] **CLI auto-completion** -- Go has Cobra shell completions that Deno lacks.
- [x] **Lazygit TUI button** -- Add button in TUI to launch lazygit if installation is detected on the system.
- [ ] **Dirty worktree session resume** -- When worktrees are dirty on `jackin up`, offer to resume the previous session instead of resetting. Investigate how to map dirty worktrees back to their correct agent sessions/tasks.
- [ ] **Persistent sessions** -- Make agent sessions persistent across restarts (similar to Claude Code `/resume`). Save session state so workers can pick up where they left off.
- [ ] **Workers-only mode** -- Support running without an orchestrator (workers only). Add this as an option in `jackin init`.
- [x] **Task state timestamps** -- Record a timestamp when tasks transition between states (e.g. pending->current, current->review, review->done/rejected). Display these in the TUI task popup for review, done, and rejected categories.
- [x] **Matrix animation** -- Port the Matrix rain animation from sysc-Go as a TUI effect.
- [x] **Task rejection pauses task** -- When a task is rejected, keep it in a paused/rejected state instead of automatically moving it back to pending. Let the orchestrator or user decide whether to retry with feedback or reassign to a different worker. Worker remains available for other tasks.
