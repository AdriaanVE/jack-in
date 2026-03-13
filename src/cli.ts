/** JACKOPS CLI entry point. */

import { join } from "@std/path";
import {
  APPROVAL_MODES,
  type ApprovalMode,
  loadConfig,
  sessionName,
} from "./config.ts";
import * as tmux from "./tmux.ts";
import * as worktree from "./worktree.ts";
import {
  initCommand,
  isAgentType,
  shellEscape,
  spawnCommand,
} from "./agents.ts";
import {
  formatJsonStatus,
  formatStatus,
  getSessionStarted,
  getStatus,
} from "./status.ts";
import * as tq from "./task-queue.ts";
import * as daemon from "./daemon.ts";
import { setupLogging } from "./log.ts";
import { init, INIT_SESSION, initTemplate } from "./init.ts";

async function prompt(message: string): Promise<string> {
  const buf = new Uint8Array(4);
  await Deno.stdout.write(new TextEncoder().encode(message));
  const n = await Deno.stdin.read(buf);
  return new TextDecoder().decode(buf.subarray(0, n ?? 0)).trim().toLowerCase();
}

async function attachSession(session: string): Promise<void> {
  if (Deno.env.get("TMUX")) {
    await tmux.selectWindow(session, "dashboard");
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

const USAGE = `JACKOPS -- tmux-native multi-agent swarm orchestrator

Usage:
  jackops init [options]             Interactive setup — generate jackops.yaml
  jackops up [options]               Spawn workers + start orchestrator daemon
  jackops down                      Kill session and clean up worktrees
  jackops status [--json]           Show worker status
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

async function findConfig(): Promise<string> {
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

function parseApproval(args: string[]): ApprovalMode | undefined {
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

function checkUnknownFlags(args: string[], known: Set<string>): void {
  for (const arg of args) {
    if (arg.startsWith("--") && !known.has(arg)) {
      console.error(`Unknown flag '${arg}'. Run 'jackops --help' for usage.`);
      Deno.exit(1);
    }
  }
}

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
async function writeDaemonEnv(base: string): Promise<string | null> {
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

function daemonCommand(
  approval?: ApprovalMode,
  envFile?: string | null,
): string {
  const cliPath = new URL(".", import.meta.url).pathname + "cli.ts";
  const deno = Deno.execPath();
  const approvalFlag = approval ? ` --approval ${shellEscape(approval)}` : "";
  const envFlag = envFile ? ` --env-file=${shellEscape(envFile)}` : "";
  return `${
    shellEscape(deno)
  } run${envFlag} --allow-run --allow-read --allow-write --allow-env --allow-net ${
    shellEscape(cliPath)
  } daemon${approvalFlag}`;
}

interface UpOpts {
  orchestrator: boolean;
  orchestratorAgent: boolean;
  approval?: ApprovalMode;
}

async function up(
  opts: UpOpts = { orchestrator: true, orchestratorAgent: true },
) {
  const configPath = await findConfig();
  const config = await loadConfig(configPath);
  if (opts.approval) config.orchestrator.approval = opts.approval;

  if (config.orchestrator.approval === "auto") {
    const missing = ["ANTHROPIC_FOUNDRY_API_KEY", "ANTHROPIC_FOUNDRY_RESOURCE"]
      .filter((k) => !Deno.env.get(k));
    if (missing.length > 0) {
      console.error(
        `Warning: approval mode 'auto' requires LLM access but ${
          missing.join(" and ")
        } not set.`,
      );
      console.error(
        "Stall detection will fail. Source your env file first or switch to --approval manual.",
      );
    }
  }
  const session = sessionName(config.project);
  const base = Deno.cwd();

  const hasSession = await tmux.hasSession(session);
  const existing = await worktree.list(base);
  const stale = existing.filter((e) =>
    worktree.isJackopsWorktree(e, config.project)
  );

  if (hasSession) {
    // Session is running -- offer to attach or restart
    console.log(`Session '${session}' is already running.`);
    const answer = await prompt(
      "\n[A]ttach, [R]estart (kill + reset worktrees), [Q]uit? [a/r/Q] ",
    );

    if (answer === "a") {
      await attachSession(session);
      return;
    } else if (answer === "r") {
      await tmux.killSession(session);
      console.log(`Killed session '${session}'.`);
      for (const e of stale) {
        await worktree.reset(e.path);
      }
      if (stale.length > 0) console.log("Worktrees reset.");
    } else {
      console.log("Aborted.");
      Deno.exit(1);
    }
  } else if (stale.length > 0) {
    // No session but leftover worktrees
    console.log("Existing worktrees found from a previous run:");
    for (const e of stale) {
      const n = await worktree.dirtyCount(e.path);
      const label = n > 0
        ? `dirty - ${n} uncommitted change${n > 1 ? "s" : ""}`
        : "clean";
      console.log(`  ${e.path} (${label})`);
    }
    const answer = await prompt(
      "\n[R]eset worktrees and continue, [A]bort to inspect? [r/A] ",
    );
    if (answer !== "r") {
      console.log("Aborted.");
      Deno.exit(1);
    }
    for (const e of stale) {
      await worktree.reset(e.path);
    }
    console.log("Worktrees reset.\n");
  }

  console.log(
    `Starting swarm for '${config.project}' with ${config.workers.length} workers...`,
  );
  console.log(`Config:   ${join(base, configPath)}`);
  console.log(`Tasks:    ${join(base, ".jackops", "tasks")}`);
  console.log(`Approval: ${config.orchestrator.approval}`);

  // Set up signal files and hooks before spawning agents
  await daemon.initSignals(base);

  // Create tmux session (comes with window 0)
  await tmux.createSession(session);
  await tmux.renameWindow(session, 0, "dashboard");

  const reusable = new Map(
    stale.map((e) => [e.path.split("/").pop() ?? "", e.path]),
  );

  const nonClaudeTargets: string[] = [];

  for (const w of config.workers) {
    // Reuse reset worktree or create a new one
    const dirName = worktree.worktreeDir(config.project, w.name);
    let wt: string;
    if (reusable.has(dirName)) {
      wt = reusable.get(dirName)!;
    } else {
      try {
        wt = await worktree.create(base, config.project, w.name);
      } catch (e) {
        console.error(
          `  Failed to create worktree for '${w.name}': ${
            e instanceof Error ? e.message : e
          }`,
        );
        continue;
      }
    }

    // Write Claude hooks before spawning the agent
    if (w.agent === "claude") {
      await daemon.writeClaudeSettings(
        wt,
        base,
        w.name,
        config.orchestrator.approval,
      );
    }

    // Create tmux window and spawn agent
    await tmux.createWindow(session, w.name);
    const target = `${session}:${w.name}`;
    const cmd = spawnCommand(w.agent, w.prompt, config.startup_instructions);
    await tmux.sendKeys(target, `cd ${shellEscape(wt)} && ${cmd}`);

    if (w.agent !== "claude") {
      nonClaudeTargets.push(target);
    }

    console.log(`  ${w.name} (${w.agent}) -> worktree: ${wt}`);
  }

  // Dismiss startup prompts (e.g. Codex trust prompt) for non-Claude agents
  if (nonClaudeTargets.length > 0) {
    console.log(
      "Waiting 3s to dismiss startup prompts for non-Claude agents...",
    );
    await new Promise((resolve) => setTimeout(resolve, 3000));
    await Promise.all(
      nonClaudeTargets.map((target) => tmux.sendKeys(target, "", true)),
    );
    console.log("Startup prompts dismissed.");
  }

  // Seed task queue from config
  if (config.tasks && config.tasks.length > 0) {
    const seeded = await tq.seed(base, config.tasks);
    if (seeded > 0) {
      console.log(`\nSeeded ${seeded} tasks from config.`);
    }
  }

  if (opts.orchestrator) {
    const envFile = await writeDaemonEnv(base);
    if (envFile) {
      console.log(`Daemon env written to ${envFile}`);
    }
    const target = `${session}:dashboard`;
    await tmux.sendKeys(target, daemonCommand(opts.approval, envFile));
    console.log(`\nOrchestrator daemon started in dashboard pane.`);
  }

  // Spawn orchestrator LLM agent if enabled and tasks exist
  const orchAgentType = config.orchestrator.agent;
  const hasTasks = config.tasks && config.tasks.length > 0;
  if (opts.orchestratorAgent && orchAgentType && hasTasks) {
    const instructionsPath = new URL(".", import.meta.url).pathname.replace(
      /\/src\/$/,
      "",
    ) + "/docs/orchestrator-instructions.md";
    const promptPath = join(base, ".jackops", "orchestrator-prompt.md");
    const instructions = await Deno.readTextFile(instructionsPath);
    const orchPrompt = [
      instructions,
      "",
      `## Current status`,
      "",
      `Run \`jackops status --json\` to get the current swarm state. The project root is: ${base}`,
      "",
      `Worker worktrees are at: ${
        config.workers.map((w) =>
          join(base, worktree.worktreeDir(config.project, w.name))
        ).join(", ")
      }`,
    ].join("\n");
    await Deno.writeTextFile(promptPath, orchPrompt);
    await tmux.createWindow(session, "orchestrator");
    const orchTarget = `${session}:orchestrator`;
    const cmd = initCommand(orchAgentType, promptPath);
    await tmux.sendKeys(orchTarget, `cd ${shellEscape(base)} && ${cmd}`);
    console.log(
      `Orchestrator agent (${orchAgentType}) started in 'orchestrator' window.`,
    );
  }

  // Select orchestrator window if it was spawned, otherwise dashboard
  const defaultWindow = (opts.orchestratorAgent && orchAgentType && hasTasks)
    ? "orchestrator"
    : "dashboard";
  await tmux.selectWindow(session, defaultWindow);

  // Clean up init session if it exists
  if (await tmux.hasSession(INIT_SESSION)) {
    if (Deno.env.get("TMUX")) {
      // Switch client to the swarm session before killing init
      try {
        await tmux.switchClient(session);
      } catch {
        // May fail if not attached to init session
      }
    }
    await tmux.killSession(INIT_SESSION);
    console.log(`Killed init session '${INIT_SESSION}'.`);
  }

  console.log(`\nSwarm running in tmux session '${session}'.`);
  console.log(`Attach with: tmux attach -t ${session}`);
}

async function down() {
  const base = Deno.cwd();
  let project: string | undefined;

  // Try loading config for project name, but don't require it
  try {
    const configPath = await findConfig();
    const config = await loadConfig(configPath);
    project = config.project;
  } catch {
    // Config missing -- fall back to cleaning up all jackops worktrees
  }

  if (project) {
    const session = sessionName(project);
    if (await tmux.hasSession(session)) {
      await tmux.killSession(session);
      console.log(`Killed tmux session '${session}'.`);
    } else {
      console.log(`No active session '${session}'.`);
    }
  } else {
    console.log("No config found. Skipping tmux session cleanup.");
  }

  // Also kill init session if it exists
  if (await tmux.hasSession(INIT_SESSION)) {
    await tmux.killSession(INIT_SESSION);
    console.log(`Killed init session '${INIT_SESSION}'.`);
  }

  const entries = await worktree.list(base);
  const jackopsWorktrees = entries.filter((e) =>
    worktree.isJackopsWorktree(e, project)
  );

  if (jackopsWorktrees.length === 0) {
    console.log("No worktrees to clean up.");
    return;
  }

  console.log(`\nWorktrees to remove:`);
  for (const e of jackopsWorktrees) {
    console.log(`  ${e.path}`);
  }

  const answer = await prompt("\nRemove worktrees? [y/N] ");

  if (answer === "y") {
    const removed = await worktree.cleanup(
      base,
      project ?? "",
      jackopsWorktrees,
    );
    console.log(`Removed ${removed.length} worktrees.`);
  } else {
    console.log("Worktrees kept.");
  }
}

async function status(json = false) {
  const configPath = await findConfig();
  const config = await loadConfig(configPath);
  const base = Deno.cwd();
  const [{ workers: statuses, daemon }, startedEpoch, tasks] = await Promise
    .all([
      getStatus(config, base),
      getSessionStarted(config),
      tq.counts(base),
    ]);
  const info = {
    statuses,
    startedEpoch,
    daemon,
    tasks,
    autoApprovalModel: config.orchestrator.approval === "auto"
      ? (Deno.env.get("ANTHROPIC_DEFAULT_SONNET_MODEL") ??
        "claude-sonnet-4-5")
      : undefined,
  };
  if (json) {
    console.log(JSON.stringify(formatJsonStatus(config, info), null, 2));
  } else {
    console.log(formatStatus(config, info));
  }
}

async function send(workerName: string, message: string) {
  const configPath = await findConfig();
  const config = await loadConfig(configPath);
  const session = sessionName(config.project);

  const worker = config.workers.find((w) => w.name === workerName);
  if (!worker) {
    console.error(
      `Unknown worker '${workerName}'. Available: ${
        config.workers.map((w) => w.name).join(", ")
      }`,
    );
    Deno.exit(1);
  }

  if (!(await tmux.hasSession(session))) {
    console.error(`No active session '${session}'. Run 'jackops up' first.`);
    Deno.exit(1);
  }

  await tmux.sendKeys(`${session}:${workerName}`, message);
  console.log(`Sent to ${workerName}.`);
}

async function attach(workerName: string) {
  const configPath = await findConfig();
  const config = await loadConfig(configPath);
  const session = sessionName(config.project);

  const worker = config.workers.find((w) => w.name === workerName);
  if (!worker) {
    console.error(
      `Unknown worker '${workerName}'. Available: ${
        config.workers.map((w) => w.name).join(", ")
      }`,
    );
    Deno.exit(1);
  }

  if (!(await tmux.hasSession(session))) {
    console.error(`No active session '${session}'. Run 'jackops up' first.`);
    Deno.exit(1);
  }

  // If already inside tmux, switch. Otherwise attach.
  if (Deno.env.get("TMUX")) {
    await tmux.selectWindow(session, workerName);
  } else {
    const cmd = new Deno.Command("tmux", {
      args: ["attach", "-t", session, ";", "select-window", "-t", workerName],
      stdin: "inherit",
      stdout: "inherit",
      stderr: "inherit",
    });
    const { code } = await cmd.spawn().status;
    Deno.exit(code);
  }
}

async function tasks(subcommand: string | undefined, args: string[]) {
  const base = Deno.cwd();

  switch (subcommand) {
    case "init": {
      await tq.init(base);
      console.log("Task queue initialized.");
      break;
    }
    case "add": {
      if (args.length === 0) {
        console.error(
          "Usage: jackops tasks add <summary> [--desc <description>]",
        );
        Deno.exit(1);
      }
      await tq.init(base); // ensure dirs exist
      const descIdx = args.indexOf("--desc");
      let summary: string;
      let description: string;
      if (descIdx >= 0) {
        summary = args.slice(0, descIdx).join(" ");
        description = args.slice(descIdx + 1).join(" ");
      } else {
        summary = args.join(" ");
        description = summary;
      }
      const id = tq.generateId();
      const task = await tq.create(base, { id, summary, description });
      console.log(`Created ${task.id}: ${task.summary}`);
      break;
    }
    case "complete": {
      const [taskId] = args;
      if (!taskId) {
        console.error("Usage: jackops tasks complete <id>");
        Deno.exit(1);
      }
      await tq.review(base, taskId);
      console.log(`Moved ${taskId} to review.`);
      break;
    }
    case "approve": {
      const [taskId] = args;
      if (!taskId) {
        console.error("Usage: jackops tasks approve <id>");
        Deno.exit(1);
      }
      await tq.approve(base, taskId);
      console.log(`Approved ${taskId}.`);
      break;
    }
    case "reject": {
      const [taskId, ...feedbackParts] = args;
      if (!taskId || feedbackParts.length === 0) {
        console.error("Usage: jackops tasks reject <id> <feedback>");
        Deno.exit(1);
      }
      const feedback = feedbackParts.join(" ");
      await tq.reject(base, taskId, feedback);
      console.log(`Rejected ${taskId}.`);
      break;
    }
    default: {
      // List tasks
      const entries = await tq.list(base);
      if (entries.length === 0) {
        console.log("No tasks. Run 'jackops tasks init' to set up the queue.");
        return;
      }
      const c: Record<string, number> = {};
      for (const e of entries) c[e.state] = (c[e.state] ?? 0) + 1;
      console.log(
        `Tasks: ${c.pending ?? 0} pending, ${c.current ?? 0} current, ${
          c.review ?? 0
        } review, ${c.complete ?? 0} complete, ${c.rejected ?? 0} rejected\n`,
      );
      for (const state of tq.TASK_STATES) {
        const stateEntries = entries.filter((e) => e.state === state);
        if (stateEntries.length === 0) continue;
        console.log(`[${state}]`);
        for (const e of stateEntries) {
          const assignee = e.task.assignee ? ` (${e.task.assignee})` : "";
          console.log(`  ${e.task.id}: ${e.task.summary}${assignee}`);
        }
      }
      break;
    }
  }
}

// --- Main ---

async function main() {
  const [command, ...args] = Deno.args;

  try {
    switch (command) {
      case "init": {
        checkUnknownFlags(args, new Set(["--agent", "--template"]));
        if (args.includes("--template")) {
          await initTemplate();
        } else {
          const agentIdx = args.indexOf("--agent");
          let agent: string | undefined;
          if (agentIdx >= 0 && agentIdx + 1 < args.length) {
            agent = args[agentIdx + 1];
            if (!isAgentType(agent)) {
              console.error(
                `Invalid agent '${agent}'. Must be one of: claude, codex, opencode, gemini`,
              );
              Deno.exit(1);
            }
          }
          await init({
            agent: agent as import("./agents.ts").AgentType | undefined,
          });
        }
        break;
      }
      case "up": {
        checkUnknownFlags(
          args,
          new Set([
            "--no-orchestrator",
            "--no-orchestrator-agent",
            "--approval",
          ]),
        );
        const orchestrator = !args.includes("--no-orchestrator");
        const orchestratorAgent = !args.includes("--no-orchestrator-agent");
        const approval = parseApproval(args);
        await up({ orchestrator, orchestratorAgent, approval });
        break;
      }
      case "down":
        await down();
        break;
      case "status":
        checkUnknownFlags(args, new Set(["--json"]));
        await status(args.includes("--json"));
        break;
      case "send": {
        const [worker, ...rest] = args;
        if (!worker || rest.length === 0) {
          console.error("Usage: jackops send <worker> <message>");
          Deno.exit(1);
        }
        await send(worker, rest.join(" "));
        break;
      }
      case "attach": {
        const [worker] = args;
        if (!worker) {
          console.error("Usage: jackops attach <worker>");
          Deno.exit(1);
        }
        await attach(worker);
        break;
      }
      case "tasks": {
        const [sub, ...rest] = args;
        await tasks(sub, rest);
        break;
      }
      case "daemon": {
        checkUnknownFlags(args, new Set(["--approval"]));
        const configPath = await findConfig();
        const config = await loadConfig(configPath);
        const approval = parseApproval(args);
        if (approval) config.orchestrator.approval = approval;
        const base = Deno.cwd();
        const session = sessionName(config.project);
        if (!(await tmux.hasSession(session))) {
          console.error(
            `No active session '${session}'. Run 'jackops up' first.`,
          );
          Deno.exit(1);
        }
        const closeLog = await setupLogging(base);
        await tq.init(base);
        const ac = new AbortController();
        Deno.addSignalListener("SIGINT", () => ac.abort());
        await daemon.run({ config, base, signal: ac.signal });
        closeLog();
        break;
      }
      case "--help":
      case "-h":
      default:
        console.log(USAGE);
        Deno.exit(!command || command === "--help" || command === "-h" ? 0 : 1);
    }
  } catch (e) {
    console.error(`Error: ${e instanceof Error ? e.message : e}`);
    Deno.exit(1);
  }
}

main();
