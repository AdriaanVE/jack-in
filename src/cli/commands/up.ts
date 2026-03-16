/** `jackops up` command handler. */

import { dirname, fromFileUrl, join } from "@std/path";
import { type ApprovalMode, sessionName } from "../../config.ts";
import * as tmux from "../../tmux.ts";
import * as worktree from "../../worktree.ts";
import { initCommand, shellEscape, spawnCommand } from "../../agents.ts";
import * as tq from "../../task-queue.ts";
import * as daemon from "../../daemon.ts";
import { INIT_SESSION } from "../../init.ts";
import {
  attachSession,
  checkUnknownFlags,
  loadCliConfig,
  parseApproval,
  prompt,
} from "../context.ts";
import { daemonCommand, writeDaemonEnv } from "../daemon-env.ts";

interface UpOpts {
  orchestrator: boolean;
  orchestratorAgent: boolean;
  approval?: ApprovalMode;
}

export async function up(args: string[]): Promise<void> {
  checkUnknownFlags(
    args,
    new Set(["--no-orchestrator", "--no-orchestrator-agent", "--approval"]),
  );
  const opts: UpOpts = {
    orchestrator: !args.includes("--no-orchestrator"),
    orchestratorAgent: !args.includes("--no-orchestrator-agent"),
    approval: parseApproval(args),
  };

  const { configPath, config } = await loadCliConfig();
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

  await daemon.initSignals(base);

  // Load worker instructions for spawn prompts
  let workerInstructions = "";
  try {
    const repoRoot = join(dirname(fromFileUrl(import.meta.url)), "../../..");
    workerInstructions = await Deno.readTextFile(
      join(repoRoot, "docs", "worker-instructions.md"),
    );
  } catch {
    // Non-fatal — workers will still get instructions via task prompts
  }

  await tmux.createSession(session);
  await tmux.renameWindow(session, 0, "dashboard-orchestrator");

  const reusable = new Map(
    stale.map((e) => [e.path.split("/").pop() ?? "", e.path]),
  );

  const nonClaudeTargets: string[] = [];

  for (const w of config.workers) {
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

    if (w.agent === "claude") {
      await daemon.writeClaudeSettings(
        wt,
        base,
        w.name,
        config.orchestrator.approval,
      );
    }

    await tmux.createWindow(session, w.name);
    const target = `${session}:${w.name}`;
    const spawnPrompt = workerInstructions
      ? [
        workerInstructions,
        "",
        "---",
        "",
        `Your role: ${w.prompt}`,
        "",
        "Read the project README and familiarize yourself with the codebase.",
        "Do not start making changes yet. The daemon will assign you specific tasks.",
        "Wait for task assignments.",
      ].join("\n")
      : w.prompt;
    const cmd = spawnCommand(w.agent, spawnPrompt, config.startup_instructions);
    await tmux.sendKeys(target, `cd ${shellEscape(wt)} && ${cmd}`);

    if (w.agent !== "claude") {
      nonClaudeTargets.push(target);
    }

    console.log(`  ${w.name} (${w.agent}) -> worktree: ${wt}`);
  }

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
    const target = `${session}:dashboard-orchestrator`;
    await tmux.sendKeys(target, daemonCommand(opts.approval, envFile));
    console.log(
      `\nOrchestrator daemon started in dashboard-orchestrator pane.`,
    );
  }

  const orchAgentType = config.orchestrator.agent;
  const hasTasks = config.tasks && config.tasks.length > 0;
  if (opts.orchestratorAgent && orchAgentType && hasTasks) {
    if (orchAgentType === "claude") {
      await daemon.mergeClaudeSettings(
        base,
        base,
        "orchestrator",
        config.orchestrator.approval,
        daemon.ORCHESTRATOR_PERMISSIONS,
      );
    }

    // Resolve docs/orchestrator-instructions.md from src/cli/commands/up.ts
    const instructionsPath = new URL(
      "../../../docs/orchestrator-instructions.md",
      import.meta.url,
    ).pathname;
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
    await tmux.splitWindow(session, "dashboard-orchestrator", 70);
    const orchTarget = `${session}:dashboard-orchestrator.1`;
    const cmd = initCommand(orchAgentType, promptPath);
    await tmux.sendKeys(orchTarget, `cd ${shellEscape(base)} && ${cmd}`);
    console.log(
      `Orchestrator agent (${orchAgentType}) started in dashboard-orchestrator pane.`,
    );
  }

  await tmux.selectWindow(session, "dashboard-orchestrator");

  if (await tmux.hasSession(INIT_SESSION)) {
    if (Deno.env.get("TMUX")) {
      try {
        await tmux.switchClient(session);
      } catch {
        // May fail if not attached to init session
      }
    }
    await tmux.killSession(INIT_SESSION);
    console.log(`Killed init session '${INIT_SESSION}'.`);
  }

  console.log(`\nSwarm running in tmux session '${session}'. Attaching...`);
  await attachSession(session);
}
