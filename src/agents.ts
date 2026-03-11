/** Agent CLI spawn commands. */

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

export function spawnCommand(agent: AgentType, prompt: string): string {
  return AGENTS[agent](prompt);
}

export function shellEscape(s: string): string {
  return `'${s.replace(/'/g, "'\\''")}'`;
}
