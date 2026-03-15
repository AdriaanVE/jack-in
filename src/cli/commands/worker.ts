/** `jackops send` and `jackops attach` command handlers. */

import { sessionName } from "../../config.ts";
import * as tmux from "../../tmux.ts";
import {
  loadCliConfig,
  requireActiveSession,
  requireWorker,
} from "../context.ts";

export async function send(workerName: string, message: string): Promise<void> {
  const { config } = await loadCliConfig();
  const session = sessionName(config.project);
  requireWorker(config, workerName);
  await requireActiveSession(session);

  await tmux.sendKeys(`${session}:${workerName}`, message);
  console.log(`Sent to ${workerName}.`);
}

export async function attach(workerName: string): Promise<void> {
  const { config } = await loadCliConfig();
  const session = sessionName(config.project);
  requireWorker(config, workerName);
  await requireActiveSession(session);

  // If already inside tmux, switch. Otherwise attach.
  if (Deno.env.get("TMUX")) {
    await tmux.selectWindow(session, workerName);
  } else {
    const cmd = new Deno.Command("tmux", {
      args: ["attach", "-t", session, ";", "select-window", "-t", workerName],
      stdin: "inherit",
      stdout: "inherit",
      stderr: "inherit",
    });
    const { code } = await cmd.spawn().status;
    Deno.exit(code);
  }
}
