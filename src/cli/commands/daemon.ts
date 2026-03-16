/** `jackops daemon` command handler. */

import { sessionName } from "../../config.ts";
import * as tq from "../../task-queue.ts";
import * as daemon from "../../daemon.ts";
import { enableRingBuffer, setupLogging } from "../../log.ts";
import { createDashboard } from "../../tui.ts";
import {
  checkUnknownFlags,
  loadCliConfig,
  parseApproval,
  requireActiveSession,
} from "../context.ts";
import { down } from "./down.ts";

export async function daemonCmd(args: string[]): Promise<void> {
  checkUnknownFlags(args, new Set(["--approval"]));
  const { config } = await loadCliConfig();
  const approval = parseApproval(args);
  if (approval) config.orchestrator.approval = approval;
  const base = Deno.cwd();
  const session = sessionName(config.project);
  await requireActiveSession(session);

  const isTTY = Deno.stdin.isTerminal();
  const closeLog = await setupLogging(base, {
    suppressConsole: isTTY,
  });

  await tq.init(base);

  const ac = new AbortController();
  Deno.addSignalListener("SIGINT", () => ac.abort());

  const ref: { destroy: (() => void) | null } = { destroy: null };
  let runDown = false;

  if (isTTY) {
    enableRingBuffer();
    await daemon.run({
      config,
      base,
      signal: ac.signal,
      onContext: (ctx) => {
        const dashboard = createDashboard({
          ctx,
          onDown: () => {
            runDown = true;
            ac.abort();
            ref.destroy?.();
          },
        });
        ref.destroy = dashboard.destroy;
        dashboard.tui.run();
      },
    });
    ref.destroy?.();
  } else {
    await daemon.run({ config, base, signal: ac.signal });
  }

  closeLog();

  if (runDown) {
    await down();
  }
}
