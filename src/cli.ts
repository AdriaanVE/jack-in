/** JACKOPS CLI entry point. */

import { loadConfig, sessionName } from "./config.ts";
import * as tmux from "./tmux.ts";
import * as worktree from "./worktree.ts";
import { shellEscape, spawnCommand } from "./agents.ts";
import { formatStatus, getStatus } from "./status.ts";

const USAGE = `JACKOPS -- tmux-native multi-agent swarm orchestrator

Usage:
  jackops up                        Spawn workers in tmux + worktrees
  jackops down                      Kill session and clean up worktrees
  jackops status                    Show worker status
  jackops send <worker> <message>   Send a message to a worker
  jackops attach <worker>           Switch to a worker's tmux window
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

  if (await tmux.hasSession(session)) {
    console.error(
      `Session '${session}' already exists. Run 'jackops down' first.`,
    );
    Deno.exit(1);
  }

  console.log(
    `Starting swarm for '${config.project}' with ${config.workers.length} workers...`,
  );

  // Create tmux session (comes with window 0)
  await tmux.createSession(session);
  await tmux.renameWindow(session, 0, "dashboard");

  for (const w of config.workers) {
    // Create worktree (sequential -- git worktree mutates shared repo metadata)
    let wt: string;
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

    // Create tmux window and spawn agent
    await tmux.createWindow(session, w.name);
    const target = `${session}:${w.name}`;
    const cmd = spawnCommand(w.agent, w.prompt);
    await tmux.sendKeys(target, `cd ${shellEscape(wt)} && ${cmd}`);

    console.log(`  ${w.name} (${w.agent}) -> ${wt}`);
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

  const buf = new Uint8Array(4);
  await Deno.stdout.write(
    new TextEncoder().encode("\nRemove worktrees? [y/N] "),
  );
  const n = await Deno.stdin.read(buf);
  const answer = new TextDecoder().decode(buf.subarray(0, n ?? 0)).trim();

  if (answer.toLowerCase() === "y") {
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
  const statuses = await getStatus(config);
  console.log(formatStatus(config, statuses));
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
