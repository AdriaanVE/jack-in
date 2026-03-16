/** Typed wrappers around tmux commands. */

import { exec } from "./subprocess.ts";

function run(args: string[]) {
  return exec("tmux", args);
}

export async function hasSession(name: string): Promise<boolean> {
  const { success } = await run(["has-session", "-t", name]);
  return success;
}

export async function createSession(name: string): Promise<void> {
  const { success, stderr } = await run(["new-session", "-d", "-s", name]);
  if (!success) throw new Error(`tmux new-session failed: ${stderr}`);
}

export async function killSession(name: string): Promise<void> {
  const { success, stderr } = await run(["kill-session", "-t", name]);
  if (!success) throw new Error(`tmux kill-session failed: ${stderr}`);
}

export async function createWindow(
  session: string,
  name: string,
): Promise<void> {
  const { success, stderr } = await run([
    "new-window",
    "-t",
    session,
    "-n",
    name,
  ]);
  if (!success) throw new Error(`tmux new-window failed: ${stderr}`);
}

export async function sendKeys(
  target: string,
  text: string,
  enter = true,
): Promise<void> {
  // Send text and Enter as separate calls -- TUI apps (Codex, Gemini) don't
  // reliably submit when Enter is appended to the same send-keys invocation.
  if (text) {
    const { success, stderr } = await run(["send-keys", "-t", target, text]);
    if (!success) throw new Error(`tmux send-keys failed: ${stderr}`);
  }
  if (enter) {
    const { success, stderr } = await run([
      "send-keys",
      "-t",
      target,
      "Enter",
    ]);
    if (!success) throw new Error(`tmux send-keys (enter) failed: ${stderr}`);
  }
}

export async function capturePane(
  target: string,
  lines = 50,
): Promise<string> {
  const { success, stdout, stderr } = await run([
    "capture-pane",
    "-t",
    target,
    "-p",
    "-S",
    `-${lines}`,
  ]);
  if (!success) throw new Error(`tmux capture-pane failed: ${stderr}`);
  return stdout;
}

export interface WindowInfo {
  name: string;
  active: boolean;
  index: number;
}

export async function listWindows(session: string): Promise<WindowInfo[]> {
  const { success, stdout, stderr } = await run([
    "list-windows",
    "-t",
    session,
    "-F",
    "#{window_index}\t#{window_name}\t#{window_active}",
  ]);
  if (!success) throw new Error(`tmux list-windows failed: ${stderr}`);
  if (!stdout) return [];
  return stdout.split("\n").map((line) => {
    const [index, name, active] = line.split("\t");
    return { index: parseInt(index), name, active: active === "1" };
  });
}

export interface PaneInfo {
  windowName: string;
  paneIndex: number;
  paneId: string; // stable %N id
  paneDead: boolean;
  currentCommand: string;
}

export async function listPanes(session: string): Promise<PaneInfo[]> {
  const { success, stdout, stderr } = await run([
    "list-panes",
    "-t",
    session,
    "-s",
    "-F",
    "#{window_name}\t#{pane_index}\t#{pane_dead}\t#{pane_current_command}\t#{pane_id}",
  ]);
  if (!success) throw new Error(`tmux list-panes failed: ${stderr}`);
  if (!stdout) return [];
  return stdout.split("\n").map((line) => {
    const [windowName, idx, dead, cmd, paneId] = line.split("\t");
    return {
      windowName,
      paneIndex: parseInt(idx),
      paneId: paneId ?? "",
      paneDead: dead === "1",
      currentCommand: cmd ?? "",
    };
  });
}

/** Get session creation time as a unix epoch (seconds), or null. */
export async function sessionCreated(name: string): Promise<number | null> {
  const { success, stdout } = await run([
    "display-message",
    "-t",
    name,
    "-p",
    "#{session_created}",
  ]);
  if (!success || !stdout) return null;
  const epoch = parseInt(stdout.trim());
  return isNaN(epoch) ? null : epoch;
}

export async function selectWindow(
  session: string,
  name: string,
): Promise<void> {
  const { success, stderr } = await run([
    "select-window",
    "-t",
    `${session}:${name}`,
  ]);
  if (!success) throw new Error(`tmux select-window failed: ${stderr}`);
}

/** Switch the current tmux client to a different session. */
export async function switchClient(targetSession: string): Promise<void> {
  const { success, stderr } = await run([
    "switch-client",
    "-t",
    targetSession,
  ]);
  if (!success) throw new Error(`tmux switch-client failed: ${stderr}`);
}

/** Swap two panes. Use fully-qualified targets (e.g. session:window.pane). */
export async function swapPane(
  source: string,
  target: string,
): Promise<void> {
  const { success, stderr } = await run([
    "swap-pane",
    "-s",
    source,
    "-t",
    target,
  ]);
  if (!success) throw new Error(`tmux swap-pane failed: ${stderr}`);
}

/** Display a message on the tmux status line. */
export async function displayMessage(
  session: string,
  message: string,
): Promise<void> {
  const { success, stderr } = await run([
    "display-message",
    "-t",
    session,
    message,
  ]);
  if (!success) throw new Error(`tmux display-message failed: ${stderr}`);
}

/** Split the target window horizontally, creating a new pane below. */
export async function splitWindow(
  session: string,
  targetWindow: string,
  percentage?: number,
): Promise<void> {
  const args = [
    "split-window",
    "-t",
    `${session}:${targetWindow}`,
    "-v",
  ];
  if (percentage !== undefined) {
    args.push("-p", String(percentage));
  }
  const { success, stderr } = await run(args);
  if (!success) throw new Error(`tmux split-window failed: ${stderr}`);
}

/** Set a user option on a specific pane. */
export async function setPaneOption(
  target: string,
  option: string,
  value: string,
): Promise<void> {
  const { success, stderr } = await run([
    "set-option",
    "-p",
    "-t",
    target,
    option,
    value,
  ]);
  if (!success) throw new Error(`tmux set-option failed: ${stderr}`);
}

/** Find a pane's %N ID by its @jackops_role user option within a session. */
export async function findPaneByRole(
  session: string,
  role: string,
): Promise<string | null> {
  const { success, stdout } = await run([
    "list-panes",
    "-s",
    "-t",
    session,
    "-F",
    "#{pane_id}\t#{@jackops_role}",
  ]);
  if (!success || !stdout) return null;
  for (const line of stdout.split("\n")) {
    const [paneId, paneRole] = line.split("\t");
    if (paneRole === role) return paneId;
  }
  return null;
}

/** Rename the first window (index 0) created with the session. */
export async function renameWindow(
  session: string,
  index: number,
  name: string,
): Promise<void> {
  const { success, stderr } = await run([
    "rename-window",
    "-t",
    `${session}:${index}`,
    name,
  ]);
  if (!success) throw new Error(`tmux rename-window failed: ${stderr}`);
}
