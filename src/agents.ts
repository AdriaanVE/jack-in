/** Agent CLI spawn commands. */

import type { StartupInstructions } from "./config.ts";
import { exec } from "./subprocess.ts";

export type AgentType = "claude" | "codex" | "opencode" | "gemini";

const AGENTS: Record<AgentType, (prompt: string) => string> = {
  claude: (prompt) => `claude ${shellEscape(prompt)}`,
  codex: (prompt) => `codex --full-auto ${shellEscape(prompt)}`,
  opencode: (prompt) => `opencode run ${shellEscape(prompt)}`,
  gemini: (prompt) => `gemini ${shellEscape(prompt)}`,
};

/** Preference order for agent selection when none is specified. */
export const AGENT_PREFERENCE: AgentType[] = [
  "claude",
  "codex",
  "opencode",
  "gemini",
];

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

export interface DetectedAgent {
  agent: AgentType;
  path: string;
}

/** Check if an agent CLI is available on PATH, return its binary path. */
export async function findAgent(
  agent: AgentType,
): Promise<DetectedAgent | null> {
  const { success, stdout } = await exec("which", [agent]);
  if (!success || !stdout) return null;
  return { agent, path: stdout.trim() };
}

/** Detect all installed agent CLIs, returned in preference order. */
export async function detectAgents(): Promise<DetectedAgent[]> {
  const results = await Promise.all(AGENT_PREFERENCE.map(findAgent));
  return results.filter((r): r is DetectedAgent => r !== null);
}

/**
 * Build a shell command string for spawning an agent in interactive init mode.
 * The prompt is read from a file (too large for send-keys inline).
 * Returns a shell command suitable for tmux send-keys.
 */
export function initCommand(agent: AgentType, promptFile: string): string {
  const file = shellEscape(promptFile);
  switch (agent) {
    case "claude":
      // Interactive mode: resume with prompt from file via cat substitution
      return `claude "$(cat ${file})"`;
    case "codex":
      return `codex "$(cat ${file})"`;
    case "opencode":
      return `opencode "$(cat ${file})"`;
    case "gemini":
      return `gemini "$(cat ${file})"`;
  }
}

export function shellEscape(s: string): string {
  return `'${s.replace(/'/g, "'\\''")}'`;
}
