# AGENTS.md

See [README.md](README.md) for project overview, usage, and development
commands.

## Key conventions

- Imports use the deno.json import map (`@std/assert`, `@std/yaml`), not inline
  `jsr:` specifiers
- Agent spawn commands must work as tmux send-keys input (shell string, not
  argv)
- Worker names and project names must match `/^[a-zA-Z0-9_-]+$/` (validated in
  config.ts)
- Worktree naming: `.w-<project>-<worker>`, branch: `jackops/<project>/<worker>`
- Git worktree operations must be sequential (shared repo metadata, no parallel
  `git worktree add`)
- `Deno.makeTempDir()` on macOS returns symlink paths (`/var/folders/...`), use
  `Deno.realPath()` when comparing with git output

## File layout

```
src/cli.ts          Entry point, subcommand dispatch (up/down/status/send/attach/tasks)
src/config.ts       YAML config parser, validation
src/agents.ts       Agent types, spawn commands, shell escaping
src/tmux.ts         Typed tmux wrappers (session, window, pane operations)
src/worktree.ts     Git worktree lifecycle (create, remove, list, cleanup)
src/status.ts       Worker liveness detection via tmux pane state
src/task-queue.ts   Filesystem-based task queue (JSON files in tasks/{pending,current,complete,rejected}/)
src/subprocess.ts   Shared Deno.Command runner
test/               Unit tests (*_test.ts), integration tests (*_integration_test.ts), e2e (e2e_test.ts)
```

## Testing

- Integration and e2e tests require tmux to be running
- All integration tests use `try/finally` for cleanup (tmux sessions, temp
  repos, worktrees)
- Test setup helpers (`makeTempGitRepo`) validate git command success
- `sanitizeResources: false` and `sanitizeOps: false` are set on integration
  tests (subprocess resource leaks)

## Shell alias

A global alias exists in `~/.zshrc`:

```bash
alias jackops="deno run --allow-run --allow-read --allow-write --allow-env ~/Code/agentic-coding/jackops/src/cli.ts"
```

Manual testing from any directory (e.g. `~/Code/tmp`): `jackops tasks init`,
`jackops tasks add "..."`, `jackops tasks`.

## GitHub

This repo is owned by `AdriaanVE`. If `gh` commands fail with repo access
errors, run `gh auth switch --user AdriaanVE`.

## Agent CLI flags

| Agent    | Command                        |
| -------- | ------------------------------ |
| claude   | `claude '<prompt>'`            |
| codex    | `codex --full-auto '<prompt>'` |
| opencode | `opencode run '<prompt>'`      |
| gemini   | `gemini '<prompt>'`            |
