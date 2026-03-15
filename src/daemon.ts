/** Orchestrator daemon: polls workers, assigns tasks, tracks completion. */

import { join } from "@std/path";
import { shellEscape } from "./agents.ts";
import {
  type ApprovalMode,
  type Config,
  isApprovalMode,
  sessionName,
} from "./config.ts";
import { evaluatePane } from "./llm.ts";
import { getJackopsLogger } from "./log.ts";
import { completionMarker } from "./marker.ts";
import * as tq from "./task-queue.ts";
import * as tmux from "./tmux.ts";

const log = getJackopsLogger("daemon");

const PROMPT_DIR = ".jackops/prompts";
const SIGNAL_DIR = ".jackops/signals";
const CURRENT_TASK_DIR = ".jackops/current-task";
export const APPROVAL_MODE_FILE = ".jackops/approval-mode";
// Grace period after task assignment before accepting completion signals.
// The Claude Stop hook fires on every response turn, so early signals are
// likely from the previous turn, not actual task completion.
const MIN_WORK_MS = 10_000;
// Tier 2: cheap pane-snapshot check starts after this threshold.
export const TIER2_TIMEOUT_MS = 60_000;
// Tier 3: expensive LLM evaluation starts after this threshold.
export const TIER3_TIMEOUT_MS = 120_000;
// Max consecutive LLM evals per task before giving up and notifying user.
export const MAX_LLM_EVALS = 3;
// Heartbeat younger than this means the worker is confirmed active.
export const HEARTBEAT_FRESH_MS = 30_000;
// Progress deadline: force Tier 3 eval regardless of heartbeat after this.
export const MAX_TASK_WALL_MS = 15 * 60_000;
// Ignore needs-input signals within this window after assignment.
const NEEDS_INPUT_GRACE_MS = 5_000;
// Orchestrator stall timeout: pane unchanged for this long triggers watchdog.
// Longer than worker tiers because the orchestrator legitimately idles between reviews.
export const ORCH_STALL_TIMEOUT_MS = 180_000;

const HOOK_SCRIPTS = [
  "stop-hook.ts",
  "stop-hook.sh",
  "permission-eval.sh",
  "yolo-approve.sh",
  "notification-hook.sh",
  "heartbeat-hook.sh",
  "prompt-hook.sh",
  "session-end-hook.sh",
  "notify-hook.sh",
] as const;

const NOTIFICATION_MATCHERS = [
  "idle_prompt",
  "permission_prompt",
  "elicitation_dialog",
] as const;

interface WorkerState {
  name: string;
  agent: string;
  currentTask: string | null;
  assignedAt: number | null;
  lastPaneSnapshot: string | null;
  lastSnapshotAt: number | null;
  llmEvalCount: number;
  escalatedToUser: boolean;
}

export interface OrchestratorState {
  name: "orchestrator";
  agent: string;
  lastPaneSnapshot: string | null;
  lastSnapshotAt: number | null;
  llmEvalCount: number;
  escalatedToUser: boolean;
  startedAt: number;
}

// --- Generic signal file helpers ---

async function hasSignalFile(path: string): Promise<boolean> {
  try {
    await Deno.stat(path);
    return true;
  } catch (e) {
    if (e instanceof Deno.errors.NotFound) return false;
    throw e;
  }
}

async function clearSignalFile(path: string): Promise<void> {
  try {
    await Deno.remove(path);
  } catch (e) {
    if (e instanceof Deno.errors.NotFound) return;
    throw e;
  }
}

// --- Signal path + typed helpers ---

export function signalPath(base: string, workerName: string): string {
  return join(base, SIGNAL_DIR, `${workerName}.done`);
}

function heartbeatPath(base: string, workerName: string): string {
  return join(base, SIGNAL_DIR, `${workerName}.heartbeat`);
}

export function needsInputPath(base: string, workerName: string): string {
  return join(base, SIGNAL_DIR, `${workerName}.needs-input`);
}

export function exitedPath(base: string, workerName: string): string {
  return join(base, SIGNAL_DIR, `${workerName}.exited`);
}

export function hasSignal(base: string, w: string): Promise<boolean> {
  return hasSignalFile(signalPath(base, w));
}
export function clearSignal(base: string, w: string): Promise<void> {
  return clearSignalFile(signalPath(base, w));
}

export function hasNeedsInput(base: string, w: string): Promise<boolean> {
  return hasSignalFile(needsInputPath(base, w));
}
export function clearNeedsInput(base: string, w: string): Promise<void> {
  return clearSignalFile(needsInputPath(base, w));
}

export function hasExited(base: string, w: string): Promise<boolean> {
  return hasSignalFile(exitedPath(base, w));
}
export function clearExited(base: string, w: string): Promise<void> {
  return clearSignalFile(exitedPath(base, w));
}

export function clearHeartbeat(base: string, w: string): Promise<void> {
  return clearSignalFile(heartbeatPath(base, w));
}

export async function heartbeatAge(
  base: string,
  workerName: string,
): Promise<number | null> {
  try {
    const stat = await Deno.stat(heartbeatPath(base, workerName));
    if (!stat.mtime) return null;
    return Date.now() - stat.mtime.getTime();
  } catch {
    return null;
  }
}

/** Clear ALL signal files for a worker (used at task assignment boundary). */
export function clearAllWorkerSignals(
  base: string,
  workerName: string,
): Promise<void[]> {
  return Promise.all([
    clearSignal(base, workerName),
    clearNeedsInput(base, workerName),
    clearExited(base, workerName),
    clearHeartbeat(base, workerName),
  ]);
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
  for (const name of HOOK_SCRIPTS) {
    const src = join(repoRoot, "hooks", name);
    const dst = join(jackopsDir, name);
    await Deno.copyFile(src, dst);
    await Deno.chmod(dst, 0o755);
  }
}

const BASE_PERMISSIONS = ["Bash(jackops *)"];
export const ORCHESTRATOR_PERMISSIONS = [
  ...BASE_PERMISSIONS,
  "Bash(tmux *)",
  "Bash(git diff *)",
  "Bash(git log *)",
];

/** Build jackops-specific Claude settings (hooks + permissions). */
export function buildClaudeSettings(
  base: string,
  workerName: string,
  approval: ApprovalMode = "manual",
  extraPermissions?: string[],
  // deno-lint-ignore no-explicit-any
): { permissions: { allow: string[] }; hooks: Record<string, any[]> } {
  const jackopsDir = join(base, ".jackops");
  const stopHook = join(jackopsDir, "stop-hook.ts");
  const signalDir = join(base, SIGNAL_DIR);
  const currentTaskDir = join(base, CURRENT_TASK_DIR);

  const esc = shellEscape;

  // deno-lint-ignore no-explicit-any
  const hooks: Record<string, any[]> = {
    Stop: [
      {
        matcher: "*",
        hooks: [
          {
            type: "command",
            command: `${esc(stopHook)} ${esc(signalDir)} ${esc(workerName)} ${
              esc(currentTaskDir)
            }`,
          },
        ],
      },
    ],
  };

  const hookCmd = (script: string) =>
    `${esc(join(jackopsDir, script))} ${esc(signalDir)} ${esc(workerName)}`;

  // deno-lint-ignore no-explicit-any
  const wildcardHook = (script: string): any[] => [{
    matcher: "*",
    hooks: [{ type: "command", command: hookCmd(script) }],
  }];

  // Notification hook (always active) -- instant needs-input detection
  hooks.Notification = NOTIFICATION_MATCHERS
    .map((matcher) => ({
      matcher,
      hooks: [{
        type: "command" as const,
        command: hookCmd("notification-hook.sh"),
      }],
    }));

  hooks.PreToolUse = wildcardHook("heartbeat-hook.sh");
  hooks.PostToolUse = wildcardHook("heartbeat-hook.sh");
  hooks.UserPromptSubmit = wildcardHook("prompt-hook.sh");
  hooks.SessionEnd = wildcardHook("session-end-hook.sh");

  if (approval === "auto") {
    const permEval = join(jackopsDir, "permission-eval.sh");
    hooks.PermissionRequest = [
      {
        matcher: "*",
        hooks: [{ type: "command", command: esc(permEval), timeout: 20 }],
      },
    ];
  } else if (approval === "yolo") {
    const yoloHook = join(jackopsDir, "yolo-approve.sh");
    hooks.PermissionRequest = [
      {
        matcher: "*",
        hooks: [{ type: "command", command: esc(yoloHook) }],
      },
    ];
  }
  // manual: no PermissionRequest hook — normal Claude permission dialog

  const allow = extraPermissions
    ? [...BASE_PERMISSIONS, ...extraPermissions]
    : BASE_PERMISSIONS;

  return {
    permissions: { allow },
    hooks,
  };
}

/** Write Claude Code settings.local.json (overwrites — use for worktrees). */
export async function writeClaudeSettings(
  worktreePath: string,
  base: string,
  workerName: string,
  approval: ApprovalMode = "manual",
  extraPermissions?: string[],
): Promise<void> {
  const settingsDir = join(worktreePath, ".claude");
  await Deno.mkdir(settingsDir, { recursive: true });

  const settings = buildClaudeSettings(
    base,
    workerName,
    approval,
    extraPermissions,
  );
  const settingsPath = join(settingsDir, "settings.local.json");

  await atomicWriteJson(settingsPath, settings);
}

/** Merge jackops settings into an existing settings.local.json (for project root). */
export async function mergeClaudeSettings(
  projectRoot: string,
  base: string,
  workerName: string,
  approval: ApprovalMode = "manual",
  extraPermissions?: string[],
): Promise<void> {
  const settingsDir = join(projectRoot, ".claude");
  await Deno.mkdir(settingsDir, { recursive: true });
  const settingsPath = join(settingsDir, "settings.local.json");

  // Read existing settings if present
  // deno-lint-ignore no-explicit-any
  let existing: Record<string, any> = {};
  try {
    existing = JSON.parse(await Deno.readTextFile(settingsPath));
  } catch {
    // No existing file or invalid JSON — start fresh
  }

  const jackops = buildClaudeSettings(
    base,
    workerName,
    approval,
    extraPermissions,
  );

  // Merge permissions.allow (deduplicate)
  const existingAllow: string[] = existing.permissions?.allow ?? [];
  const mergedAllow = [
    ...new Set([...existingAllow, ...jackops.permissions.allow]),
  ];
  existing.permissions = { ...existing.permissions, allow: mergedAllow };

  const isJackopsHook = (
    // deno-lint-ignore no-explicit-any
    e: any,
  ) =>
    e.hooks?.some((h: { command?: string }) =>
      h.command?.includes(".jackops/")
    );

  // Merge hooks: replace jackops hooks per event, remove stale events
  if (!existing.hooks) existing.hooks = {};
  for (const [event, entries] of Object.entries(jackops.hooks)) {
    if (!existing.hooks[event]) {
      existing.hooks[event] = entries;
    } else {
      existing.hooks[event] = [
        ...existing.hooks[event].filter(
          // deno-lint-ignore no-explicit-any
          (e: any) => !isJackopsHook(e),
        ),
        ...entries,
      ];
    }
  }

  // Remove jackops hooks from events not in the new settings (e.g.
  // PermissionRequest when switching to manual mode)
  for (const event of Object.keys(existing.hooks)) {
    if (event in jackops.hooks) continue;
    existing.hooks[event] = existing.hooks[event].filter(
      // deno-lint-ignore no-explicit-any
      (e: any) => !isJackopsHook(e),
    );
    if (existing.hooks[event].length === 0) delete existing.hooks[event];
  }

  await atomicWriteJson(settingsPath, existing);
}

/** Atomic JSON write: write to temp file, then rename. */
async function atomicWriteJson(
  path: string,
  // deno-lint-ignore no-explicit-any
  data: any,
): Promise<void> {
  const tmp = path + ".tmp";
  await Deno.writeTextFile(tmp, JSON.stringify(data, null, 2) + "\n");
  await Deno.rename(tmp, path);
}

/** Read runtime approval mode from signal file, falling back to config. */
export async function readApprovalMode(
  base: string,
): Promise<ApprovalMode | null> {
  try {
    const mode = (await Deno.readTextFile(join(base, APPROVAL_MODE_FILE)))
      .trim();
    if (isApprovalMode(mode)) return mode;
  } catch {
    // File doesn't exist yet
  }
  return null;
}

/** Write runtime approval mode signal file. */
export async function writeApprovalMode(
  base: string,
  mode: ApprovalMode,
): Promise<void> {
  const dir = join(base, ".jackops");
  await Deno.mkdir(dir, { recursive: true });
  await Deno.writeTextFile(join(base, APPROVAL_MODE_FILE), mode + "\n");
}

// Re-export marker utilities for backward compatibility
export {
  completionMarker,
  MARKER_PREFIX as COMPLETION_MARKER_PREFIX,
} from "./marker.ts";

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

const TASK_REMINDER =
  `Reminder: commit all changes before completing. Do not push. Approve any writes to .jackops/signals/.`;

export function formatTaskPrompt(
  task: tq.Task,
  workerName: string,
  agent: string,
  base: string,
): string {
  const lines: string[] = [];
  lines.push(TASK_REMINDER);
  lines.push("");
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
    // Non-Claude agents: use notify-hook shim + marker for daemon-side scan
    const shim = join(base, ".jackops", "notify-hook.sh");
    const signalDir = join(base, SIGNAL_DIR);
    const marker = completionMarker(task.id);
    lines.push("");
    lines.push(
      `IMPORTANT: When you are completely done with this task, run: ${shim} done ${
        shellEscape(signalDir)
      } ${shellEscape(workerName)}`,
    );
    lines.push(
      `Also output exactly this on its own line: ${marker}`,
    );
    lines.push("");
    lines.push(
      `Tip: If you encounter an error you can't resolve, run: ${shim} error ${
        shellEscape(signalDir)
      } ${shellEscape(workerName)} "description"`,
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
  let approval = config.orchestrator.approval;

  const workers = new Map<string, WorkerState>();
  for (const w of config.workers) {
    if (w.role === "executor") {
      workers.set(w.name, {
        name: w.name,
        agent: w.agent,
        currentTask: null,
        assignedAt: null,
        lastPaneSnapshot: null,
        lastSnapshotAt: null,
        llmEvalCount: 0,
        escalatedToUser: false,
      });
    }
  }

  if (workers.size === 0) {
    log.error("No executor workers configured. Nothing to orchestrate.");
    return;
  }

  const orchState: OrchestratorState | null = config.orchestrator.agent
    ? {
      name: "orchestrator",
      agent: config.orchestrator.agent as string,
      lastPaneSnapshot: null,
      lastSnapshotAt: null,
      llmEvalCount: 0,
      escalatedToUser: false,
      startedAt: Date.now(),
    }
    : null;

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
    await clearAllWorkerSignals(base, name);
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
    // Poll for runtime approval mode changes
    const newMode = await readApprovalMode(base);
    if (newMode && newMode !== approval) {
      log.info`Approval mode changed: ${approval} -> ${newMode}`;
      approval = newMode;
      // Reset watchdog state so escalations are re-evaluated under new mode
      for (const [, state] of workers) {
        if (state.currentTask) resetWatchdog(state);
      }
      if (orchState) {
        orchState.lastPaneSnapshot = null;
        orchState.lastSnapshotAt = null;
        orchState.llmEvalCount = 0;
        orchState.escalatedToUser = false;
      }
    }

    await tick(session, base, workers, approval);

    if (orchState) {
      await checkOrchestrator(session, orchState, approval);
    }

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
  const watchdogChecks: Promise<void>[] = [];

  for (const [, state] of workers) {
    if (!state.currentTask) {
      // Worker is idle and ready — no need to check signal file
      log.debug`${state.name}: task=none idle`;
      idleWorkers.push(state);
      continue;
    }

    // Batch all signal checks in parallel (4 stat calls -> 1 round-trip)
    const [exited, signaled, needsInput, hbAge] = await Promise.all([
      hasExited(base, state.name),
      hasSignal(base, state.name),
      hasNeedsInput(base, state.name),
      heartbeatAge(base, state.name),
    ]);

    // 1. Check .exited signal — worker process is gone
    if (exited) {
      log
        .warn`[exited] ${state.name}: worker exited while task ${state.currentTask} in progress`;
      try {
        await tq.unclaim(base, state.currentTask);
        log
          .info`[exited] ${state.name}: task ${state.currentTask} returned to pending`;
      } catch (e) {
        log.warn`[exited] Could not unclaim ${state.currentTask}: ${
          e instanceof Error ? e.message : e
        }`;
      }
      await clearCurrentTask(base, state.name);
      state.currentTask = null;
      state.assignedAt = null;
      resetWatchdog(state);
      await clearAllWorkerSignals(base, state.name);
      try {
        await tmux.displayMessage(
          session,
          `JACKOPS: ${state.name} exited -- task returned to queue`,
        );
      } catch {
        // display-message may fail if no client attached
      }
      continue;
    }

    // 2. Check .done signal — existing completion logic
    log.debug`${state.name}: task=${state.currentTask} signaled=${signaled}`;

    if (signaled) {
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
      markComplete(state, idleWorkers);
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
      continue;
    }

    // 3. Check .needs-input signal — worker is blocked
    if (needsInput) {
      // Ignore needs-input within grace period after assignment
      if (
        state.assignedAt && now - state.assignedAt < NEEDS_INPUT_GRACE_MS
      ) {
        log.debug`${state.name}: ignoring early needs-input (${
          now - state.assignedAt
        }ms < ${NEEDS_INPUT_GRACE_MS}ms)`;
        await clearNeedsInput(base, state.name);
        continue;
      }
      log.info`[needs-input] ${state.name}: worker blocked on input`;
      const target = `${session}:${state.name}`;
      if (approval === "yolo") {
        log.info`[yolo-approve] ${state.name}: sending Enter (needs-input)`;
        try {
          await tmux.sendKeys(target, "", true);
        } catch {
          // pane may be gone
        }
      } else if (approval === "auto") {
        // Trigger immediate LLM eval (skip Tier 2 wait)
        watchdogChecks.push(
          watchdog(session, state, base, approval, now),
        );
      } else {
        // manual: notify user
        log
          .info`[needs-input] ${state.name}: manual mode, notifying user`;
        try {
          await tmux.displayMessage(
            session,
            `JACKOPS: ${state.name} needs input -- check worker pane`,
          );
        } catch {
          // display-message may fail
        }
      }
      await clearNeedsInput(base, state.name);
      continue;
    }

    // 4. Evaluate heartbeat mtime (already fetched above)

    const elapsed = state.assignedAt ? now - state.assignedAt : 0;

    // 5. Fresh heartbeat AND under wall time limit: skip watchdog
    if (
      hbAge !== null && hbAge < HEARTBEAT_FRESH_MS &&
      elapsed < MAX_TASK_WALL_MS
    ) {
      log.debug`${state.name}: heartbeat fresh (${
        Math.round(hbAge / 1000)
      }s), skipping watchdog`;
      continue;
    }

    // 6. Over wall time limit: force watchdog regardless of heartbeat
    if (elapsed >= MAX_TASK_WALL_MS) {
      log
        .info`[wall-time] ${state.name}: task running for ${
        Math.round(elapsed / 60_000)
      }min, forcing watchdog`;
      watchdogChecks.push(
        watchdog(session, state, base, approval, now),
      );
      continue;
    }

    // 7. Normal Tier 2/3 logic (no fresh heartbeat)
    if (state.assignedAt && elapsed > TIER2_TIMEOUT_MS) {
      watchdogChecks.push(
        watchdog(session, state, base, approval, now),
      );
    }
  }

  if (watchdogChecks.length > 0) await Promise.all(watchdogChecks);

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

    // Clear all signals before sending task and write current-task file
    await clearAllWorkerSignals(base, state.name);
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
    resetWatchdog(state);
    log.info`[assign] ${task.id} -> ${state.name}: ${task.summary}`;
    taskIdx++;
  }

  printStatus(workers);
}

/** Reset watchdog-related fields on a worker. */
function resetWatchdog(state: WorkerState): void {
  state.lastPaneSnapshot = null;
  state.lastSnapshotAt = null;
  state.llmEvalCount = 0;
  state.escalatedToUser = false;
}

/** Reset worker state fields when a task completes. */
function markComplete(state: WorkerState, idleWorkers: WorkerState[]): void {
  state.assignedAt = null;
  resetWatchdog(state);
  idleWorkers.push(state);
}

/** Check if pane content contains the completion marker for the given task. */
export function paneContainsMarker(
  paneContent: string,
  taskId: string,
): boolean {
  return paneContent.includes(completionMarker(taskId));
}

/** Tiered watchdog: cheap pane check first, then LLM eval with guardrails. */
async function watchdog(
  session: string,
  state: WorkerState,
  base: string,
  approval: ApprovalMode,
  now: number,
): Promise<void> {
  const target = `${session}:${state.name}`;
  let paneContent: string;
  try {
    paneContent = await tmux.capturePane(target, 50);
  } catch {
    return; // Pane gone or inaccessible
  }

  // --- Daemon-side marker scan (runs every tick, independent of tiers) ---
  if (state.currentTask && paneContainsMarker(paneContent, state.currentTask)) {
    log
      .info`[marker-scan] ${state.name}: found completion marker in pane for ${state.currentTask}, creating signal`;
    try {
      const sig = signalPath(base, state.name);
      await Deno.writeTextFile(sig, "");
    } catch (e) {
      log.warn`[marker-scan] Failed to write signal for ${state.name}: ${
        e instanceof Error ? e.message : e
      }`;
    }
    return;
  }

  // --- Tier 2: pane snapshot diff (TIER2 - TIER3 window) ---
  const elapsed = now - (state.assignedAt ?? now);
  if (elapsed < TIER3_TIMEOUT_MS) {
    const prevSnapshot = state.lastPaneSnapshot;
    state.lastPaneSnapshot = paneContent;
    state.lastSnapshotAt = now;

    if (prevSnapshot === null) {
      log.debug`${state.name}: tier2 — first snapshot captured`;
      return;
    }

    if (prevSnapshot === paneContent) {
      // Pane is unchanged — agent is idle
      log
        .info`[tier2-idle] ${state.name}: pane unchanged, nudging for marker`;
      const marker = completionMarker(state.currentTask!);
      const nudge =
        `If you have completed your task, output exactly this on its own line: ${marker}`;
      try {
        await tmux.sendKeys(target, nudge);
      } catch {
        // pane may be gone
      }
    } else {
      log.debug`${state.name}: tier2 — pane changed, agent still active`;
    }
    return;
  }

  // --- Tier 3: LLM evaluation with guardrails ---
  // Keep updating snapshot for continuity
  state.lastPaneSnapshot = paneContent;
  state.lastSnapshotAt = now;

  if (state.escalatedToUser) {
    log.debug`${state.name}: already escalated to user, skipping`;
    return;
  }

  if (state.llmEvalCount >= MAX_LLM_EVALS) {
    log
      .warn`[tier3-escalate] ${state.name}: ${MAX_LLM_EVALS} LLM evals exhausted, notifying user`;
    state.escalatedToUser = true;
    try {
      await tmux.displayMessage(
        session,
        `JACKOPS: ${state.name} may be stuck - ${MAX_LLM_EVALS} checks failed, needs manual attention`,
      );
    } catch {
      // display-message may fail if no client attached
    }
    return;
  }

  if (approval === "yolo") {
    log.info`[yolo-approve] ${state.name}: sending Enter`;
    log.debug`${state.name}: pane content (last 50 lines):\n${paneContent}`;
    try {
      await tmux.sendKeys(target, "", true);
    } catch {
      // pane may be gone
    }
    state.llmEvalCount++;
    return;
  }

  if (approval === "manual") {
    log.info`[stall-detected] ${state.name}: may need attention`;
    log.debug`${state.name}: pane content (last 50 lines):\n${paneContent}`;
    state.escalatedToUser = true;
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

  // approval === "auto" — LLM evaluation (no auto-respond, only permission prompts)
  state.llmEvalCount++;
  log
    .info`[tier3-eval] Evaluating ${state.name} via LLM (${state.llmEvalCount}/${MAX_LLM_EVALS})...`;
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
      .info`[tier3-result] ${state.name}: status=${result.status} safe=${result.safe_to_approve} action=${
      result.approval_keystroke || "none"
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
        state.escalatedToUser = true;
        try {
          const msg =
            `JACKOPS: ${state.name} needs approval - ${result.reason}`;
          await tmux.displayMessage(session, msg);
        } catch {
          // display-message may fail if no client attached
        }
      }
    } else if (result.status === "error") {
      log.info`[error-detected] ${state.name}: ${result.reason}`;
      state.escalatedToUser = true;
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

/** Check if the orchestrator agent is alive and progressing. */
export async function checkOrchestrator(
  session: string,
  state: OrchestratorState,
  approval: ApprovalMode,
): Promise<void> {
  const target = `${session}:orchestrator`;
  let paneContent: string;
  try {
    paneContent = await tmux.capturePane(target, 50);
  } catch {
    // Pane gone — orchestrator crashed or was killed
    if (!state.escalatedToUser) {
      log
        .warn`[orchestrator] Pane capture failed — orchestrator may have crashed`;
      state.escalatedToUser = true;
      try {
        await tmux.displayMessage(
          session,
          "JACKOPS: orchestrator pane gone -- may have crashed",
        );
      } catch {
        // display-message may fail if no client attached
      }
    }
    return;
  }

  // Pane changed — orchestrator is active, reset escalation
  if (
    state.lastPaneSnapshot !== null && paneContent !== state.lastPaneSnapshot
  ) {
    state.lastPaneSnapshot = paneContent;
    state.lastSnapshotAt = Date.now();
    state.llmEvalCount = 0;
    state.escalatedToUser = false;
    log.debug`[orchestrator] Pane changed, still active`;
    return;
  }

  // First snapshot — just record it
  if (state.lastPaneSnapshot === null) {
    state.lastPaneSnapshot = paneContent;
    state.lastSnapshotAt = Date.now();
    log.debug`[orchestrator] First snapshot captured`;
    return;
  }

  // Pane unchanged — check if stall threshold exceeded
  const stalledFor = Date.now() - (state.lastSnapshotAt ?? Date.now());
  if (stalledFor < ORCH_STALL_TIMEOUT_MS) {
    log.debug`[orchestrator] Pane unchanged for ${
      Math.round(stalledFor / 1000)
    }s (threshold: ${ORCH_STALL_TIMEOUT_MS / 1000}s)`;
    return;
  }

  // Stall detected — already escalated?
  if (state.escalatedToUser) {
    log.debug`[orchestrator] Already escalated to user, skipping`;
    return;
  }

  if (state.llmEvalCount >= MAX_LLM_EVALS) {
    log
      .warn`[orchestrator] ${MAX_LLM_EVALS} LLM evals exhausted, notifying user`;
    state.escalatedToUser = true;
    try {
      await tmux.displayMessage(
        session,
        `JACKOPS: orchestrator may be stuck - ${MAX_LLM_EVALS} checks failed`,
      );
    } catch {
      // display-message may fail if no client attached
    }
    return;
  }

  if (approval === "yolo") {
    log.info`[orchestrator] Stalled, sending Enter (yolo)`;
    try {
      await tmux.sendKeys(target, "", true);
    } catch {
      // pane may be gone
    }
    state.llmEvalCount++;
    return;
  }

  if (approval === "manual") {
    log.info`[orchestrator] Stalled, notifying user (manual)`;
    state.escalatedToUser = true;
    try {
      await tmux.displayMessage(
        session,
        "JACKOPS: orchestrator may be stuck - check manually",
      );
    } catch {
      // display-message may fail
    }
    return;
  }

  // approval === "auto" — LLM evaluation
  state.llmEvalCount++;
  log
    .info`[orchestrator] Evaluating via LLM (${state.llmEvalCount}/${MAX_LLM_EVALS})...`;

  try {
    const result = await evaluatePane(
      paneContent,
      "orchestrator agent reviewing tasks",
      state.agent,
    );
    log
      .info`[orchestrator] LLM: status=${result.status} safe=${result.safe_to_approve} action=${
      result.approval_keystroke || "none"
    } reason=${result.reason}`;

    if (result.status === "permission_prompt") {
      if (result.safe_to_approve && result.approval_keystroke) {
        log.info`[orchestrator] Auto-approving: ${result.reason}`;
        if (result.approval_keystroke === "Enter") {
          await tmux.sendKeys(target, "", true);
        } else {
          await tmux.sendKeys(target, result.approval_keystroke);
        }
      } else {
        log.info`[orchestrator] Needs manual attention: ${result.reason}`;
        state.escalatedToUser = true;
        try {
          await tmux.displayMessage(
            session,
            `JACKOPS: orchestrator needs approval - ${result.reason}`,
          );
        } catch {
          // display-message may fail
        }
      }
    } else if (result.status === "error") {
      log.info`[orchestrator] Error detected: ${result.reason}`;
      state.escalatedToUser = true;
      try {
        await tmux.displayMessage(
          session,
          `JACKOPS: orchestrator hit an error - ${result.reason}`,
        );
      } catch {
        // display-message may fail
      }
    }
  } catch (e) {
    log.error`[orchestrator] LLM evaluation failed: ${
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
