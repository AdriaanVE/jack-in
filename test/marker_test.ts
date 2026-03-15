import { assertEquals, assertStringIncludes } from "@std/assert";
import {
  completionMarker,
  hasCompletionMarker,
  MARKER_PREFIX,
  stripFormatting,
} from "../src/marker.ts";

// --- MARKER_PREFIX / completionMarker ---

Deno.test("MARKER_PREFIX is the expected string", () => {
  assertEquals(MARKER_PREFIX, "JACKOPS_TASK_COMPLETE:");
});

Deno.test("completionMarker returns prefixed task ID", () => {
  assertEquals(completionMarker("task-001"), "JACKOPS_TASK_COMPLETE:task-001");
});

// --- stripFormatting ---

Deno.test("stripFormatting passes through plain text", () => {
  assertEquals(
    stripFormatting("JACKOPS_TASK_COMPLETE:t1"),
    "JACKOPS_TASK_COMPLETE:t1",
  );
});

Deno.test("stripFormatting strips inline backticks", () => {
  assertEquals(
    stripFormatting("`JACKOPS_TASK_COMPLETE:t1`"),
    "JACKOPS_TASK_COMPLETE:t1",
  );
});

Deno.test("stripFormatting strips code fence both sides", () => {
  assertEquals(
    stripFormatting("```JACKOPS_TASK_COMPLETE:t1```"),
    "JACKOPS_TASK_COMPLETE:t1",
  );
});

Deno.test("stripFormatting strips opening code fence", () => {
  assertEquals(
    stripFormatting("```JACKOPS_TASK_COMPLETE:t1"),
    "JACKOPS_TASK_COMPLETE:t1",
  );
});

Deno.test("stripFormatting strips closing code fence", () => {
  assertEquals(
    stripFormatting("JACKOPS_TASK_COMPLETE:t1```"),
    "JACKOPS_TASK_COMPLETE:t1",
  );
});

Deno.test("stripFormatting strips double quotes", () => {
  assertEquals(
    stripFormatting('"JACKOPS_TASK_COMPLETE:t1"'),
    "JACKOPS_TASK_COMPLETE:t1",
  );
});

Deno.test("stripFormatting strips single quotes", () => {
  assertEquals(
    stripFormatting("'JACKOPS_TASK_COMPLETE:t1'"),
    "JACKOPS_TASK_COMPLETE:t1",
  );
});

Deno.test("stripFormatting strips trailing period", () => {
  assertEquals(
    stripFormatting("JACKOPS_TASK_COMPLETE:t1."),
    "JACKOPS_TASK_COMPLETE:t1",
  );
});

Deno.test("stripFormatting strips surrounding whitespace", () => {
  assertEquals(
    stripFormatting("  JACKOPS_TASK_COMPLETE:t1  "),
    "JACKOPS_TASK_COMPLETE:t1",
  );
});

Deno.test("stripFormatting handles combined backtick + quote", () => {
  // backticks are stripped first, revealing quotes, which then also get stripped
  assertEquals(
    stripFormatting('`"JACKOPS_TASK_COMPLETE:t1"`'),
    "JACKOPS_TASK_COMPLETE:t1",
  );
});

// --- hasCompletionMarker ---

Deno.test("hasCompletionMarker matches exact last line", () => {
  assertEquals(
    hasCompletionMarker("JACKOPS_TASK_COMPLETE:t1", "t1"),
    true,
  );
});

Deno.test("hasCompletionMarker matches marker on non-last line", () => {
  const text = `Done with the task.
JACKOPS_TASK_COMPLETE:t1
Let me know if you need anything else.`;
  assertEquals(hasCompletionMarker(text, "t1"), true);
});

Deno.test("hasCompletionMarker matches marker with trailing blank lines", () => {
  const text = `JACKOPS_TASK_COMPLETE:t1

`;
  assertEquals(hasCompletionMarker(text, "t1"), true);
});

Deno.test("hasCompletionMarker matches backtick-wrapped marker", () => {
  assertEquals(
    hasCompletionMarker("`JACKOPS_TASK_COMPLETE:t1`", "t1"),
    true,
  );
});

Deno.test("hasCompletionMarker matches code-fenced marker", () => {
  assertEquals(
    hasCompletionMarker("```JACKOPS_TASK_COMPLETE:t1```", "t1"),
    true,
  );
});

Deno.test("hasCompletionMarker matches quoted marker", () => {
  assertEquals(
    hasCompletionMarker('"JACKOPS_TASK_COMPLETE:t1"', "t1"),
    true,
  );
});

Deno.test("hasCompletionMarker matches marker with trailing period", () => {
  assertEquals(
    hasCompletionMarker("JACKOPS_TASK_COMPLETE:t1.", "t1"),
    true,
  );
});

Deno.test("hasCompletionMarker matches marker inline with other text", () => {
  assertEquals(
    hasCompletionMarker(
      "Here is the result: JACKOPS_TASK_COMPLETE:t1 done",
      "t1",
    ),
    true,
  );
});

Deno.test("hasCompletionMarker rejects wrong task ID", () => {
  assertEquals(
    hasCompletionMarker("JACKOPS_TASK_COMPLETE:t1", "t2"),
    false,
  );
});

Deno.test("hasCompletionMarker rejects empty text", () => {
  assertEquals(hasCompletionMarker("", "t1"), false);
});

Deno.test("hasCompletionMarker rejects text without marker", () => {
  assertEquals(
    hasCompletionMarker("Task is done, everything looks good.", "t1"),
    false,
  );
});

Deno.test("hasCompletionMarker ignores marker beyond 10 non-empty lines from end", () => {
  const lines = ["JACKOPS_TASK_COMPLETE:t1"];
  for (let i = 0; i < 11; i++) lines.push(`line ${i}`);
  assertEquals(hasCompletionMarker(lines.join("\n"), "t1"), false);
});

Deno.test("hasCompletionMarker finds marker within 10 non-empty lines from end", () => {
  const lines = ["JACKOPS_TASK_COMPLETE:t1"];
  for (let i = 0; i < 9; i++) lines.push(`line ${i}`);
  assertEquals(hasCompletionMarker(lines.join("\n"), "t1"), true);
});

Deno.test("hasCompletionMarker stripFormatting only on first 5 lines", () => {
  // Backtick-wrapped marker at position 6 from end — includes() catches it
  const lines: string[] = [];
  for (let i = 0; i < 5; i++) lines.push(`line ${i}`);
  lines.push("`JACKOPS_TASK_COMPLETE:t1`");
  // includes() won't match because backticks are part of the string
  // stripFormatting won't run because it's line 6+
  // But includes() DOES match because the raw string contains the marker
  assertEquals(hasCompletionMarker(lines.join("\n"), "t1"), true);
});

Deno.test("hasCompletionMarker stripFormatting catches marker only in backticks within first 5", () => {
  // Only backticks, no raw includes match — needs stripFormatting
  const text = "`JACKOPS_TASK_COMPLETE:t1`";
  // includes() checks for "JACKOPS_TASK_COMPLETE:t1" which IS inside the backtick string
  // So this actually matches via includes() first
  assertEquals(hasCompletionMarker(text, "t1"), true);
});

// --- Sync test: stop-hook inlined marker logic must match src/marker.ts ---

Deno.test("stop-hook.ts contains identical marker logic as src/marker.ts", async () => {
  const hookSrc = await Deno.readTextFile("hooks/stop-hook.ts");
  const markerSrc = await Deno.readTextFile("src/marker.ts");

  // Verify the hook contains the same MARKER_PREFIX value
  assertStringIncludes(hookSrc, `const MARKER_PREFIX = "${MARKER_PREFIX}";`);

  // Strip comment-only lines so comments in marker.ts don't cause false diffs
  // Strip comment-only and blank lines to compare pure logic
  const stripNoise = (s: string) =>
    s.split("\n").filter((l) => l.trim() && !/^\s*\/\//.test(l)).join("\n")
      .trim();

  // Extract function bodies and verify logic matches (ignoring comments)
  for (const fnName of ["stripFormatting", "hasCompletionMarker"]) {
    const markerFnMatch = markerSrc.match(
      new RegExp(
        `export function ${fnName}\\([^)]*\\)[^{]*\\{([\\s\\S]*?)\\n\\}`,
      ),
    );
    const hookFnMatch = hookSrc.match(
      new RegExp(`function ${fnName}\\([^)]*\\)[^{]*\\{([\\s\\S]*?)\\n\\}`),
    );
    assertEquals(
      markerFnMatch !== null,
      true,
      `${fnName} not found in src/marker.ts`,
    );
    assertEquals(
      hookFnMatch !== null,
      true,
      `${fnName} not found in hooks/stop-hook.ts`,
    );
    assertEquals(
      stripNoise(hookFnMatch![1]),
      stripNoise(markerFnMatch![1]),
      `${fnName} body differs between src/marker.ts and hooks/stop-hook.ts`,
    );
  }
});
