/** `jackops status` command handler. */

import {
  formatJsonStatus,
  formatStatus,
  getSessionStarted,
  getStatus,
} from "../../status.ts";
import * as tq from "../../task-queue.ts";
import * as daemon from "../../daemon.ts";
import { checkUnknownFlags, loadCliConfig } from "../context.ts";

export async function status(args: string[]): Promise<void> {
  checkUnknownFlags(args, new Set(["--json"]));
  const json = args.includes("--json");

  const { config } = await loadCliConfig();
  const base = Deno.cwd();
  const [
    { workers: statuses, daemon: dm },
    startedEpoch,
    tasks,
    runtimeApproval,
  ] = await Promise
    .all([
      getStatus(config, base),
      getSessionStarted(config),
      tq.counts(base),
      daemon.readApprovalMode(base),
    ]);
  const effectiveApproval = runtimeApproval ?? config.orchestrator.approval;
  const info = {
    statuses,
    startedEpoch,
    daemon: dm,
    tasks,
    runtimeApproval,
    autoApprovalModel: effectiveApproval === "auto"
      ? (Deno.env.get("ANTHROPIC_DEFAULT_SONNET_MODEL") ??
        "claude-sonnet-4-5")
      : undefined,
  };
  if (json) {
    console.log(JSON.stringify(formatJsonStatus(config, info), null, 2));
  } else {
    console.log(formatStatus(config, info));
  }
}
