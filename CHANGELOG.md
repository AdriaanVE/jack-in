# Changelog

All notable changes to this project will be documented in this file.

## [0.1.0] - 2026-04-16

Initial public release.

### Added

- TUI dashboard with worker cards, task bar, and log pane (Bubble Tea)
- Daemon: filesystem-based task queue with automatic assignment and signal detection
- LLM orchestrator agent for reviewing work, approving/rejecting tasks, creating follow-ups
- Worker isolation via git worktrees (one branch per worker)
- Support for multiple agent CLIs: Claude Code, Codex, Opencode, Gemini
- Interactive setup wizard (`jackin init`) with LLM-driven task generation
- Three approval modes: manual, auto (LLM-evaluated), yolo
- Live approval mode switching while swarm is running
- Watchdog: detects stuck workers, evaluates permission prompts in auto mode
- LLM backend priority: Anthropic API, Azure Foundry, headless Claude fallback
- Claude Code hooks integration (Stop, PermissionRequest) with polling fallback
- Remote access via ttyd + ngrok (press R in TUI)
- Token usage tracking and cost estimation per worker
- Task lifecycle: pending, current, review, complete, rejected (with retry/drop)
- Task dependencies (`depends_on` resolved at seed time)
- Worker management: reset, refresh, send messages, capture output
- Homebrew distribution (`brew install AdriaanVE/tap/jackin`)
- Matrix-inspired TUI effects
