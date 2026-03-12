/** LogTape-based logging: console for user-facing output, disk for debug. */

import { join } from "@std/path";
import {
  configure,
  getConsoleSink,
  getLogger,
  type LogRecord,
  type Sink,
} from "@logtape/logtape";

const LOG_FILE = "daemon.log";
const FLUSH_INTERVAL_MS = 2000;

const INFO_LEVELS = new Set(["info", "warning", "error", "fatal"]);

export function isConsoleLevel(level: string): boolean {
  return INFO_LEVELS.has(level);
}

export function formatRecord(record: LogRecord): string {
  const ts = new Date(record.timestamp).toISOString();
  const level = record.level.toUpperCase().padEnd(5);
  const cat = record.category.join(".");
  const parts: string[] = [];
  for (let i = 0; i < record.message.length; i++) {
    parts.push(String(record.message[i]));
  }
  return `${ts} [${level}] [${cat}] ${parts.join("")}`;
}

/** Buffered file sink — flushes every FLUSH_INTERVAL_MS or on close. */
function fileSink(logPath: string): Sink & { close: () => void } {
  const file = Deno.openSync(logPath, {
    write: true,
    create: true,
    append: true,
  });
  const encoder = new TextEncoder();
  let buffer = "";
  let timer: ReturnType<typeof setInterval> | null = null;

  function flush() {
    if (buffer.length === 0) return;
    file.writeSync(encoder.encode(buffer));
    buffer = "";
  }

  timer = setInterval(flush, FLUSH_INTERVAL_MS);

  const sink: Sink & { close: () => void } = (record: LogRecord) => {
    buffer += formatRecord(record) + "\n";
  };
  sink.close = () => {
    if (timer) clearInterval(timer);
    flush();
    file.close();
  };
  return sink;
}

/** Configure LogTape for the daemon: console (info+) and file (debug+). */
export async function setupLogging(base: string): Promise<() => void> {
  const dir = join(base, ".jackops");
  await Deno.mkdir(dir, { recursive: true });
  const logPath = join(dir, LOG_FILE);
  const sink = fileSink(logPath);

  const consoleSink = getConsoleSink();
  const filteredConsole: Sink = (record) => {
    if (isConsoleLevel(record.level)) consoleSink(record);
  };

  await configure({
    sinks: {
      console: filteredConsole,
      file: sink,
    },
    loggers: [
      {
        category: ["jackops"],
        lowestLevel: "debug",
        sinks: ["console", "file"],
      },
    ],
  });

  return () => sink.close();
}

/** Get a logger for a jackops subcategory. */
export function getJackopsLogger(subcategory?: string) {
  return subcategory
    ? getLogger(["jackops", subcategory])
    : getLogger(["jackops"]);
}
