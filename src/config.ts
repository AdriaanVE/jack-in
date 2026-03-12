/** Parse jackops.yaml. */

import { parse as parseYaml } from "@std/yaml";
import { AGENT_NAMES, type AgentType, isAgentType } from "./agents.ts";

export type WorkerRole = "executor" | "reviewer" | "planner";
export const WORKER_ROLES: WorkerRole[] = ["executor", "reviewer", "planner"];

export interface WorkerConfig {
  name: string;
  agent: AgentType;
  prompt: string;
  role: WorkerRole;
}

export type ApprovalMode = "manual" | "auto" | "yolo";
export const APPROVAL_MODES: ApprovalMode[] = ["manual", "auto", "yolo"];

export interface StartupInstructions {
  default: string;
  codex: string;
}

export const DEFAULT_STARTUP_INSTRUCTIONS: StartupInstructions = {
  default: "Read README.md if it exists, then follow the instructions below.",
  codex:
    "Read ~/.codex/AGENTS.md and README.md if they exist, then follow the instructions below.",
};

export interface OrchestratorConfig {
  poll_interval: number;
  max_retries: number;
  approval: ApprovalMode;
}

export interface TaskConfig {
  summary: string;
  description?: string;
  files?: string[];
  acceptance?: string[];
  depends_on?: string[];
}

export interface Config {
  project: string;
  workers: WorkerConfig[];
  tasks?: TaskConfig[];
  orchestrator: OrchestratorConfig;
  startup_instructions: StartupInstructions | null;
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
        `Invalid worker '${name}': 'agent' must be one of: ${
          AGENT_NAMES.join(", ")
        }`,
      );
    }

    const prompt = entry.prompt;
    if (typeof prompt !== "string" || !prompt) {
      throw new Error(
        `Invalid worker '${name}': 'prompt' must be a non-empty string`,
      );
    }

    const role = entry.role;
    if (
      role !== undefined &&
      (typeof role !== "string" || !WORKER_ROLES.includes(role as WorkerRole))
    ) {
      throw new Error(
        `Invalid worker '${name}': 'role' must be one of: ${
          WORKER_ROLES.join(", ")
        }`,
      );
    }

    parsed.push({
      name,
      agent,
      prompt,
      role: (role as WorkerRole) ?? "executor",
    });
  }

  const tasks: TaskConfig[] = [];
  if (raw.tasks) {
    if (!Array.isArray(raw.tasks)) {
      throw new Error(`Invalid config: 'tasks' must be an array`);
    }
    for (const t of raw.tasks) {
      if (!t || typeof t !== "object") {
        throw new Error(`Invalid task entry: expected an object`);
      }
      const entry = t as Record<string, unknown>;
      if (typeof entry.summary !== "string" || !entry.summary) {
        throw new Error(`Invalid task: 'summary' must be a non-empty string`);
      }
      tasks.push({
        summary: entry.summary,
        description: typeof entry.description === "string"
          ? entry.description
          : undefined,
        files: Array.isArray(entry.files)
          ? entry.files.filter((f): f is string => typeof f === "string")
          : undefined,
        acceptance: Array.isArray(entry.acceptance)
          ? entry.acceptance.filter((a): a is string => typeof a === "string")
          : undefined,
        depends_on: Array.isArray(entry.depends_on)
          ? entry.depends_on.filter((d): d is string => typeof d === "string")
          : undefined,
      });
    }
  }

  const orchestrator: OrchestratorConfig = {
    poll_interval: 5000,
    max_retries: 2,
    approval: "manual",
  };
  if (raw.orchestrator && typeof raw.orchestrator === "object") {
    const o = raw.orchestrator as Record<string, unknown>;
    if (typeof o.poll_interval === "number") {
      orchestrator.poll_interval = o.poll_interval;
    }
    if (typeof o.max_retries === "number") {
      orchestrator.max_retries = o.max_retries;
    }
    if (typeof o.approval === "string") {
      if (!APPROVAL_MODES.includes(o.approval as ApprovalMode)) {
        throw new Error(
          `Invalid orchestrator 'approval': must be one of: ${
            APPROVAL_MODES.join(", ")
          }`,
        );
      }
      orchestrator.approval = o.approval as ApprovalMode;
    }
  }

  let startup_instructions: StartupInstructions | null =
    DEFAULT_STARTUP_INSTRUCTIONS;
  if (raw.startup_instructions !== undefined) {
    if (
      raw.startup_instructions === null || raw.startup_instructions === false
    ) {
      startup_instructions = null;
    } else if (
      typeof raw.startup_instructions === "object" &&
      raw.startup_instructions !== null
    ) {
      const si = raw.startup_instructions as Record<string, unknown>;
      startup_instructions = {
        default: typeof si.default === "string"
          ? si.default
          : DEFAULT_STARTUP_INSTRUCTIONS.default,
        codex: typeof si.codex === "string"
          ? si.codex
          : DEFAULT_STARTUP_INSTRUCTIONS.codex,
      };
    } else if (typeof raw.startup_instructions === "string") {
      startup_instructions = {
        default: raw.startup_instructions,
        codex: raw.startup_instructions,
      };
    }
  }

  return {
    project,
    workers: parsed,
    tasks: tasks.length > 0 ? tasks : undefined,
    orchestrator,
    startup_instructions,
  };
}
