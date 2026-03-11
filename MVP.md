# MVP

Config-driven parallel agent workers with worktree isolation.

One command to go from a YAML config to a running swarm of agents in tmux, each in its own git worktree.

## Commands

```bash
jackops up                          # Create worktrees, tmux session, spawn agents
jackops down                        # Kill agents, remove tmux session, cleanup worktrees
jackops status                      # List workers: name, agent, worktree, alive/done
jackops send <worker> <message>     # Send a message to a worker via tmux send-keys
jackops attach <worker>             # Switch to a worker's tmux window
```

## Config

```yaml
# jackops.yaml
project: my-app

workers:
  - name: backend
    agent: claude
    prompt: "Implement the REST API endpoints defined in spec.md. Focus on src/api/."
  - name: frontend
    agent: codex
    prompt: "Build the React components defined in spec.md. Focus on src/components/."
  - name: tests
    agent: claude
    prompt: "Write tests for the API and components. Focus on tests/."
```

Minimal config. Just name, agent, prompt. No roles, models, or counts yet.

## What it does

### `jackops up`

1. Read `jackops.yaml` from cwd
2. Create tmux session `jackops-<project>`
3. For each worker:
   a. `git worktree add .w-<name>` (branch: `jackops/<name>`)
   b. `tmux new-window -t jackops-<project> -n <name>`
   c. `tmux send-keys` to cd into worktree and start the agent CLI with the prompt

### `jackops down`

1. Kill tmux session `jackops-<project>`
2. Prompt before removing worktrees (they may have uncommitted work)
3. `git worktree remove .w-<name>` for each worker (if confirmed)

### `jackops status`

Plain text output:

```
JACKOPS -- my-app

  backend     claude    alive    .w-backend
  frontend    codex     alive    .w-frontend
  tests       claude    done     .w-tests
```

Worker state comes from checking if the tmux window still has a running agent process. No hook IPC -- just `tmux list-panes` and process status.

### `jackops send <worker> <message>`

```bash
tmux send-keys -t "jackops-<project>:<worker>" "<message>" C-m
```

For multi-line or complex prompts, write to a temp file and tell the agent to read it.

### `jackops attach <worker>`

```bash
tmux select-window -t "jackops-<project>:<worker>"
```

If you're outside the tmux session:

```bash
tmux attach -t "jackops-<project>" \; select-window -t "<worker>"
```

## Agent spawn commands

| Agent | Spawn command |
|-------|--------------|
| Claude Code | `claude -p "<prompt>"` |
| Codex | `codex --quiet "<prompt>"` |
| Opencode | `opencode run "<prompt>"` |
| Gemini | `gemini "<prompt>"` |

Agents run in one-shot prompt mode where possible. The worker's tmux window stays open after the agent exits so you can inspect output or restart manually.

## Project structure

```
jackops/
  src/
    cli.ts          # Entry point, argument parsing, subcommand dispatch
    config.ts       # Parse jackops.yaml
    tmux.ts         # Typed wrappers around tmux commands
    worktree.ts     # git worktree create/remove/list
    agents.ts       # Agent spawn commands per CLI type
    status.ts       # Read tmux state, format status output
  deno.json         # Import map, tasks, permissions
```

Six files. That's the whole MVP.

## Out of scope

- Task queue (workers get a one-shot prompt, not a task system)
- Review cycle
- Dashboard TUI (`jackops status` is enough)
- Claude Code hook IPC (all agents use the same tmux-based communication)
- Session reader (reading agent disk formats)
- Messaging bridge (remote control)
- Auto-restart of failed workers
- Merging worker results back to main

These become relevant after the MVP proves the concept works.
