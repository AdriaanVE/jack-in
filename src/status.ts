/** Query tmux state and format status output. */

import { type Config, sessionName } from "./config.ts";
import * as tmux from "./tmux.ts";
import { worktreeDir } from "./worktree.ts";

function formatTime(epoch: number): string {
  const d = new Date(epoch * 1000);
  return d.toLocaleString([], {
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  });
}

interface WorkerStatus {
  name: string;
  agent: string;
  state: "running" | "idle" | "gone";
  worktree: string;
}

export async function getSessionStarted(
  config: Config,
): Promise<number | null> {
  const session = sessionName(config.project);
  if (!(await tmux.hasSession(session))) return null;
  return await tmux.sessionCreated(session);
}

export async function getStatus(config: Config): Promise<WorkerStatus[]> {
  const session = sessionName(config.project);
  if (!(await tmux.hasSession(session))) return [];

  const panes = await tmux.listPanes(session);
  const paneByWindow = new Map<string, tmux.PaneInfo>();
  for (const p of panes) {
    paneByWindow.set(p.windowName, p);
  }

  return config.workers.map((w) => {
    const pane = paneByWindow.get(w.name);
    let state: WorkerStatus["state"] = "gone";
    if (pane) {
      // If pane is dead or current command is a shell, agent has exited
      const shell = pane.currentCommand;
      const isShell = !shell || ["bash", "zsh", "fish", "sh"].includes(shell);
      state = pane.paneDead || isShell ? "idle" : "running";
    }
    return {
      name: w.name,
      agent: w.agent,
      state,
      worktree: worktreeDir(config.project, w.name),
    };
  });
}

export function formatStatus(
  config: Config,
  statuses: WorkerStatus[],
  startedEpoch?: number | null,
): string {
  const lines: string[] = [];
  const header = startedEpoch
    ? `JACKOPS -- ${config.project} (started ${formatTime(startedEpoch)})`
    : `JACKOPS -- ${config.project}`;
  lines.push(header);
  lines.push("");

  if (statuses.length === 0) {
    lines.push("  No active session.");
    return lines.join("\n");
  }

  const nameWidth = Math.max(...statuses.map((s) => s.name.length), 4);
  const agentWidth = Math.max(...statuses.map((s) => s.agent.length), 5);

  for (const s of statuses) {
    lines.push(
      `  ${s.name.padEnd(nameWidth)}  ${s.agent.padEnd(agentWidth)}  ${
        s.state.padEnd(7)
      }  ${s.worktree}`,
    );
  }

  return lines.join("\n");
}
