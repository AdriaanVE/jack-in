/** Git worktree management. */

import { exec } from "./subprocess.ts";

function git(args: string[], cwd?: string) {
  return exec("git", args, cwd);
}

export const WORKTREE_PREFIX = ".w-";

export function worktreeDir(project: string, name: string): string {
  return `${WORKTREE_PREFIX}${project}-${name}`;
}

export function worktreePath(base: string, project: string, name: string): string {
  return `${base}/${worktreeDir(project, name)}`;
}

function branchName(project: string, name: string): string {
  return `jackops/${project}/${name}`;
}

export async function create(base: string, project: string, name: string): Promise<string> {
  const wt = worktreePath(base, project, name);
  const branch = branchName(project, name);
  const { success, stderr } = await git(
    ["worktree", "add", wt, "-b", branch],
    base,
  );
  if (!success) {
    // Branch may already exist from a previous run
    if (stderr.includes("already exists")) {
      const retry = await git(["worktree", "add", wt, branch], base);
      if (!retry.success) throw new Error(`git worktree add failed: ${retry.stderr}`);
    } else {
      throw new Error(`git worktree add failed: ${stderr}`);
    }
  }
  return wt;
}

export async function removeByPath(path: string, base: string): Promise<void> {
  const { success, stderr } = await git(
    ["worktree", "remove", path, "--force"],
    base,
  );
  if (!success) throw new Error(`git worktree remove failed: ${stderr}`);
}

export interface WorktreeInfo {
  path: string;
  branch: string;
  bare: boolean;
}

export async function list(base: string): Promise<WorktreeInfo[]> {
  const { success, stdout, stderr } = await git(
    ["worktree", "list", "--porcelain"],
    base,
  );
  if (!success) throw new Error(`git worktree list failed: ${stderr}`);
  if (!stdout) return [];

  const entries: WorktreeInfo[] = [];
  let current: Partial<WorktreeInfo> = {};

  for (const line of stdout.split("\n")) {
    if (line.startsWith("worktree ")) {
      current.path = line.slice("worktree ".length);
    } else if (line.startsWith("branch ")) {
      current.branch = line.slice("branch ".length);
    } else if (line === "bare") {
      current.bare = true;
    } else if (line === "") {
      if (current.path) {
        entries.push({
          path: current.path,
          branch: current.branch ?? "",
          bare: current.bare ?? false,
        });
      }
      current = {};
    }
  }
  // Last entry (no trailing blank line)
  if (current.path) {
    entries.push({
      path: current.path,
      branch: current.branch ?? "",
      bare: current.bare ?? false,
    });
  }

  return entries;
}

export function isJackopsWorktree(entry: WorktreeInfo, project?: string): boolean {
  const dirName = entry.path.split("/").pop() ?? "";
  if (!dirName.startsWith(WORKTREE_PREFIX)) return false;
  if (project) {
    return dirName.startsWith(`${WORKTREE_PREFIX}${project}-`);
  }
  return true;
}

/** Remove jackops worktrees for a project. Uses entry.path directly. */
export async function cleanup(base: string, project: string, entries?: WorktreeInfo[]): Promise<string[]> {
  const all = entries ?? await list(base);
  const removed: string[] = [];
  for (const entry of all) {
    if (isJackopsWorktree(entry, project)) {
      try {
        await removeByPath(entry.path, base);
        removed.push(entry.path);
      } catch (e) {
        console.error(`  Failed to remove ${entry.path}: ${e instanceof Error ? e.message : e}`);
      }
    }
  }
  return removed;
}
