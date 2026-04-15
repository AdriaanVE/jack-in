# JACK-IN Improvements

Architectural analysis identifying flaws, upgrade opportunities, and potential reworks.

## Current Flaws

### 1. Orchestrator is a Single Point of Failure

- One LLM agent reviews everything - if it gets confused, loops, or crashes, the system stalls
- Orchestrator context grows unbounded over time (no pruning)
- No health check or automatic recovery

### 2. Polling Architecture (5s latency)

- Everything waits for the next tick
- Signal files are polling with extra steps
- Could be event-driven with channels/hooks

### 3. No Task Dependencies

- Flat queue, FIFO only
- Can't express "B depends on A"
- Workers might implement conflicting changes simultaneously

### 4. Merge Conflicts Discovered Late

- Two workers can modify the same files
- Conflict only detected at merge time
- Wasted work if both can't merge

### 5. Context Lost on Rejection

- Worker gets feedback string, but loses session context
- Same mistakes may repeat
- No structured learning from failures

### 6. Review Bottleneck

- All diffs go through one orchestrator
- With many workers, review queue backs up
- Orchestrator can't parallelize

### 7. No Queue Correctness Model

- No lease semantics for task ownership
- Crash/restart can cause duplicate assignment or stuck tasks
- No idempotent transitions or replay behavior
- Missing startup reconciliation (what state were we in?)

### 8. Trust Boundary Contradiction

- Daemon claims to be "mechanical" but runs LLM watchdog evaluation
- Auto-approve decision is an LLM judgment call, not mechanical
- No explicit threat model for what watchdog can/cannot approve

### 9. No Audit Trail

- Only current state folders exist
- No append-only event log for debugging or recovery
- Hard to answer "what happened to task X?"

---

## Upgrade Opportunities

### 1. Event-Driven Instead of Polling

```
Current:  daemon polls every 5s
Better:   worker signals → daemon reacts immediately

Use channels + Claude Code hooks for near-instant response.
```

### 2. Split Orchestrator into Reviewer + Strategist

```
Current:  One agent does everything

Better:
  ┌─────────────┐     ┌─────────────┐
  │  Reviewer   │     │  Strategist │
  │ (stateless) │     │  (stateful) │
  │ approves    │     │ creates     │
  │ diffs       │     │ tasks       │
  └─────────────┘     └─────────────┘
        │                    │
        └────────┬───────────┘
                 ▼
           Parallelizable reviews
           Focused strategy agent
```

Benefits:
- Reviews can run in parallel (stateless)
- Strategist focuses on planning, not code review
- Cleaner separation of concerns

### 3. Task Dependencies (DAG)

```yaml
tasks:
  - id: setup-db
    summary: "Create database schema"
  - id: add-auth
    summary: "Add authentication"
    depends_on: [setup-db]  # Won't start until setup-db complete
```

Benefits:
- Prevents workers from starting tasks with unmet dependencies
- Enables smarter scheduling
- Natural representation of project structure

### 4. Pre-flight Conflict Detection

Before assigning a task:
1. Check which files the task likely touches (from description or `files` hint)
2. Check if any in-progress tasks touch same files
3. Block or warn if overlap detected

Benefits:
- Catches conflicts before work starts
- No wasted effort on unmergeable changes
- Workers can be redirected to non-conflicting tasks

### 5. Worker Specialization + Routing

```yaml
workers:
  - name: backend
    skills: [go, sql, api]
  - name: frontend
    skills: [react, css, typescript]

# Task auto-routes to worker with matching skills
```

Benefits:
- Better task-worker fit
- Workers build expertise in their area
- More efficient resource allocation

### 6. Structured Rejection History

```json
{
  "id": "abc123",
  "summary": "Add user auth",
  "attempts": [
    {
      "worker": "worker-1",
      "feedback": "Missing tests",
      "diff_hash": "a1b2c3",
      "timestamp": "2024-01-15T10:00:00Z"
    },
    {
      "worker": "worker-2",
      "feedback": "Tests pass but no error handling",
      "diff_hash": "d4e5f6",
      "timestamp": "2024-01-15T11:00:00Z"
    }
  ]
}
```

Benefits:
- Worker sees full history, not just last feedback
- Patterns emerge (same mistake repeated = bad task description)
- Can route retries to different workers

### 7. Priority Queue

```
pending/
├── 0-critical/   # Blockers
├── 1-high/       # Important
├── 2-normal/     # Default
└── 3-low/        # Nice to have
```

Or add `priority` field to task JSON:
```json
{
  "id": "abc123",
  "summary": "Fix production bug",
  "priority": 0
}
```

Benefits:
- Critical tasks get worked on first
- Low-priority tasks don't block important work
- User can reorder queue

---

## Major Rework Ideas

### 1. Orchestrator as State Machine (not free-form agent)

**Current:** Orchestrator is a Claude Code session that "figures it out"

**Problem:** Can get confused, go off-track, forget what it was doing

**Better:**
```
┌─────────┐    ┌─────────┐    ┌─────────┐    ┌─────────┐
│  IDLE   │───▶│ REVIEW  │───▶│ DECIDE  │───▶│ ACTION  │
└─────────┘    └─────────┘    └─────────┘    └─────────┘
                   │              │              │
                   │              ▼              │
                   │         approve/reject      │
                   │         create task         │
                   │              │              │
                   └──────────────┴──────────────┘
```

Each state is a focused prompt, not open-ended conversation.
Daemon drives the state machine, orchestrator just evaluates.

**Benefits:**
- More predictable behavior
- Easier to debug (which state failed?)
- Less context accumulation
- Can checkpoint and resume

### 2. Pull-Based Task Assignment

**Current:** Daemon assigns tasks to workers (push)

**Alternative:** Workers claim tasks (pull)
```
Worker: "I'm idle, here are my skills: [go, tests]"
Queue:  "Here are matching pending tasks: [...]"
Worker: "I'll take task-123"
```

**Benefits:**
- More autonomous workers
- Workers self-select based on fit
- Reduces daemon complexity
- Natural load balancing

### 3. Hierarchical Tasks (Epics)

```
Epic: "Add user authentication"
├── Task: "Setup database schema"
├── Task: "Implement JWT tokens"
├── Task: "Add login endpoint"
└── Task: "Write integration tests"
```

**Benefits:**
- Progress tracked at epic level
- Subtasks auto-generated or manually added
- Better visibility into large features
- Natural grouping for reporting

### 4. Multiple Review Tracks

```
┌─────────────────────────────────────────────────┐
│                   REVIEW ROUTER                  │
└─────────────────────────────────────────────────┘
         │              │              │
         ▼              ▼              ▼
   ┌──────────┐  ┌──────────┐  ┌──────────┐
   │   Code   │  │   Test   │  │   Docs   │
   │ Reviewer │  │ Reviewer │  │ Reviewer │
   └──────────┘  └──────────┘  └──────────┘
```

Route tasks to specialized reviewers based on type.

**Benefits:**
- Parallel reviews
- Specialized expertise
- Faster throughput
- Better feedback quality

---

## Prioritization

### Foundation (Do First)

1. **Queue correctness** - leases, idempotency, crash recovery, startup reconciliation
2. **LLM decision contract** - strict schema for daemon↔orchestrator, invalid output handling
3. **Audit/observability** - task event log, stuck/latency metrics

### High Impact, Moderate Effort

4. **Task dependencies (DAG)** - prevents wasted work, enables planning
5. **Pre-flight conflict detection** - catches problems before workers start (advisory, not blocking)
6. **Split reviewer from strategist** - parallelizes reviews, focused agents

### High Impact, High Effort

7. **Orchestrator as state machine** - more reliable, predictable behavior
8. **Event-driven architecture** - hybrid mode (event-first + polling fallback)

### Lower Priority (Nice to Have)

- Priority queue
- Worker specialization
- SQLite storage
- Hierarchical tasks

---

## Implementation Notes

### Task Dependencies

Add to task schema:
```go
type Task struct {
    ID          string   `json:"id"`
    Summary     string   `json:"summary"`
    Description string   `json:"description"`
    DependsOn   []string `json:"depends_on,omitempty"` // Task IDs
    // ...
}
```

Scheduler logic:
```go
func canAssign(task Task, completedTasks map[string]bool) bool {
    for _, dep := range task.DependsOn {
        if !completedTasks[dep] {
            return false
        }
    }
    return true
}
```

**Gotchas:**
- Cycle detection required (A→B→A deadlocks the queue)
- Orphan deps (task depends on deleted/unknown ID) need graceful handling
- Blocked tasks need visible "waiting on X" status in TUI
- Starvation risk: low-priority tasks with deps never run if deps keep failing

### Pre-flight Conflict Detection

Add `files` hint to tasks:
```json
{
  "id": "abc123",
  "summary": "Add auth middleware",
  "files": ["internal/auth/*.go", "cmd/server/main.go"]
}
```

Before assignment:
```go
func hasConflict(task Task, inProgress []Task) bool {
    for _, active := range inProgress {
        if filesOverlap(task.Files, active.Files) {
            return true
        }
    }
    return false
}
```

**Gotchas:**
- File hints are predictions; actual changes may differ
- Hard blocking on overlap can deadlock throughput
- Better: advisory "conflict score" with warnings, not hard blocks
- Consider soft-locking by path prefix with timeout instead

### Reviewer/Strategist Split

Two orchestrator panes or separate sessions:
- Reviewer: receives diff, outputs approve/reject/feedback
- Strategist: monitors progress, creates tasks, handles stuck workers

Reviewer can be stateless (fresh prompt per review).
Strategist maintains context across session.

**Gotchas:**
- Parallel reviewers race on approve/reject/merge
- Need compare-and-swap on task state (version field or atomic file ops)
- State-machine prompts risk schema drift; keep prompts tiny and strictly validated

### Queue Correctness

Add lease semantics:
```go
type Task struct {
    // ...
    Owner     string `json:"owner,omitempty"`      // Worker holding lease
    LeaseAt   int64  `json:"lease_at,omitempty"`   // Unix ms when leased
    LeaseTTL  int64  `json:"lease_ttl,omitempty"`  // TTL in ms
    Version   int    `json:"version"`              // For compare-and-swap
}
```

Startup reconciliation:
1. Scan `current/` for tasks with expired leases → move back to `pending/`
2. Scan `review/` for stale tasks → alert or auto-retry
3. Validate all task files parse correctly

Idempotent transitions:
- Check version before moving files
- Log every state change to append-only event log

### Filesystem Queue Gotchas

- Atomic rename (`os.Rename`) fails across filesystem boundaries
- Ensure `.jack-in/tasks/` subdirs are on same device
- Consider `link` + `unlink` pattern for atomic cross-dir moves
- Write temp file in target dir, then rename (same-dir atomic)

---

## Alternative Approaches

### Stateless Review Workers

Keep one strategist, but run reviews as short-lived isolated jobs:
```
Task in review → spawn reviewer → get verdict → terminate
```
- No context accumulation in reviewers
- Easier to scale and parallelize
- Strategist remains stateful for planning

### Lease-Based Scheduler (vs Full Pull Model)

Instead of workers claiming tasks (complex), use leases:
- Daemon assigns task with lease TTL
- Worker heartbeats to extend lease
- Expired lease = task returns to pending

Preserves central control while enabling specialization.

### Soft-Locking by Path Prefix

Simpler than semantic conflict detection:
```
Worker claims: internal/auth/*
Other tasks touching internal/auth/* wait or warn
Lock expires after task completes or times out
```

### Event-Sourced Task Core

Keep filesystem queue for UX (`ls .jack-in/tasks/pending`), but derive state from append-only events:
```
events/
├── 001-task-created.json
├── 002-task-assigned.json
├── 003-task-completed.json
└── 004-task-approved.json
```

Benefits:
- Easy recovery (replay events)
- Full audit trail
- Debug "what happened" by reading event log

### Hybrid Event + Polling Model

Don't replace polling entirely:
- Use Claude hooks for fast path (immediate response)
- Keep 5s polling as reconciliation fallback
- Handles agents without hooks, missed events, daemon restarts
