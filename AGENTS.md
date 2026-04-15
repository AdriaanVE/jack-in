# AGENTS.md

See [README.md](README.md) for project overview, usage, and development
commands.

## Key conventions

- Agent spawn commands must work as tmux send-keys input (shell string, not
  argv)
- Worker names and project names must match `/^[a-zA-Z0-9_-]+$/` (validated in
  config)
- Worktree naming: `.w-<project>-<worker>`, branch: `jackin/<project>/<worker>`
- Git worktree operations must be sequential (shared repo metadata, no parallel
  `git worktree add`)

## File layout

```
cmd/jackin/main.go       Entry point
src/cmd/                 CLI commands (up, down, status, send, attach, tasks, init, etc.)
src/cmd/embed/           Embedded instruction files (init, orchestrator, worker, skill)
src/cmd/helpers.go       Shared CLI utilities (config loading, worker shell commands)
src/config/              YAML config parser, validation, defaults
src/daemon/              Orchestrator daemon (task assignment, watchdog, LLM eval, hooks)
src/domain/              Shared types (AgentType, ApprovalMode, TaskState, WorkerRole)
src/agent/               Agent type registry, spawn commands, shell escaping
src/tmux/                Typed tmux wrappers (session, window, pane operations)
src/worktree/            Git worktree lifecycle (create, remove, list, cleanup)
src/taskqueue/           Filesystem-based task queue (JSON files in .jack-in/tasks/)
src/ui/                  TUI dashboard (Bubble Tea)
src/marker/              Task completion marker detection
src/usage/               Claude Code token usage tracking
src/quotes/              Startup quotes
hooks/                   Claude Code hooks (stop, permission-eval, notify)
```

## Testing

```bash
go test ./...            # Run all tests
go test ./src/daemon/    # Run daemon tests only
```

- Integration tests that require tmux are skipped when tmux is not available
- Tests use `t.TempDir()` for isolated file operations

## Agent CLI flags

| Agent    | Command                        |
| -------- | ------------------------------ |
| claude   | `claude '<prompt>'`            |
| codex    | `codex --full-auto '<prompt>'` |
| opencode | `opencode run '<prompt>'`      |
| gemini   | `gemini '<prompt>'`            |
