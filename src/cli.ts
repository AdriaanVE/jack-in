/** JACKOPS CLI entry point. */

import { runCli } from "./cli/main.ts";

if (import.meta.main) {
  runCli();
}
