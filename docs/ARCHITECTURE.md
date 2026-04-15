# JACK-IN Architecture

## Tech Stack

**Go** with Cobra CLI, Bubbletea TUI, and embedded instruction files.

## High-Level Overview

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                              TMUX SESSION                                    │
│                          (jackin-<project>)                                  │
│                                                                              │
│  ┌─────────────────────────────────────┐  ┌─────────────────────────────┐   │
│  │     WINDOW 0: dashboard-orchestrator │  │   WINDOW 1+: Workers        │   │
│  │  ┌─────────────────────────────────┐ │  │  ┌───────────────────────┐  │   │
│  │  │  PANE 0 (30%): Daemon + TUI     │ │  │  │  worker-1 (claude)    │  │   │
│  │  │                                 │ │  │  │  ┌─────────────────┐  │  │   │
│  │  │  ┌─────────┐    ┌───────────┐   │ │  │  │  │ Claude Code CLI │  │  │   │
│  │  │  │ Daemon  │───▶│    TUI    │   │ │  │  │  │ in worktree     │  │  │   │
│  │  │  │ (Go)    │    │(Bubbletea)│   │ │  │  │  └─────────────────┘  │  │   │
│  │  │  └─────────┘    └───────────┘   │ │  │  └───────────────────────┘  │   │
│  │  └─────────────────────────────────┘ │  │  ┌───────────────────────┐  │   │
│  │  ┌─────────────────────────────────┐ │  │  │  worker-2 (codex)     │  │   │
│  │  │  PANE 1 (70%): Orchestrator     │ │  │  │  ┌─────────────────┐  │  │   │
│  │  │                                 │ │  │  │  │ Codex CLI       │  │  │   │
│  │  │  ┌─────────────────────────────┐│ │  │  │  │ in worktree     │  │  │   │
│  │  │  │  Claude Code (LLM agent)    ││ │  │  │  └─────────────────┘  │  │   │
│  │  │  │  Reviews diffs, approves,   ││ │  │  └───────────────────────┘  │   │
│  │  │  │  rejects, creates tasks     ││ │  │                             │   │
│  │  │  └─────────────────────────────┘│ │  │  ... more workers ...       │   │
│  │  └─────────────────────────────────┘ │  └─────────────────────────────┘   │
│  └─────────────────────────────────────┘                                     │
└─────────────────────────────────────────────────────────────────────────────┘
```

## Components

### 1. Daemon (Mechanical Scheduler)

The daemon is a Go process that runs the tick loop. It makes no judgment calls.

**Responsibilities:**
- Assign pending tasks to idle workers
- Detect task completion via signal files
- Move tasks between states (pending → current → review)
- Refresh worktrees after task approval/rejection
- Run watchdog to detect stuck workers
- Auto-approve safe operations (when approval=auto)
- Emit state events to TUI

**Location:** Top pane of `dashboard-orchestrator` window

### 2. Orchestrator Agent (LLM Reviewer)

The orchestrator is an AI agent (Claude Code) that reviews completed work.

**Responsibilities:**
- Review git diffs from workers
- Approve or reject tasks
- Merge approved work to target branch
- Create follow-up tasks
- Handle rejected tasks (retry or drop)
- Unstick workers when signal detection fails

**Location:** Bottom pane of `dashboard-orchestrator` window

### 3. Workers (AI Implementers)

Workers are AI agents that implement tasks in isolated git worktrees.

**Responsibilities:**
- Receive task assignments via prompt
- Implement the task in their worktree
- Commit changes to their branch
- Signal completion via marker output

**Location:** Separate tmux windows (one per worker)

### 4. TUI Dashboard (Bubbletea)

Optional terminal UI that displays swarm status.

**Features:**
- Worker status cards
- Task counts by state
- Live log panel
- Hotkeys for common actions

**Location:** Same pane as daemon

## Agent Instructions

Workers and the orchestrator receive **distinct instructions** embedded in the binary.

### Worker Instructions (`src/cmd/embed/worker-instructions.md`)

Sent to each worker when assigned a task:
- Task summary and description
- Acceptance criteria
- Completion marker format (`JACKIN_TASK_COMPLETE:<taskId>`)
- Guidelines for working in the worktree

### Orchestrator Instructions (`src/cmd/embed/orchestrator-instructions.md`)

Written to `.jack-in/orchestrator-prompt.md` at startup:
- Role explanation (reviewer, not implementer)
- Review loop workflow
- Approval/rejection criteria
- Merge workflow
- Rejected task handling
- Available CLI commands

### Init Instructions (`src/cmd/embed/init-instructions.md`)

Used by `jackin init` for interactive config setup.

### Skill Instructions (`src/cmd/embed/skill.md`)

Installed to `.claude/skills/jackin/SKILL.md` for reference.

## Component Interaction

```
                                    ┌──────────────────┐
                                    │   jack-in.yaml   │
                                    │   (config)       │
                                    └────────┬─────────┘
                                             │ reads
                                             ▼
┌────────────────────────────────────────────────────────────────────────────┐
│                              DAEMON (mechanical)                            │
│                                                                             │
│   ┌─────────────┐      ┌─────────────┐      ┌─────────────┐                │
│   │  Tick Loop  │─────▶│ Task Queue  │─────▶│  Worktree   │                │
│   │  (5s poll)  │      │  Manager    │      │  Manager    │                │
│   └──────┬──────┘      └─────────────┘      └─────────────┘                │
│          │                                                                  │
│          │ monitors          ┌─────────────┐                               │
│          └──────────────────▶│  Watchdog   │──────┐                        │
│                              │  (LLM eval) │      │ auto-approve           │
│                              └─────────────┘      │ (if safe)              │
└──────────────────────────────────────────────────┼─────────────────────────┘
                                                   │
       ┌───────────────────────────────────────────┼───────────────────┐
       │                                           │                   │
       ▼                                           ▼                   ▼
┌─────────────┐                           ┌─────────────┐      ┌─────────────┐
│  Worker 1   │                           │  Worker 2   │      │ Orchestrator│
│  (tmux win) │                           │  (tmux win) │      │ (tmux pane) │
└──────┬──────┘                           └──────┬──────┘      └──────┬──────┘
       │                                         │                    │
       │ works in                                │ works in           │ reviews
       ▼                                         ▼                    ▼
┌─────────────┐                           ┌─────────────┐      ┌─────────────┐
│ .w-worker-1/│                           │ .w-worker-2/│      │ project root│
│ (worktree)  │                           │ (worktree)  │      │ (git repo)  │
└─────────────┘                           └─────────────┘      └─────────────┘
```

## Task Queue (Filesystem State Machine)

```
.jack-in/tasks/
├── pending/          <-- New tasks (jackin tasks add)
├── current/          <-- Assigned to worker
├── review/           <-- Worker completed, awaiting review
├── complete/         <-- Approved and merged
└── rejected/         <-- Rejected, awaiting retry/drop
```

### Task Lifecycle

```
  pending ──▶ current ──▶ review ──▶ complete
                           │
                           └──▶ rejected ──┬──▶ pending (retry)
                                           └──▶ [deleted] (drop)
```

## Signal Files

```
.jack-in/signals/<worker>/
├── done              # Worker finished task
├── needs-input       # Worker waiting for input
├── heartbeat         # Last activity timestamp
└── exited            # Worker process terminated
```

## Daemon Tick Loop

```
┌─────────────────────────────────────────────────────────────────┐
│                        DAEMON TICK (every 5s)                    │
│                                                                  │
│  1. READ CONFIG (hot-reload)                                     │
│  2. CHECK WORKERS                                                │
│     ├─ Idle + pending → ASSIGN                                   │
│     ├─ Working + done → MOVE to review                           │
│     ├─ Working + stuck → WATCHDOG                                │
│     └─ Task left review → REFRESH worktree                       │
│  3. WATCHDOG (if approval=auto)                                  │
│     ├─ Capture pane, LLM evaluates                               │
│     └─ Auto-approve safe ops, escalate unsafe                    │
│  4. EMIT STATE (only if changed)                                 │
│  5. LOG to .jack-in/daemon.log                                   │
└──────────────────────────────────────────────────────────────────┘
```

## Worktree Isolation

```
project/                      # Main repo
├── .w-jack-in-worker-1/      # Worker 1 worktree
└── .w-jack-in-worker-2/      # Worker 2 worktree

Branch structure:
  main (or jackin-develop)         <-- Target branch
       ├── jackin/<project>/worker-1
       └── jackin/<project>/worker-2
```

## Daemon vs Orchestrator

| Daemon (Mechanical) | Orchestrator (LLM) |
|--------------------|--------------------|
| Go code, deterministic | Claude Code agent |
| Assigns tasks | Reviews work |
| Detects signals | Approves/rejects |
| Refreshes worktrees | Creates follow-ups |
| Auto-approves safe ops | Makes judgment calls |
| Runs every 5 seconds | Runs continuously |

**The daemon is the scheduler. The orchestrator is the reviewer.**

## Approval Modes

| Mode | Behavior |
|------|----------|
| `manual` | All prompts require user approval |
| `auto` | LLM evaluates; auto-approves safe ops |
| `yolo` | Auto-approve everything |

## CLI Commands

| Command | Description |
|---------|-------------|
| `jackin init` | Interactive config setup |
| `jackin up` | Start the swarm |
| `jackin down` | Stop the swarm |
| `jackin status` | Show swarm status |
| `jackin tasks` | List all tasks |
| `jackin tasks add` | Add a new task |
| `jackin tasks retry <id>` | Retry rejected task |
| `jackin tasks drop <id>` | Delete rejected task |
| `jackin refresh <worker>` | Reset worktree |
| `jackin reset <worker>` | Kill and respawn worker |
| `jackin send <worker> <msg>` | Send message to worker |
| `jackin attach <worker>` | Jump to worker window |

## Project Structure

```
cmd/jackin/main.go           # Entry point
src/
├── cmd/                     # CLI commands (Cobra)
│   ├── embed/               # Embedded instruction files
│   │   ├── worker-instructions.md
│   │   ├── orchestrator-instructions.md
│   │   ├── init-instructions.md
│   │   └── skill.md
│   ├── up.go, down.go, init.go, tasks.go, ...
├── config/                  # jack-in.yaml parsing
├── daemon/                  # Tick loop, watchdog, LLM eval
├── domain/                  # Shared types
├── taskqueue/               # Filesystem task queue
├── tmux/                    # tmux primitives
├── ui/                      # Bubbletea TUI
└── worktree/                # Git worktree management
```
