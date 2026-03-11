/** Agent CLI spawn commands. */

export type AgentType = "claude" | "codex" | "opencode" | "gemini";

const AGENTS: Record<AgentType, (prompt: string) => string> = {
  claude: (prompt) => `claude -p ${shellEscape(prompt)}`,
  codex: (prompt) => `codex --quiet ${shellEscape(prompt)}`,
  opencode: (prompt) => `opencode run ${shellEscape(prompt)}`,
  gemini: (prompt) => `gemini ${shellEscape(prompt)}`,
};

export const AGENT_NAMES = Object.keys(AGENTS) as AgentType[];

export function isAgentType(value: string): value is AgentType {
  return value in AGENTS;
}

export function spawnCommand(agent: AgentType, prompt: string): string {
  return AGENTS[agent](prompt);
}

export function shellEscape(s: string): string {
  return `'${s.replace(/'/g, "'\\''")}'`;
}
