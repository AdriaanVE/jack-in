/** `jackops approval` command handler. */

import { join } from "@std/path";
import { APPROVAL_MODES, isApprovalMode } from "../../config.ts";
import * as worktree from "../../worktree.ts";
import * as daemon from "../../daemon.ts";
import { loadCliConfig } from "../context.ts";

export async function approval(args: string[]): Promise<void> {
  const [mode] = args;
  const base = Deno.cwd();
  const { config } = await loadCliConfig();

  if (!mode) {
    // Show current mode
    const runtime = await daemon.readApprovalMode(base);
    const current = runtime ?? config.orchestrator.approval;
    const source = runtime ? "runtime" : "config";
    console.log(`Approval mode: ${current} (${source})`);
    return;
  }

  if (!isApprovalMode(mode)) {
    console.error(
      `Invalid approval mode '${mode}'. Must be one of: ${
        APPROVAL_MODES.join(", ")
      }`,
    );
    Deno.exit(1);
  }

  // Rewrite Claude settings for each Claude worker
  const switched: string[] = [];
  const skipped: string[] = [];

  for (const w of config.workers) {
    if (w.agent === "claude") {
      const wt = join(
        base,
        worktree.worktreeDir(config.project, w.name),
      );
      await daemon.writeClaudeSettings(wt, base, w.name, mode);
      switched.push(w.name);
    } else {
      skipped.push(`${w.name} (${w.agent})`);
    }
  }

  // Update orchestrator settings if Claude
  if (config.orchestrator.agent === "claude") {
    await daemon.mergeClaudeSettings(base, base, "orchestrator", mode);
    switched.push("orchestrator");
  }

  // Write runtime mode file for daemon
  await daemon.writeApprovalMode(base, mode);

  console.log(`Approval mode switched to: ${mode}`);
  if (switched.length > 0) {
    console.log(`  Updated: ${switched.join(", ")}`);
  }
  if (skipped.length > 0) {
    console.log(
      `  Skipped (no live switching): ${skipped.join(", ")}`,
    );
  }
}
