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
src/cli.ts          Entry point, subcommand dispatch (up/down/status/send/attach)
src/config.ts       YAML config parser, validation
src/agents.ts       Agent types, spawn commands, shell escaping
src/tmux.ts         Typed tmux wrappers (session, window, pane operations)
src/worktree.ts     Git worktree lifecycle (create, remove, list, cleanup)
src/status.ts       Worker liveness detection via tmux pane state
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

## Agent CLI flags

| Agent    | Command                        |
| -------- | ------------------------------ |
| claude   | `claude '<prompt>'`            |
| codex    | `codex --full-auto '<prompt>'` |
| opencode | `opencode run '<prompt>'`      |
| gemini   | `gemini '<prompt>'`            |
