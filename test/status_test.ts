import { assertEquals } from "@std/assert";
import { formatStatus } from "../src/status.ts";
import type { Config } from "../src/config.ts";

const testConfig: Config = {
  project: "test-app",
  workers: [
    { name: "backend", agent: "claude", prompt: "do backend" },
    { name: "frontend", agent: "codex", prompt: "do frontend" },
  ],
};

Deno.test("formatStatus shows no active session when empty", () => {
  const output = formatStatus(testConfig, []);
  assertEquals(output.includes("No active session"), true);
  assertEquals(output.includes("test-app"), true);
});

Deno.test("formatStatus shows worker statuses", () => {
  const statuses = [
    {
      name: "backend",
      agent: "claude",
      state: "running" as const,
      worktree: ".w-test-app-backend",
    },
    {
      name: "frontend",
      agent: "codex",
      state: "idle" as const,
      worktree: ".w-test-app-frontend",
    },
  ];
  const output = formatStatus(testConfig, statuses);
  assertEquals(output.includes("backend"), true);
  assertEquals(output.includes("claude"), true);
  assertEquals(output.includes("running"), true);
  assertEquals(output.includes("frontend"), true);
  assertEquals(output.includes("codex"), true);
  assertEquals(output.includes("idle"), true);
});

Deno.test("formatStatus pads columns consistently", () => {
  const statuses = [
    {
      name: "a",
      agent: "claude",
      state: "running" as const,
      worktree: ".w-test-app-a",
    },
    {
      name: "longname",
      agent: "codex",
      state: "gone" as const,
      worktree: ".w-test-app-longname",
    },
  ];
  const output = formatStatus(testConfig, statuses);
  const lines = output.split("\n").filter((l) =>
    l.includes("claude") || l.includes("codex")
  );
  // Both agent names should start at the same column
  const claudeIdx = lines[0].indexOf("claude");
  const codexIdx = lines[1].indexOf("codex");
  assertEquals(claudeIdx, codexIdx);
});
