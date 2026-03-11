/** Parse jackops.yaml. */

import { parse as parseYaml } from "@std/yaml";
import { AGENT_NAMES, type AgentType, isAgentType } from "./agents.ts";

export interface WorkerConfig {
  name: string;
  agent: AgentType;
  prompt: string;
}

export interface Config {
  project: string;
  workers: WorkerConfig[];
}

const SAFE_NAME = /^[a-zA-Z0-9_-]+$/;

function validateName(value: string, label: string): void {
  if (!SAFE_NAME.test(value)) {
    throw new Error(
      `Invalid ${label} '${value}': must contain only alphanumeric chars, hyphens, and underscores`,
    );
  }
}

export function sessionName(project: string): string {
  return `jackops-${project}`;
}

export async function loadConfig(path: string): Promise<Config> {
  const text = await Deno.readTextFile(path);
  const raw = parseYaml(text) as Record<string, unknown>;

  if (!raw || typeof raw !== "object") {
    throw new Error(`Invalid config: expected an object`);
  }

  const project = raw.project;
  if (typeof project !== "string" || !project) {
    throw new Error(`Invalid config: 'project' must be a non-empty string`);
  }
  validateName(project, "project");

  const workers = raw.workers;
  if (!Array.isArray(workers) || workers.length === 0) {
    throw new Error(`Invalid config: 'workers' must be a non-empty array`);
  }

  const parsed: WorkerConfig[] = [];
  const names = new Set<string>();

  for (const w of workers) {
    if (!w || typeof w !== "object") {
      throw new Error(`Invalid worker entry: expected an object`);
    }
    const entry = w as Record<string, unknown>;

    const name = entry.name;
    if (typeof name !== "string" || !name) {
      throw new Error(`Invalid worker: 'name' must be a non-empty string`);
    }
    validateName(name, "worker name");
    if (names.has(name)) {
      throw new Error(`Duplicate worker name: '${name}'`);
    }
    names.add(name);

    const agent = entry.agent;
    if (typeof agent !== "string" || !isAgentType(agent)) {
      throw new Error(
        `Invalid worker '${name}': 'agent' must be one of: ${AGENT_NAMES.join(", ")}`,
      );
    }

    const prompt = entry.prompt;
    if (typeof prompt !== "string" || !prompt) {
      throw new Error(`Invalid worker '${name}': 'prompt' must be a non-empty string`);
    }

    parsed.push({ name, agent, prompt });
  }

  return { project, workers: parsed };
}
