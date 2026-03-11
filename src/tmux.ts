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

export async function sendKeys(target: string, text: string): Promise<void> {
  const { success, stderr } = await run(["send-keys", "-t", target, text, "C-m"]);
  if (!success) throw new Error(`tmux send-keys failed: ${stderr}`);
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
  paneDead: boolean;
  currentCommand: string;
}

export async function listPanes(session: string): Promise<PaneInfo[]> {
  const { success, stdout, stderr } = await run([
    "list-panes",
    "-t",
    session,
    "-a",
    "-F",
    "#{window_name}\t#{pane_dead}\t#{pane_current_command}",
  ]);
  if (!success) throw new Error(`tmux list-panes failed: ${stderr}`);
  if (!stdout) return [];
  return stdout.split("\n").map((line) => {
    const [windowName, dead, cmd] = line.split("\t");
    return { windowName, paneDead: dead === "1", currentCommand: cmd ?? "" };
  });
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
