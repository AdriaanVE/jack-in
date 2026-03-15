/** `jackops daemon` command handler. */

import { sessionName } from "../../config.ts";
import * as tq from "../../task-queue.ts";
import * as daemon from "../../daemon.ts";
import { setupLogging } from "../../log.ts";
import {
  checkUnknownFlags,
  loadCliConfig,
  parseApproval,
  requireActiveSession,
} from "../context.ts";

export async function daemonCmd(args: string[]): Promise<void> {
  checkUnknownFlags(args, new Set(["--approval"]));
  const { config } = await loadCliConfig();
  const approval = parseApproval(args);
  if (approval) config.orchestrator.approval = approval;
  const base = Deno.cwd();
  const session = sessionName(config.project);
  await requireActiveSession(session);
  const closeLog = await setupLogging(base);
  await tq.init(base);
  const ac = new AbortController();
  Deno.addSignalListener("SIGINT", () => ac.abort());
  await daemon.run({ config, base, signal: ac.signal });
  closeLog();
}
