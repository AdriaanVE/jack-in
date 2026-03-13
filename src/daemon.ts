/** Orchestrator daemon: polls workers, assigns tasks, tracks completion. */

import { join } from "@std/path";
import { shellEscape } from "./agents.ts";
import { type ApprovalMode, type Config, sessionName } from "./config.ts";
import * as tmux from "./tmux.ts";
import * as tq from "./task-queue.ts";
import { evaluatePane } from "./llm.ts";
import { getJackopsLogger } from "./log.ts";

const log = getJackopsLogger("daemon");

const PROMPT_DIR = ".jackops/prompts";
const SIGNAL_DIR = ".jackops/signals";
const CURRENT_TASK_DIR = ".jackops/current-task";
const STALL_TIMEOUT_MS = 60_000;
// Grace period after task assignment before accepting completion signals.
// The Claude Stop hook fires on every response turn, so early signals are
// likely from the previous turn, not actual task completion.
const MIN_WORK_MS = 10_000;

interface WorkerState {
  name: string;
  agent: string;
  currentTask: string | null;
  assignedAt: number | null;
  lastStallCheck: number | null;
}

// --- Signal file helpers ---

export function signalPath(base: string, workerName: string): string {
  return join(base, SIGNAL_DIR, `${workerName}.done`);
}

export async function hasSignal(
  base: string,
  workerName: string,
): Promise<boolean> {
  try {
    await Deno.stat(signalPath(base, workerName));
    return true;
  } catch (e) {
    if (e instanceof Deno.errors.NotFound) return false;
    throw e;
  }
}

export async function clearSignal(
  base: string,
  workerName: string,
): Promise<void> {
  try {
    await Deno.remove(signalPath(base, workerName));
  } catch (e) {
    if (e instanceof Deno.errors.NotFound) return;
    throw e;
  }
}

/** Initialize signal directory and install hook scripts. */
export async function initSignals(base: string): Promise<void> {
  const dir = join(base, SIGNAL_DIR);
  await Deno.mkdir(dir, { recursive: true });

  const jackopsDir = join(base, ".jackops");

  // Ensure current-task directory exists
  await Deno.mkdir(join(base, CURRENT_TASK_DIR), { recursive: true });

  // Copy hook scripts from repo hooks/ directory
  const repoRoot = new URL(".", import.meta.url).pathname.replace(
    /\/src\/$/,
    "",
  );
  for (
    const name of [
      "stop-hook.ts",
      "stop-hook.sh",
      "permission-eval.sh",
      "yolo-approve.sh",
    ]
  ) {
    const src = join(repoRoot, "hooks", name);
    const dst = join(jackopsDir, name);
    await Deno.copyFile(src, dst);
    await Deno.chmod(dst, 0o755);
  }
}

/** Write Claude Code settings with Stop + optional PermissionRequest hooks. */
export async function writeClaudeSettings(
  worktreePath: string,
  base: string,
  workerName: string,
  approval: ApprovalMode = "manual",
): Promise<void> {
  const settingsDir = join(worktreePath, ".claude");
  await Deno.mkdir(settingsDir, { recursive: true });
  const jackopsDir = join(base, ".jackops");
  const stopHook = join(jackopsDir, "stop-hook.ts");
  const signalDir = join(base, SIGNAL_DIR);
  const currentTaskDir = join(base, CURRENT_TASK_DIR);

  // deno-lint-ignore no-explicit-any
  const hooks: Record<string, any[]> = {
    Stop: [
      {
        matcher: "*",
        hooks: [
          {
            type: "command",
            command: `${shellEscape(stopHook)} ${shellEscape(signalDir)} ${
              shellEscape(workerName)
            } ${shellEscape(currentTaskDir)}`,
          },
        ],
      },
    ],
  };

  if (approval === "auto") {
    const permEval = join(jackopsDir, "permission-eval.sh");
    hooks.PermissionRequest = [
      {
        matcher: "*",
        hooks: [
          {
            type: "command",
            command: shellEscape(permEval),
            timeout: 20,
          },
        ],
      },
    ];
  } else if (approval === "yolo") {
    const yoloHook = join(jackopsDir, "yolo-approve.sh");
    hooks.PermissionRequest = [
      {
        matcher: "*",
        hooks: [
          {
            type: "command",
            command: shellEscape(yoloHook),
          },
        ],
      },
    ];
  }
  // manual: no PermissionRequest hook — normal Claude permission dialog

  await Deno.writeTextFile(
    join(settingsDir, "settings.local.json"),
    JSON.stringify({ hooks }, null, 2) + "\n",
  );
}

// --- Completion marker ---

export const COMPLETION_MARKER_PREFIX = "JACKOPS_TASK_COMPLETE:";

export function completionMarker(taskId: string): string {
  return `${COMPLETION_MARKER_PREFIX}${taskId}`;
}

// --- Current-task file helpers ---

export function currentTaskPath(base: string, workerName: string): string {
  return join(base, CURRENT_TASK_DIR, workerName);
}

export async function writeCurrentTask(
  base: string,
  workerName: string,
  taskId: string,
): Promise<void> {
  const dir = join(base, CURRENT_TASK_DIR);
  await Deno.mkdir(dir, { recursive: true });
  await Deno.writeTextFile(currentTaskPath(base, workerName), taskId);
}

export async function clearCurrentTask(
  base: string,
  workerName: string,
): Promise<void> {
  try {
    await Deno.remove(currentTaskPath(base, workerName));
  } catch (e) {
    if (e instanceof Deno.errors.NotFound) return;
    throw e;
  }
}

// --- Task prompt ---

export function formatTaskPrompt(
  task: tq.Task,
  workerName: string,
  agent: string,
  base: string,
): string {
  const lines: string[] = [];
  lines.push(`# Task: ${task.summary}`);
  lines.push("");
  lines.push(task.description);
  if (task.files && task.files.length > 0) {
    lines.push("");
    lines.push(`## Files likely involved`);
    for (const f of task.files) lines.push(`- ${f}`);
  }
  if (task.acceptance && task.acceptance.length > 0) {
    lines.push("");
    lines.push(`## Acceptance criteria`);
    for (const a of task.acceptance) lines.push(`- ${a}`);
  }
  if (task.feedback) {
    lines.push("");
    lines.push(`## Feedback from previous review`);
    lines.push(task.feedback);
  }
  lines.push("");
  lines.push(
    `Do not ask for confirmation before proceeding. Implement the changes directly. Only stop to ask if the task is ambiguous or you would need to make a destructive/irreversible change.`,
  );
  if (agent === "claude") {
    // Claude: completion detected via transcript marker in Stop hook
    const marker = completionMarker(task.id);
    lines.push("");
    lines.push(
      `IMPORTANT: When you have fully completed this task, output exactly this on its own line as the last line of your final message:`,
    );
    lines.push(marker);
  } else {
    // Non-Claude agents need to signal completion themselves
    const sig = signalPath(base, workerName);
    lines.push("");
    lines.push(
      `IMPORTANT: When you are completely done with this task, run: touch ${sig}`,
    );
  }
  lines.push("");
  return lines.join("\n");
}

export async function writeTaskPrompt(
  base: string,
  task: tq.Task,
  workerName: string,
  agent: string,
): Promise<string> {
  const dir = join(base, PROMPT_DIR);
  await Deno.mkdir(dir, { recursive: true });
  const path = join(dir, `${task.id}.md`);
  await Deno.writeTextFile(
    path,
    formatTaskPrompt(task, workerName, agent, base),
  );
  return path;
}

// --- Task prompt delivery ---

export const MAX_SENDKEYS_BYTES = 3500;

/** Build the message to send to a worker. Inline if small, file path if large. */
export async function taskMessage(
  base: string,
  task: tq.Task,
  workerName: string,
  agent: string,
): Promise<string> {
  const prompt = formatTaskPrompt(task, workerName, agent, base);
  if (new TextEncoder().encode(prompt).length <= MAX_SENDKEYS_BYTES) {
    return prompt;
  }
  const promptPath = await writeTaskPrompt(base, task, workerName, agent);
  return `Read and complete the task described in ${promptPath}`;
}

// --- Watch for new tasks ---

/** Block until a new file appears in pending/ or the signal is aborted. */
async function waitForNewTask(
  base: string,
  signal: AbortSignal,
): Promise<void> {
  const pendingDir = join(base, ".jackops/tasks/pending");
  const watcher = Deno.watchFs(pendingDir);
  const onAbort = () => watcher.close();
  signal.addEventListener("abort", onAbort, { once: true });
  try {
    for await (const event of watcher) {
      if (event.kind === "create" || event.kind === "modify") break;
    }
  } catch {
    // Watcher closed by abort signal
  } finally {
    signal.removeEventListener("abort", onAbort);
  }
}

// --- Daemon loop ---

export interface DaemonOptions {
  config: Config;
  base: string;
  signal: AbortSignal;
}

export async function run(opts: DaemonOptions): Promise<void> {
  const { config, base, signal } = opts;
  const session = sessionName(config.project);
  const interval = config.orchestrator.poll_interval;
  const approval = config.orchestrator.approval;

  const workers = new Map<string, WorkerState>();
  for (const w of config.workers) {
    if (w.role === "executor") {
      workers.set(w.name, {
        name: w.name,
        agent: w.agent,
        currentTask: null,
        assignedAt: null,
        lastStallCheck: null,
      });
    }
  }

  if (workers.size === 0) {
    log.error("No executor workers configured. Nothing to orchestrate.");
    return;
  }

  // Set up signal files and hooks
  await initSignals(base);
  for (const w of config.workers) {
    if (w.role === "executor" && w.agent === "claude") {
      const wt = join(
        base,
        `.w-${config.project}-${w.name}`,
      );
      await writeClaudeSettings(wt, base, w.name, approval);
    }
  }

  // Reconcile in-flight tasks from a previous daemon run.
  // Any task in current/ assigned to one of our workers gets re-adopted
  // and the prompt is re-sent (the worker has a fresh session).
  const currentTasks = await tq.list(base, "current");
  for (const { task } of currentTasks) {
    if (task.assignee && workers.has(task.assignee)) {
      const state = workers.get(task.assignee)!;
      state.currentTask = task.id;
      state.assignedAt = Date.now();
      await writeCurrentTask(base, task.assignee, task.id);
      const msg = await taskMessage(base, task, state.name, state.agent);
      const target = `${session}:${state.name}`;
      try {
        await tmux.sendKeys(target, msg);
        log.info`[recover] Re-sending ${task.id} to ${task.assignee}`;
      } catch (e) {
        log.warn`[recover] Failed to re-send ${task.id} to ${task.assignee}: ${
          e instanceof Error ? e.message : e
        }`;
      }
    }
  }

  // Clear any stale signals
  for (const name of workers.keys()) {
    await clearSignal(base, name);
  }

  // Workers with no task are idle — create an initial signal
  // so the daemon knows they're ready for a task
  for (const [name, state] of workers) {
    if (state.currentTask === null) {
      await Deno.writeTextFile(signalPath(base, name), "");
    }
  }

  log
    .info`Daemon started: ${workers.size} executors, polling every ${interval}ms, approval: ${approval}`;
  log.debug`Workers: ${[...workers.keys()].join(", ")}`;

  while (!signal.aborted) {
    await tick(session, base, workers, approval);

    const c = await tq.counts(base);
    log
      .debug`Tick done. Tasks: ${c.pending} pending, ${c.current} current, ${c.review} review, ${c.complete} complete, ${c.rejected} rejected`;

    if (c.pending === 0 && c.current === 0 && c.review === 0) {
      log
        .info`All tasks complete (${c.complete} done, ${c.rejected} rejected). Watching for new tasks...`;
      await waitForNewTask(base, signal);
      if (signal.aborted) break;
      log.info`New task detected, resuming poll loop.`;
      // Re-create signal files for idle workers so tick() picks them up
      for (const [name, state] of workers) {
        if (state.currentTask === null) {
          await Deno.writeTextFile(signalPath(base, name), "");
        }
      }
      continue;
    }

    await new Promise<void>((resolve) => {
      const timer = setTimeout(resolve, interval);
      signal.addEventListener("abort", () => {
        clearTimeout(timer);
        resolve();
      }, { once: true });
    });
  }

  log.info("Daemon stopped.");
}

async function tick(
  session: string,
  base: string,
  workers: Map<string, WorkerState>,
  approval: ApprovalMode,
): Promise<void> {
  const now = Date.now();
  const idleWorkers: WorkerState[] = [];
  const stallChecks: Promise<void>[] = [];

  for (const [, state] of workers) {
    const signaled = await hasSignal(base, state.name);
    log.debug`${state.name}: task=${
      state.currentTask ?? "none"
    } signaled=${signaled}`;

    if (state.currentTask && signaled) {
      // Ignore early signals — the Stop hook fires on every Claude response
      // turn, so signals arriving right after assignment are from the previous
      // turn, not actual task completion.
      if (state.assignedAt && now - state.assignedAt < MIN_WORK_MS) {
        log.debug`${state.name}: ignoring early signal (${
          now - state.assignedAt
        }ms < ${MIN_WORK_MS}ms)`;
        await clearSignal(base, state.name);
        continue;
      }
      // Worker finished its task
      try {
        await tq.review(base, state.currentTask);
        log
          .info`[review] ${state.name} finished ${state.currentTask}, sent to review`;
      } catch (e) {
        log.warn`Could not move ${state.currentTask} to review: ${
          e instanceof Error ? e.message : e
        }`;
      }
      await clearCurrentTask(base, state.name);
      state.currentTask = null;
      state.assignedAt = null;
      state.lastStallCheck = null;
      idleWorkers.push(state);
    } else if (!state.currentTask && signaled) {
      // Worker is idle and ready
      idleWorkers.push(state);
    } else if (
      state.currentTask && !signaled &&
      state.assignedAt &&
      now - state.assignedAt > STALL_TIMEOUT_MS &&
      (!state.lastStallCheck || now - state.lastStallCheck > STALL_TIMEOUT_MS)
    ) {
      // Worker may be stuck on a permission prompt or asking for confirmation
      log.debug`${state.name}: possible stall detected (${
        now - state.assignedAt
      }ms since assignment)`;
      state.lastStallCheck = now;
      stallChecks.push(checkStalled(session, state, base, approval));
    }
  }

  if (stallChecks.length > 0) await Promise.all(stallChecks);

  // Assign ready tasks to idle workers
  if (idleWorkers.length === 0) {
    printStatus(workers);
    return;
  }
  const readyTasks = await tq.ready(base);
  let taskIdx = 0;
  for (const state of idleWorkers) {
    if (taskIdx >= readyTasks.length) break;
    const task = readyTasks[taskIdx];

    try {
      await tq.claim(base, task.id, state.name);
    } catch {
      taskIdx++;
      continue;
    }

    // Clear signal before sending task and write current-task file
    await clearSignal(base, state.name);
    await writeCurrentTask(base, state.name, task.id);

    const msg = await taskMessage(base, task, state.name, state.agent);
    const target = `${session}:${state.name}`;

    try {
      await tmux.sendKeys(target, msg);
    } catch (e) {
      log.warn`Failed to send task to ${state.name}: ${
        e instanceof Error ? e.message : e
      }`;
      // Unclaim so the task returns to pending for another worker
      try {
        await tq.unclaim(base, task.id);
        await clearCurrentTask(base, state.name);
      } catch {
        log.warn`Could not unclaim ${task.id}`;
      }
      taskIdx++;
      continue;
    }

    state.currentTask = task.id;
    state.assignedAt = now;
    state.lastStallCheck = null;
    log.info`[assign] ${task.id} -> ${state.name}: ${task.summary}`;
    taskIdx++;
  }

  printStatus(workers);
}

async function checkStalled(
  session: string,
  state: WorkerState,
  base: string,
  approval: ApprovalMode,
): Promise<void> {
  const target = `${session}:${state.name}`;
  let paneContent: string;
  try {
    paneContent = await tmux.capturePane(target, 30);
  } catch {
    return; // Pane gone or inaccessible
  }

  if (approval === "yolo") {
    // Approve blindly — send Enter to dismiss any prompt
    log.info`[yolo-approve] ${state.name}: sending Enter`;
    log.debug`${state.name}: pane content (last 30 lines):\n${paneContent}`;
    try {
      await tmux.sendKeys(target, "", true);
    } catch {
      // pane may be gone
    }
    return;
  }

  if (approval === "manual") {
    // Notify only, don't evaluate or approve
    log.info`[stall-detected] ${state.name}: may need attention`;
    log.debug`${state.name}: pane content (last 30 lines):\n${paneContent}`;
    try {
      await tmux.displayMessage(
        session,
        `JACKOPS: ${state.name} may be stuck - check manually`,
      );
    } catch {
      // display-message may fail if no client attached
    }
    return;
  }

  // approval === "auto" — LLM evaluation
  log.info`[stall-check] Evaluating ${state.name} via LLM...`;
  log.debug`${state.name}: pane content for LLM eval:\n${paneContent}`;

  let taskSummary = "unknown task";
  if (state.currentTask) {
    const entry = await tq.get(base, state.currentTask);
    if (entry) taskSummary = entry.task.summary;
  }

  try {
    const result = await evaluatePane(
      paneContent,
      taskSummary,
      state.agent,
    );
    log
      .info`[stall-result] ${state.name}: status=${result.status} safe=${result.safe_to_approve} action=${
      result.approval_keystroke || result.response_text || "none"
    } reason=${result.reason}`;

    if (result.status === "permission_prompt") {
      if (result.safe_to_approve && result.approval_keystroke) {
        log.info`[auto-approve] ${state.name}: ${result.reason}`;
        if (result.approval_keystroke === "Enter") {
          await tmux.sendKeys(target, "", true);
        } else {
          await tmux.sendKeys(target, result.approval_keystroke);
        }
      } else {
        log.info`[needs-attention] ${state.name}: ${result.reason}`;
        try {
          const msg =
            `JACKOPS: ${state.name} needs approval - ${result.reason}`;
          await tmux.displayMessage(session, msg);
        } catch {
          // display-message may fail if no client attached
        }
      }
    } else if (result.status === "waiting_for_input" && result.response_text) {
      log.info`[auto-respond] ${state.name}: sending "${result.response_text}"`;
      try {
        await tmux.sendKeys(target, result.response_text);
      } catch {
        // pane may be gone
      }
    } else if (result.status === "error") {
      log.info`[error-detected] ${state.name}: ${result.reason}`;
      try {
        const msg = `JACKOPS: ${state.name} hit an error - ${result.reason}`;
        await tmux.displayMessage(session, msg);
      } catch {
        // display-message may fail
      }
    } else {
      log.debug`${state.name}: LLM says "${result.status}" — no action needed`;
    }
  } catch (e) {
    log.error`LLM evaluation failed for ${state.name}: ${
      e instanceof Error ? e.message : e
    }`;
  }
}

function printStatus(workers: Map<string, WorkerState>): void {
  const parts: string[] = [];
  for (const [name, state] of workers) {
    const label = state.currentTask ? `working(${state.currentTask})` : "idle";
    parts.push(`${name}:${label}`);
  }
  const now = new Date().toLocaleTimeString([], {
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  });
  log.debug`[${now}] ${parts.join("  ")}`;
}
