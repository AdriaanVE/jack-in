import { assertEquals } from "@std/assert";
import { formatRecord, isConsoleLevel } from "../src/log.ts";
import type { LogRecord } from "@logtape/logtape";

function makeRecord(
  overrides: Partial<LogRecord> = {},
): LogRecord {
  return {
    level: "info",
    category: ["jackops", "daemon"],
    // 2025-01-15T12:00:00.000Z
    timestamp: 1736942400000,
    message: ["hello world"],
    rawMessage: "hello world",
    properties: {},
    ...overrides,
  };
}

Deno.test("formatRecord includes timestamp, level, category, message", () => {
  const output = formatRecord(makeRecord());
  assertEquals(output.includes("2025-01-15T12:00:00.000Z"), true);
  assertEquals(output.includes("[INFO ]"), true);
  assertEquals(output.includes("[jackops.daemon]"), true);
  assertEquals(output.includes("hello world"), true);
});

Deno.test("formatRecord pads short levels", () => {
  const output = formatRecord(makeRecord({ level: "debug" }));
  assertEquals(output.includes("[DEBUG]"), true);
});

Deno.test("formatRecord joins message parts", () => {
  const output = formatRecord(
    makeRecord({ message: ["task ", "abc-123", " assigned to ", "coder"] }),
  );
  assertEquals(output.includes("task abc-123 assigned to coder"), true);
});

Deno.test("formatRecord uses root category", () => {
  const output = formatRecord(makeRecord({ category: ["jackops"] }));
  assertEquals(output.includes("[jackops]"), true);
});

Deno.test("isConsoleLevel passes info and above", () => {
  assertEquals(isConsoleLevel("info"), true);
  assertEquals(isConsoleLevel("warning"), true);
  assertEquals(isConsoleLevel("error"), true);
  assertEquals(isConsoleLevel("fatal"), true);
});

Deno.test("isConsoleLevel blocks debug and trace", () => {
  assertEquals(isConsoleLevel("debug"), false);
  assertEquals(isConsoleLevel("trace"), false);
});
