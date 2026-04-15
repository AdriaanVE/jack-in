Continue the orchestration loop:

1. Run `jackin status --json` to get current state
2. Check for tasks in `review` - evaluate and approve/reject
3. Check for tasks in `rejected` - inspect worktree, then retry or drop
4. Check for stuck workers (no progress, errors, or needing input)
5. Repeat
