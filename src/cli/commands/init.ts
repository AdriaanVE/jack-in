/** `jackops init` command handler. */

import { isAgentType } from "../../agents.ts";
import { init, initTemplate } from "../../init.ts";
import { checkUnknownFlags } from "../context.ts";

export async function initCmd(args: string[]): Promise<void> {
  checkUnknownFlags(args, new Set(["--agent", "--template"]));
  if (args.includes("--template")) {
    await initTemplate();
  } else {
    const agentIdx = args.indexOf("--agent");
    let agent: string | undefined;
    if (agentIdx >= 0 && agentIdx + 1 < args.length) {
      agent = args[agentIdx + 1];
      if (!isAgentType(agent)) {
        console.error(
          `Invalid agent '${agent}'. Must be one of: claude, codex, opencode, gemini`,
        );
        Deno.exit(1);
      }
    }
    await init({
      agent: agent as import("../../agents.ts").AgentType | undefined,
    });
  }
}
