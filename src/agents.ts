/** Agent CLI spawn commands. */

import type { StartupInstructions } from "./config.ts";

export type AgentType = "claude" | "codex" | "opencode" | "gemini";

const AGENTS: Record<AgentType, (prompt: string) => string> = {
  claude: (prompt) => `claude ${shellEscape(prompt)}`,
  codex: (prompt) => `codex --full-auto ${shellEscape(prompt)}`,
  opencode: (prompt) => `opencode run ${shellEscape(prompt)}`,
  gemini: (prompt) => `gemini ${shellEscape(prompt)}`,
};

export const AGENT_NAMES = Object.keys(AGENTS) as AgentType[];

export function isAgentType(value: string): value is AgentType {
  return Object.hasOwn(AGENTS, value);
}

export function spawnCommand(
  agent: AgentType,
  prompt: string,
  startup?: StartupInstructions | null,
): string {
  let fullPrompt = prompt;
  if (startup) {
    const instructions = agent === "codex" ? startup.codex : startup.default;
    fullPrompt = `${instructions}\n\n${prompt}`;
  }
  return AGENTS[agent](fullPrompt);
}

export function shellEscape(s: string): string {
  return `'${s.replace(/'/g, "'\\''")}'`;
}
