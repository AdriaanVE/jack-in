/** Shared CLI utilities: config loading, flag parsing, prompts, guards. */

import {
  APPROVAL_MODES,
  type ApprovalMode,
  type Config,
  loadConfig,
} from "../config.ts";
import * as tmux from "../tmux.ts";

export { type Config } from "../config.ts";

export const USAGE = `JACKOPS -- tmux-native multi-agent swarm orchestrator

Usage:
  jackops init [options]             Interactive setup — generate jackops.yaml
  jackops up [options]               Spawn workers + start orchestrator daemon
  jackops down                      Kill session and clean up worktrees
  jackops status [--json]           Show worker status
  jackops approval [<mode>]         Show or switch approval mode (manual|auto|yolo)
  jackops send <worker> <message>   Send a message to a worker
  jackops attach <worker>           Switch to a worker's tmux window
  jackops daemon [options]          Start the orchestrator daemon
  jackops tasks                     List all tasks
  jackops tasks add <summary>       Add a task to the queue
  jackops tasks complete <id>       Force a current task to review
  jackops tasks approve <id>        Approve a reviewed task
  jackops tasks reject <id> <msg>   Reject a reviewed task with feedback
  jackops tasks init                Initialize task queue directories

Options:
  --no-orchestrator                Skip starting the daemon (manual approval only)
  --no-orchestrator-agent          Skip spawning the LLM orchestrator agent
  --approval <mode>                Override approval mode from config
  --agent <type>                   Agent to use for init (claude|codex|opencode|gemini)
  --template                       Generate a template config without an LLM

Approval modes (set via --approval or orchestrator.approval in jackops.yaml):
  manual   Workers pause on permission prompts (default)
  auto     LLM evaluates and approves safe operations
  yolo     All permission prompts auto-approved
`;

export async function prompt(message: string): Promise<string> {
  const buf = new Uint8Array(4);
  await Deno.stdout.write(new TextEncoder().encode(message));
  const n = await Deno.stdin.read(buf);
  return new TextDecoder().decode(buf.subarray(0, n ?? 0)).trim().toLowerCase();
}

export async function attachSession(session: string): Promise<void> {
  if (Deno.env.get("TMUX")) {
    await tmux.selectWindow(session, "dashboard-orchestrator");
  } else {
    const cmd = new Deno.Command("tmux", {
      args: ["attach", "-t", session],
      stdin: "inherit",
      stdout: "inherit",
      stderr: "inherit",
    });
    const { code } = await cmd.spawn().status;
    Deno.exit(code);
  }
}

export async function findConfig(): Promise<string> {
  for (const c of ["jackops.yaml", "jackops.yml"]) {
    try {
      await Deno.stat(c);
      return c;
    } catch (e) {
      if (e instanceof Deno.errors.NotFound) continue;
      throw e;
    }
  }
  throw new Error("No jackops.yaml found in current directory");
}

export async function loadCliConfig(): Promise<
  { configPath: string; config: Config }
> {
  const configPath = await findConfig();
  const config = await loadConfig(configPath);
  return { configPath, config };
}

export function parseApproval(args: string[]): ApprovalMode | undefined {
  const idx = args.indexOf("--approval");
  if (idx < 0 || idx + 1 >= args.length) return undefined;
  const value = args[idx + 1];
  if (!APPROVAL_MODES.includes(value as ApprovalMode)) {
    console.error(
      `Invalid approval mode '${value}'. Must be one of: ${
        APPROVAL_MODES.join(", ")
      }`,
    );
    Deno.exit(1);
  }
  return value as ApprovalMode;
}

export function checkUnknownFlags(args: string[], known: Set<string>): void {
  for (const arg of args) {
    if (arg.startsWith("--") && !known.has(arg)) {
      console.error(`Unknown flag '${arg}'. Run 'jackops --help' for usage.`);
      Deno.exit(1);
    }
  }
}

export function requireWorker(
  config: Config,
  workerName: string,
): Config["workers"][number] {
  const worker = config.workers.find((w) => w.name === workerName);
  if (!worker) {
    console.error(
      `Unknown worker '${workerName}'. Available: ${
        config.workers.map((w) => w.name).join(", ")
      }`,
    );
    Deno.exit(1);
  }
  return worker;
}

export async function requireActiveSession(session: string): Promise<void> {
  if (!(await tmux.hasSession(session))) {
    console.error(`No active session '${session}'. Run 'jackops up' first.`);
    Deno.exit(1);
  }
}
