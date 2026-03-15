/** `jackops down` command handler. */

import * as tmux from "../../tmux.ts";
import * as worktree from "../../worktree.ts";
import { sessionName } from "../../config.ts";
import { INIT_SESSION } from "../../init.ts";
import { loadCliConfig, prompt } from "../context.ts";

export async function down(): Promise<void> {
  const base = Deno.cwd();
  let project: string | undefined;

  // Try loading config for project name, but don't require it
  try {
    const { config } = await loadCliConfig();
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

  // Also kill init session if it exists
  if (await tmux.hasSession(INIT_SESSION)) {
    await tmux.killSession(INIT_SESSION);
    console.log(`Killed init session '${INIT_SESSION}'.`);
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

  const answer = await prompt("\nRemove worktrees? [y/N] ");

  if (answer === "y") {
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
