# Lessons from claude_code_bridge

Ideas worth adopting in jackops, prioritized by effort/impact.

## P1: Req-ID protocol for pane communication

CCB wraps every message with `CCB_REQ_ID: <id>` and requires `CCB_DONE: <id>` to
close it. Multiple concurrent requests to the same pane never mix up. Jackops
relies on signal files per worker — adding a req-id protocol to pane
communication would be more robust, especially if a worker ever handles
overlapping tasks.

## P2: Continuous pane logging

CCB captures pane output to `~/.cache/ccb/pane-logs/` with TTL-based rotation.
This gives instant replay for debugging without expensive `tmux capture-pane`
calls on every watchdog tick. Jackops currently snapshots panes per tick —
continuous logging would be cheaper and more reliable.

## P3: Plan artifact (plan.md + state.json)

CCB persists the master plan as structured files (`todo.md`, `state.json`,
`plan_log.md`) that survive context window resets. Jackops tasks are individual
files but there's no persistent "master plan" artifact. A plan file would help
the orchestrator maintain coherence across many tasks and resume after context
loss.

## P4: Async turn-boundary enforcement

CCB explicitly forbids Claude from polling after submitting a task — the agent
must end its turn immediately. This prevents wasted tokens and duplicate
requests. The jackops orchestrator could benefit from similar guardrails when
delegating work to workers.

## P5: Dual-review with scoring

CCB routes reviews to a second provider (e.g., Codex reviews Claude's work) with
a scoring framework (pass if overall >= 7.0 and all dimensions >= 3.0). Jackops
uses a single orchestrator LLM. Cross-model review catches blind spots that
same-model review misses.

## P6: Context transfer between agents

CCB has a `ContextTransfer` class with dedup + truncation for handing condensed
context from a completed task to the next related one. Jackops workers start
fresh each time — passing context forward could reduce ramp-up time.

## P7: Provider adapter abstraction

CCB's `BaseProviderAdapter` interface makes adding new AI providers trivial. If
jackops ever wants Codex/Gemini workers alongside Claude, an adapter pattern
would be cleaner than ad-hoc integration.
