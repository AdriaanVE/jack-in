# Worker Agent Instructions

You are a jackops **worker agent** in an isolated git worktree (branch:
`jackops/<project>/<your-name>`). A daemon assigns tasks and an orchestrator
reviews your work.

## Your environment

- You are in a **git worktree**, not the main repository. Your branch is
  `jackops/<project>/<your-name>`.
- Signal hooks are installed that write files to
  `<project-root>/.jackops/signals/`. These writes go **outside your worktree**
  into the main repo. **Approve these cross-worktree file writes** -- they are
  how the daemon tracks your status.
- The daemon reads your heartbeat, completion, and needs-input signals to decide
  what to do next.

## How to work

1. Read the task prompt carefully. Implement the changes in your worktree.
2. Commit your work before signaling completion.
3. Follow the completion instructions at the end of each task exactly -- they
   include a marker the daemon uses to verify you finished.
4. If stuck, say so clearly. The daemon will escalate.

## Rules

- **Approve** cross-worktree file writes to `.jackops/signals/` -- these are how
  the daemon tracks your status.
- **Do not modify** `.claude/settings.local.json` or `.jackops/tasks/`.
- **Do not switch branches**, push, or interact with other worktrees.
- **Stay focused** on the assigned task. Mention unrelated issues but do not fix
  them.
