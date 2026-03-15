/** CLI dispatch: parses top-level command and delegates to handlers. */

import { USAGE } from "./context.ts";
import { initCmd } from "./commands/init.ts";
import { up } from "./commands/up.ts";
import { down } from "./commands/down.ts";
import { status } from "./commands/status.ts";
import { approval } from "./commands/approval.ts";
import { attach, send } from "./commands/worker.ts";
import { tasks } from "./commands/tasks.ts";
import { daemonCmd } from "./commands/daemon.ts";

export async function runCli(): Promise<void> {
  const [command, ...args] = Deno.args;

  try {
    switch (command) {
      case "init":
        await initCmd(args);
        break;
      case "up":
        await up(args);
        break;
      case "down":
        await down();
        break;
      case "status":
        await status(args);
        break;
      case "approval":
        await approval(args);
        break;
      case "send": {
        const [worker, ...rest] = args;
        if (!worker || rest.length === 0) {
          console.error("Usage: jackops send <worker> <message>");
          Deno.exit(1);
        }
        await send(worker, rest.join(" "));
        break;
      }
      case "attach": {
        const [worker] = args;
        if (!worker) {
          console.error("Usage: jackops attach <worker>");
          Deno.exit(1);
        }
        await attach(worker);
        break;
      }
      case "tasks": {
        const [sub, ...rest] = args;
        await tasks(sub, rest);
        break;
      }
      case "daemon":
        await daemonCmd(args);
        break;
      case "--help":
      case "-h":
      default:
        console.log(USAGE);
        Deno.exit(!command || command === "--help" || command === "-h" ? 0 : 1);
    }
  } catch (e) {
    console.error(`Error: ${e instanceof Error ? e.message : e}`);
    Deno.exit(1);
  }
}
