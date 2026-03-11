/** JACKOPS CLI entry point. */

import { loadConfig, sessionName } from "./config.ts";
import * as tmux from "./tmux.ts";
import * as worktree from "./worktree.ts";
import { shellEscape, spawnCommand } from "./agents.ts";
import { formatStatus, getSessionStarted, getStatus } from "./status.ts";
import * as tq from "./task-queue.ts";

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
  jackops up                        Spawn workers in tmux + worktrees
  jackops down                      Kill session and clean up worktrees
  jackops status                    Show worker status
  jackops send <worker> <message>   Send a message to a worker
  jackops attach <worker>           Switch to a worker's tmux window
  jackops tasks                     List all tasks
  jackops tasks add <summary>       Add a task to the queue
  jackops tasks init                Initialize task queue directories
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

async function up() {
  const configPath = await findConfig();
  const config = await loadConfig(configPath);
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

  // Create tmux session (comes with window 0)
  await tmux.createSession(session);
  await tmux.renameWindow(session, 0, "dashboard");

  const reusable = new Map(
    stale.map((e) => [e.path.split("/").pop() ?? "", e.path]),
  );

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

    // Create tmux window and spawn agent
    await tmux.createWindow(session, w.name);
    const target = `${session}:${w.name}`;
    const cmd = spawnCommand(w.agent, w.prompt);
    await tmux.sendKeys(target, `cd ${shellEscape(wt)} && ${cmd}`);

    console.log(`  ${w.name} (${w.agent}) -> ${wt}`);
  }

  // Seed task queue from config
  if (config.tasks && config.tasks.length > 0) {
    const seeded = await tq.seed(base, config.tasks);
    if (seeded > 0) {
      console.log(`\nSeeded ${seeded} tasks from config.`);
    }
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

async function status() {
  const configPath = await findConfig();
  const config = await loadConfig(configPath);
  const [statuses, started] = await Promise.all([
    getStatus(config),
    getSessionStarted(config),
  ]);
  console.log(formatStatus(config, statuses, started));
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
          c.complete ?? 0
        } complete, ${c.rejected ?? 0} rejected\n`,
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
      case "up":
        await up();
        break;
      case "down":
        await down();
        break;
      case "status":
        await status();
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
      default:
        console.log(USAGE);
        Deno.exit(command ? 1 : 0);
    }
  } catch (e) {
    console.error(`Error: ${e instanceof Error ? e.message : e}`);
    Deno.exit(1);
  }
}

main();
