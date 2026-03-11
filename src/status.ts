/** Query tmux state and format status output. */

import { type Config, sessionName } from "./config.ts";
import * as tmux from "./tmux.ts";
import { worktreeDir } from "./worktree.ts";

interface WorkerStatus {
  name: string;
  agent: string;
  state: "running" | "idle" | "gone";
  worktree: string;
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

export function formatStatus(config: Config, statuses: WorkerStatus[]): string {
  const lines: string[] = [];
  lines.push(`JACKOPS -- ${config.project}`);
  lines.push("");

  if (statuses.length === 0) {
    lines.push("  No active session.");
    return lines.join("\n");
  }

  const nameWidth = Math.max(...statuses.map((s) => s.name.length), 4);
  const agentWidth = Math.max(...statuses.map((s) => s.agent.length), 5);

  for (const s of statuses) {
    lines.push(
      `  ${s.name.padEnd(nameWidth)}  ${s.agent.padEnd(agentWidth)}  ${s.state.padEnd(7)}  ${s.worktree}`,
    );
  }

  return lines.join("\n");
}
