/** Daemon environment file and command-string generation. */

import { join } from "@std/path";
import { type ApprovalMode } from "../config.ts";
import { shellEscape } from "../agents.ts";

// TODO: add env vars for other providers (OpenAI, Azure OpenAI, Google, etc.)
// TODO: add --env-file <path> option to load env from a file before forwarding
// TODO: make handling the env vars safe — delete daemon.env on shutdown,
//       ensure it never appears in logs, tmux scrollback, or LLM eval prompts
const LLM_ENV_KEYS = [
  "ANTHROPIC_FOUNDRY_API_KEY",
  "ANTHROPIC_FOUNDRY_RESOURCE",
  "ANTHROPIC_DEFAULT_SONNET_MODEL",
];

/** Write a temporary .env file with LLM credentials (mode 0600). */
export async function writeDaemonEnv(base: string): Promise<string | null> {
  const lines: string[] = [];
  for (const key of LLM_ENV_KEYS) {
    const value = Deno.env.get(key);
    if (value) lines.push(`${key}=${value}`);
  }
  if (lines.length === 0) return null;
  const dir = join(base, ".jackops");
  await Deno.mkdir(dir, { recursive: true });
  const path = join(dir, "daemon.env");
  await Deno.writeTextFile(path, lines.join("\n") + "\n");
  await Deno.chmod(path, 0o600);
  return path;
}

export function daemonCommand(
  approval?: ApprovalMode,
  envFile?: string | null,
): string {
  // Resolve src/cli.ts from src/cli/daemon-env.ts
  const cliPath = new URL("../cli.ts", import.meta.url).pathname;
  const deno = Deno.execPath();
  const approvalFlag = approval ? ` --approval ${shellEscape(approval)}` : "";
  const envFlag = envFile ? ` --env-file=${shellEscape(envFile)}` : "";
  return `${
    shellEscape(deno)
  } run${envFlag} --allow-run --allow-read --allow-write --allow-env --allow-net ${
    shellEscape(cliPath)
  } daemon${approvalFlag}`;
}
