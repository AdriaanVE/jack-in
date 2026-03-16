/** Query tmux state and format status output. */

import { join } from "@std/path";
import { type ApprovalMode, type Config, sessionName } from "./config.ts";
import type { TaskCounts } from "./task-queue.ts";
import * as tmux from "./tmux.ts";
import { worktreeDir } from "./worktree.ts";

const CURRENT_TASK_DIR = ".jackops/current-task";

function formatTime(epoch: number): string {
  const d = new Date(epoch * 1000);
  return d.toLocaleString([], {
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  });
}

export type WorkerState = "working" | "waiting" | "stopped" | "gone";

export interface WorkerStatus {
  name: string;
  agent: string;
  state: WorkerState;
  worktree: string;
}

export interface DaemonStatus {
  running: boolean;
}

export type { TaskCounts };

const APPROVAL_DESCRIPTIONS: Record<ApprovalMode, string> = {
  manual: "manual approval required",
  auto: "LLM evaluates permission prompts",
  yolo: "all prompts auto-approved",
};

const SHELLS = new Set(["bash", "zsh", "fish", "sh"]);

export async function getSessionStarted(
  config: Config,
): Promise<number | null> {
  const session = sessionName(config.project);
  if (!(await tmux.hasSession(session))) return null;
  return await tmux.sessionCreated(session);
}

function paneIsRunning(pane: tmux.PaneInfo): boolean {
  const cmd = pane.currentCommand;
  return !pane.paneDead && !!cmd && !SHELLS.has(cmd);
}

async function hasCurrentTask(
  base: string,
  workerName: string,
): Promise<boolean> {
  try {
    await Deno.stat(join(base, CURRENT_TASK_DIR, workerName));
    return true;
  } catch (e) {
    if (e instanceof Deno.errors.NotFound) return false;
    throw e;
  }
}

export async function getStatus(
  config: Config,
  base?: string,
): Promise<{ workers: WorkerStatus[]; daemon: DaemonStatus }> {
  const session = sessionName(config.project);
  if (!(await tmux.hasSession(session))) {
    return { workers: [], daemon: { running: false } };
  }

  const panes = await tmux.listPanes(session);
  const paneByWindow = new Map<string, tmux.PaneInfo>();
  for (const p of panes) {
    paneByWindow.set(p.windowName, p);
  }

  const workers = await Promise.all(config.workers.map(async (w) => {
    const pane = paneByWindow.get(w.name);
    let state: WorkerState = "gone";
    if (pane) {
      if (!paneIsRunning(pane)) {
        state = "stopped";
      } else if (base && await hasCurrentTask(base, w.name)) {
        state = "working";
      } else {
        state = "waiting";
      }
    }
    return {
      name: w.name,
      agent: w.agent,
      state,
      worktree: worktreeDir(config.project, w.name),
    };
  }));

  const dashboard = paneByWindow.get("dashboard-orchestrator");
  const daemon: DaemonStatus = {
    running: !!dashboard && paneIsRunning(dashboard),
  };

  return { workers, daemon };
}

export interface StatusInfo {
  statuses: WorkerStatus[];
  startedEpoch: number | null;
  daemon: DaemonStatus;
  tasks: TaskCounts | null;
  autoApprovalModel?: string | null;
  runtimeApproval?: ApprovalMode | null;
}

export interface JsonStatus {
  session: string;
  daemon: DaemonStatus;
  workers: (WorkerStatus & { branch: string })[];
  tasks: TaskCounts;
}

export function formatJsonStatus(
  config: Config,
  info: StatusInfo,
): JsonStatus {
  const session = sessionName(config.project);
  return {
    session,
    daemon: info.daemon,
    workers: info.statuses.map((s) => ({
      ...s,
      branch: `jackops/${config.project}/${s.name}`,
    })),
    tasks: info.tasks ??
      { pending: 0, current: 0, review: 0, complete: 0, rejected: 0 },
  };
}

export function formatStatus(config: Config, info: StatusInfo): string {
  const { statuses, startedEpoch, daemon, tasks } = info;
  const lines: string[] = [];
  const session = sessionName(config.project);
  const approval = info.runtimeApproval ?? config.orchestrator.approval;

  // Header
  const header = startedEpoch
    ? `JACKOPS -- ${config.project} (started ${formatTime(startedEpoch)})`
    : `JACKOPS -- ${config.project}`;
  lines.push(header);

  if (statuses.length === 0) {
    lines.push("");
    lines.push("  No active session.");
    return lines.join("\n");
  }

  // Session info
  lines.push(`Session:  ${session}`);
  lines.push(`Daemon:   ${daemon.running ? "running" : "stopped"}`);
  lines.push(`Approval: ${approval} (${APPROVAL_DESCRIPTIONS[approval]})`);
  if (approval === "auto" && info.autoApprovalModel) {
    lines.push(`Model:    ${info.autoApprovalModel}`);
  }

  // Workers
  lines.push("");
  lines.push("Workers:");

  const nameWidth = Math.max(...statuses.map((s) => s.name.length), 4);
  const agentWidth = Math.max(...statuses.map((s) => s.agent.length), 5);
  const stateWidth = Math.max(...statuses.map((s) => s.state.length), 6);

  for (const s of statuses) {
    lines.push(
      `  ${s.name.padEnd(nameWidth)}  ${s.agent.padEnd(agentWidth)}  ${
        s.state.padEnd(stateWidth)
      }  ${s.worktree}`,
    );
  }

  // Tasks
  if (tasks) {
    const total = tasks.pending + tasks.current + tasks.review +
      tasks.complete + tasks.rejected;
    if (total > 0) {
      lines.push("");
      lines.push(
        `Tasks: ${tasks.pending} pending, ${tasks.current} current, ${tasks.review} review, ${tasks.complete} complete, ${tasks.rejected} rejected`,
      );
    }
  }

  return lines.join("\n");
}
