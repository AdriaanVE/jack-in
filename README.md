```
 _____   _                          _                         __       
|_   _| | |                        | |                       / _|      
  | |   | | ___ __   _____      __ | | ___   _ _ __   __ _  | |_ _   _ 
  | |   | |/ / '_ \ / _ \ \ /\ / / | |/ / | | | '_ \ / _` | |  _| | | |
 _| |_  |   <| | | | (_) \ V  V /  |   <| |_| | | | | (_| | | | | |_| |
|_____| |_|\_\_| |_|\___/ \_/\_/   |_|\_\\__,_|_| |_|\__, | |_|  \__,_|
                                                      __/ |            
                                                     |___/             
```

# JACK-IN

tmux-native multi-agent swarm orchestrator.

Spawns and coordinates AI coding agents (Claude Code, Codex, Opencode, Gemini)
across git worktrees. TUI dashboard, task queue, review cycle, and optional
remote control via ngrok, all from the terminal.

> **Status: experimental (v0.1).** This is a personal pet project. I built it to
> understand how to get multiple AI coding agents running together on real tasks,
> while keeping everything visible in tmux so I can watch what they're doing and
> step in when needed. The major AI labs are now shipping their own multi-agent
> orchestration (see
> [Claude Code teams](https://code.claude.com/docs#run-agent-teams-and-build-custom-agents),
> [Oh My Claude Code](https://github.com/yeachan-heo/oh-my-claudecode)), which
> may supersede this tool. Use at your own discretion.

## Why

I wanted a way to run multiple AI agents in parallel without losing visibility.
Most multi-agent tools either hide behind a web UI, lock you into one framework,
or shell out to CLIs without showing you what's happening. JACK-IN stays in tmux
where the work happens. Every agent runs in its own window, so you can see
exactly what each one is doing, send it a message, or take over manually at any
point.

## Quick Start

### Install

```bash
# Homebrew (macOS / Linux)
brew install AdriaanVE/tap/jackin

# Or with Go 1.25+
go install github.com/AdriaanVE/jack-in/cmd/jackin@latest

# Or build from source
git clone https://github.com/AdriaanVE/jack-in.git
cd jack-in && go build -o jackin ./cmd/jackin
```

### API Credentials (for auto mode)

Skip this if you use `manual` or `yolo` approval mode, or if your agent CLIs
(claude, codex) are already authenticated. API credentials are only needed for
`auto` mode's LLM-based permission evaluation.

If you use `auto` approval mode, the daemon needs API credentials to evaluate
permission prompts. Set one of these before running `jackin up`:

```bash
# Option 1: Anthropic API key
export ANTHROPIC_API_KEY=sk-...

# Option 2: Azure Foundry
export ANTHROPIC_FOUNDRY_RESOURCE=your-resource
export ANTHROPIC_FOUNDRY_API_KEY=your-key

# Option 3: point to a credentials file (daemon sources it automatically)
export JACKIN_ENV_FILE=~/dotenvs/claude.env
```

Without credentials, auto mode falls back to `claude --print` (headless), which
requires the `claude` CLI to be authenticated.

Then, in any git repo:

```bash
cd your-project
jackin init              # Interactive setup wizard
jackin up                # Start the swarm
```

`jackin init` detects which agent CLIs you have installed, asks what you want to
work on, and generates a `jack-in.yaml` config file. It uses an LLM to turn your
description into structured tasks with dependencies and acceptance criteria. You
can also pass `--template` to generate a commented config template without the
LLM, or create `jack-in.yaml` by hand (see [Manual Configuration](#manual-configuration)
below).

Once `jack-in.yaml` exists, `jackin up` spawns the TUI dashboard, creates git
worktrees for each worker, starts the agents, and begins assigning tasks.

```bash
jackin down              # Stop everything and clean up worktrees
```

To reconnect to a running swarm: `tmux attach -t jackin-<project>`.

## Manual Configuration

Instead of `jackin init`, you can create `jack-in.yaml` directly:

```yaml
project: my-app
branch: main             # Target branch for merging approved work

workers:
  - agent: claude
    prompt: "implement features and fix bugs"
  - agent: codex
    prompt: "write tests and documentation"

orchestrator:
  approval: auto         # manual | auto | yolo
  agent: claude          # LLM orchestrator reviews work

tasks:
  - summary: "Document the auth module"
  - summary: "Refactor database layer"
    description: "Extract DB queries into a repository pattern"
```

See [Configuration Reference](#configuration-reference) for all options.

## Commands

### Setup and Lifecycle

```bash
jackin init                    # Interactive LLM-driven setup
jackin init --template         # Generate template without LLM
jackin init --agent codex      # Use specific agent for setup
jackin up                      # Start swarm (TUI + workers + daemon)
jackin up --approval auto      # Override approval mode
jackin up --no-orchestrator    # Skip daemon (workers only)
jackin up --no-orchestrator-agent  # Daemon but no LLM orchestrator
jackin down                    # Stop everything, clean worktrees
jackin status                  # Show worker states and task counts
jackin status --json           # JSON output for scripts
```

### Approval Mode

```bash
jackin approval                # Show current mode
jackin approval manual         # Switch to manual (prompts pause workers)
jackin approval auto           # Switch to auto (LLM evaluates safety)
jackin approval yolo           # Switch to yolo (auto-approve everything)
```

### Worker Management

```bash
jackin attach                  # Attach to dashboard
jackin attach worker-1         # Attach to worker's window
jackin attach orchestrator     # Attach to orchestrator window
jackin send worker-1 "message" # Send text to worker's pane
jackin capture worker-1        # Capture last 50 lines of output
jackin capture worker-1 -n 100 # Capture last 100 lines
jackin reset worker-1          # Kill and respawn worker (fresh agent)
jackin refresh worker-1        # Reset worktree to target branch
jackin prune                   # Remove orphaned worktrees
```

### Task Management

```bash
jackin tasks                   # List all tasks with counts
jackin tasks init              # Create queue directories
jackin tasks add "summary"     # Add task to pending
jackin tasks add "fix bug" --desc "Detailed description"
jackin tasks complete <id>     # Force current -> review (unstick)
jackin tasks approve <id>      # Approve reviewed task
jackin tasks reject <id> "feedback"  # Reject with feedback
jackin tasks retry <id>        # Retry rejected task (same worker)
jackin tasks drop <id>         # Delete rejected task permanently
jackin tasks cancel <id>       # Cancel in-progress task
jackin tasks validate          # Check queue integrity
```

## Approval Modes

Control how permission prompts are handled:

| Mode     | Behavior                                                    |
| -------- | ----------------------------------------------------------- |
| `manual` | Workers pause on permission prompts. You approve manually.  |
| `auto`   | LLM evaluates safety. Safe operations auto-approved.        |
| `yolo`   | Everything auto-approved. Workers never pause.              |

Switch modes live while the swarm is running. Claude workers pick up changes
immediately. The daemon applies it on the next tick for watchdog evaluations.

### Auto Mode: LLM Permission Evaluation

In `auto` mode, the daemon's watchdog detects when a worker is stuck (pane
unchanged, waiting for input). It captures the pane content and sends it to an
LLM for safety evaluation.

The LLM backend priority:
1. **Anthropic API** - if `ANTHROPIC_API_KEY` is set
2. **Azure Foundry** - if `ANTHROPIC_FOUNDRY_RESOURCE` and `ANTHROPIC_FOUNDRY_API_KEY` are set
3. **Headless Claude** - fallback using `claude --print -p <prompt>`

See [API Credentials](#api-credentials-for-auto-mode) for setup.

The evaluation determines:
- **Status**: working, waiting for approval, waiting for input, error, or done
- **Safety**: whether the requested operation is safe to auto-approve
- **Action**: keystroke to send (e.g., `y` for yes) or text response

Safe operations (file reads, git status, test runs) are auto-approved. Unsafe
operations (file deletions, git push, arbitrary commands) trigger a notification
so you can review manually.

## Remote Control

Access your swarm from anywhere via browser. Requires `ttyd` and `ngrok`:

```bash
brew install ttyd ngrok
ngrok authtoken <your-token>   # One-time setup
```

Press `R` in the TUI dashboard to start remote access:
1. Launches ttyd serving the tmux session on port 7681
2. Starts ngrok tunnel to expose it publicly
3. Copies the public URL to clipboard

Open the URL on your phone or another machine to view and control the swarm.
Press `R` again to stop.

## Architecture

```
tmux session: jackin-<project>
+------------------------------------------------------------------------+
|  window 0: dashboard                                                    |
|  +------------------------------------------------------------------+  |
|  |  TUI (Bubble Tea)                                                |  |
|  |  +------------+  +------------+  +------------+                  |  |
|  |  | worker-1   |  | worker-2   |  | orchestr.  |  <- worker cards |  |
|  |  | claude     |  | codex      |  | claude     |                  |  |
|  |  | working    |  | idle       |  | reviewing  |                  |  |
|  |  +------------+  +------------+  +------------+                  |  |
|  |                                                                  |  |
|  |  [= Tasks: 2 pending | 1 current | 0 review | 5 complete ]       |  |
|  |                                                                  |  |
|  |  +------------------------------------------------------------+  |  |
|  |  | bottom pane: orchestrator / selected worker output         |  |  |
|  |  | (swap with arrow keys or click cards)                      |  |  |
|  |  +------------------------------------------------------------+  |  |
|  +------------------------------------------------------------------+  |
|                                                                        |
|  window 1: worker-1     window 2: worker-2     window 3: cli           |
|  (claude agent)         (codex agent)          (user terminal)         |
+------------------------------------------------------------------------+

Daemon (background goroutine):
  - Assigns pending tasks to idle workers
  - Detects completion via signal files + Claude hooks
  - Moves tasks through queue states
  - Watchdog: detects stuck workers, evaluates permissions (auto mode)

Orchestrator agent (LLM in bottom pane):
  - Reviews completed work (git diff)
  - Approves or rejects tasks
  - Creates follow-up tasks
  - Intervenes when workers are stuck

Remote (optional):
  ttyd (port 7681) -> ngrok tunnel -> public URL -> browser
```

### Task Lifecycle

```
pending/ -> current/ -> review/ -> complete/
                          |
                          +-> rejected/ -+-> current/ (retry)
                                         +-> [deleted] (drop)
```

Task queue is filesystem-based at `.jack-in/tasks/`. Signal files in
`.jack-in/signals/` track worker state.

## Claude Code Skill

When working inside a jackin project, Claude Code can use the `/jackin` skill
for the full CLI reference. The skill is auto-installed at
`.claude/skills/jackin/` when running `jackin up`.

## Configuration Reference

```yaml
project: my-app              # Required. Session name prefix.
branch: main                 # Target branch for merging (default: jackin-develop)

workers:                     # At least one required
  - agent: claude            # claude | codex | opencode | gemini
    prompt: "your prompt"    # What this worker does
    # name: worker-1         # Auto-assigned if omitted
    # role: executor         # executor | planner | reviewer

orchestrator:
  approval: auto             # manual | auto | yolo (default: manual)
  agent: claude              # LLM type, or false to disable
  poll_interval: 5000        # Daemon tick interval ms (default: 5000)
  max_retries: 2             # Task retry limit (default: 2)

tasks:                       # Optional. Seeded on jackin up.
  - summary: "Task title"
    description: "Details"   # Optional
    files: ["src/"]          # Optional file hints
    acceptance:              # Optional completion criteria
      - "Tests pass"
    depends_on: ["Other task summary"]
```

## TUI Hotkeys

| Key       | Action                              |
| --------- | ----------------------------------- |
| `q`       | Quit                                |
| `?`       | Toggle help                         |
| `a`       | Toggle approval mode                |
| `R`       | Toggle remote access (ttyd+ngrok)   |
| `c`       | Open CLI window                     |
| `Enter`   | Activate selected card in bottom    |
| `Esc`     | Return to orchestrator in bottom    |
| `1-9`     | Quick-select worker card            |
| Arrows    | Navigate cards                      |

## Development

Requires [Go 1.25+](https://go.dev/) and [tmux](https://github.com/tmux/tmux).

```bash
go build -o jackin ./cmd/jackin   # Build
go test ./...                      # Run tests
```

Pre-commit hooks via [pre-commit](https://pre-commit.com/):

```bash
pre-commit install
```

## Requirements

- Go 1.25+
- tmux
- At least one agent CLI: `claude`, `codex`, `opencode`, or `gemini`
- Optional: `ttyd` and `ngrok` for remote access

## License

MIT
