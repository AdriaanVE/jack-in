# jackops init — Implementation Plan

## Overview

`jackops init` is an LLM-driven interactive setup command. It spawns an AI agent
in the user's terminal that reads the project, asks questions, and generates a
`jackops.yaml` config file.

## UX Flow

````
$ cd my-project
$ jackops init

Detecting installed agents... found: claude, codex
Reading project context...

Spawning claude as your setup assistant...

> Hi! I see this is a Python web app using FastAPI with auth, database,
> and API modules. Let me help you set up jackops.
>
> Project name: "my-project" — sound good?
> ...
> (3-5 questions)
> ...
> Here's your config:
>
> ```yaml
> project: my-project
> workers: ...
> ```
>
> Written to ./jackops.yaml
> Run `jackops up` to start the swarm.
````

## Architecture

### No tmux for init

Init runs inline in the current terminal. Reasons:

- First contact with jackops — tmux session before a config exists is confusing
- One-shot conversation — no need for persistent windows
- Natural handoff: init writes config, `jackops up` creates the tmux world

### Agent spawn strategy

The init agent runs interactively (not in --print or --full-auto mode) because
it needs to have a conversation with the user. The prompt is passed as the
initial message.

| Agent    | Spawn command                           |
| -------- | --------------------------------------- |
| claude   | `claude -p <prompt>`                    |
| codex    | `codex` (prompt via stdin/first msg)    |
| opencode | `opencode` (prompt via stdin/first msg) |
| gemini   | `gemini` (prompt via stdin/first msg)   |

For Claude specifically, we can use `--print` mode with `--allowedTools`
restricted to file writing, or interactive mode where the agent writes the file
directly. Interactive is preferred since it allows back-and-forth.

### Context assembly

At runtime, the init command builds a prompt by combining:

1. **Static:** `docs/init-instructions.md` (schema reference, rules, examples)
2. **Dynamic:**
   - `AVAILABLE AGENTS: claude, codex` (detected at runtime)
   - `SUGGESTED PROJECT NAME: my-project` (basename of cwd)
   - `PROJECT README:` (contents of ./README.md, truncated to 500 lines)
   - `FILE LISTING:` (shallow tree of the repo)

These are concatenated into a single prompt string passed to the agent.

## Implementation

### New files

| File                        | Purpose                                                        |
| --------------------------- | -------------------------------------------------------------- |
| `docs/init-instructions.md` | LLM instruction document (config schema, rules, examples)      |
| `src/init.ts`               | Init module (detect agents, read repo, assemble prompt, spawn) |

### Changes to existing files

| File            | Change                                                                       |
| --------------- | ---------------------------------------------------------------------------- |
| `src/cli.ts`    | Add `init` subcommand                                                        |
| `src/agents.ts` | Add `detectAgents()` function, add `initSpawnCommand()` for interactive mode |

### `src/init.ts` — Module design

```typescript
// Detect which agent CLIs are available on PATH
async function detectAgents(): Promise<AgentType[]>;

// Read repo context (README + file listing)
async function readRepoContext(): Promise<{ readme: string; files: string[] }>;

// Assemble the full prompt from template + runtime context
function assemblePrompt(opts: {
  instructions: string; // from init-instructions.md
  agents: AgentType[]; // detected
  projectName: string; // suggested
  readme: string; // target repo README
  files: string[]; // target repo file listing
}): string;

// Spawn the agent interactively in the current terminal
async function spawnInitAgent(agent: AgentType, prompt: string): Promise<void>;

// Validate the generated config exists and is valid
async function validateConfig(path: string): Promise<boolean>;

// Main init flow
export async function init(opts: InitOpts): Promise<void>;
```

### `src/cli.ts` — New subcommand

```
jackops init [--agent <type>]
```

Flags:

- `--agent <type>` — Force a specific agent (default: first available,
  preference order: claude > codex > opencode > gemini)

### `src/agents.ts` — New functions

```typescript
// Check if an agent CLI is on PATH
async function isAgentInstalled(agent: AgentType): Promise<boolean>;

// Detect all installed agents
async function detectAgents(): Promise<AgentType[]>;

// Spawn command for init mode (interactive, no full-auto)
function initSpawnCommand(agent: AgentType, prompt: string): string[];
```

## Edge cases

### Existing jackops.yaml

- Check before spawning the agent
- Show current config summary
- Ask: "[O]verwrite, [E]dit (pass to agent), [A]bort?"
- If edit: include current config in the agent's context so it can modify it

### No agents installed

- Print error with install links for each agent
- Offer to generate a template config manually (no LLM):
  `jackops init --template` writes a commented example jackops.yaml

### Agent writes invalid YAML

- After agent exits, validate with `loadConfig()`
- If invalid, print the error and suggest manual edits
- Could retry by re-spawning the agent with the error, but keep it simple for v1

### Non-interactive terminal

- Detect with `Deno.stdin.isTerminal()`
- If not a TTY: error with "jackops init requires an interactive terminal"

### Large README

- Truncate to 500 lines with a note: "(truncated, showing first 500 lines)"

## Testing

### Unit tests (`test/init_test.ts`)

- `assemblePrompt()` — verify template interpolation
- `detectAgents()` — mock `which` to test detection logic (integration test)
- Context truncation — verify README gets capped at 500 lines
- Project name inference — verify basename extraction and SAFE_NAME validation

### Integration tests (`test/init_integration_test.ts`)

- Agent detection with real PATH (needs --allow-run)
- Full init flow would need a mock agent — defer to manual testing for v1

## Milestones

1. `detectAgents()` + `readRepoContext()` — pure utility functions
2. `assemblePrompt()` — template interpolation
3. `spawnInitAgent()` — spawn agent in current terminal
4. Wire up CLI subcommand
5. Handle edge cases (existing config, no agents, validation)
6. Tests
