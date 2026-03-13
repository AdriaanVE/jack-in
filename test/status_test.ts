import { assertEquals } from "@std/assert";
import {
  formatJsonStatus,
  formatStatus,
  type StatusInfo,
} from "../src/status.ts";
import type { Config } from "../src/config.ts";

const testConfig: Config = {
  project: "test-app",
  workers: [
    {
      name: "backend",
      agent: "claude",
      prompt: "do backend",
      role: "executor",
    },
    {
      name: "frontend",
      agent: "codex",
      prompt: "do frontend",
      role: "executor",
    },
  ],
  orchestrator: {
    poll_interval: 5000,
    max_retries: 2,
    approval: "manual",
    agent: "claude",
  },
  startup_instructions: null,
};

const defaultWorker = {
  name: "backend",
  agent: "claude",
  state: "working" as const,
  worktree: ".w-test-app-backend",
};

function makeInfo(
  overrides: Partial<StatusInfo> = {},
): StatusInfo {
  return {
    statuses: [],
    startedEpoch: null,
    daemon: { running: false },
    tasks: null,
    ...overrides,
  };
}

Deno.test("formatStatus shows no active session when empty", () => {
  const output = formatStatus(testConfig, makeInfo());
  assertEquals(output.includes("No active session"), true);
  assertEquals(output.includes("test-app"), true);
});

Deno.test("formatStatus shows worker statuses", () => {
  const statuses = [
    {
      name: "backend",
      agent: "claude",
      state: "working" as const,
      worktree: ".w-test-app-backend",
    },
    {
      name: "frontend",
      agent: "codex",
      state: "waiting" as const,
      worktree: ".w-test-app-frontend",
    },
  ];
  const output = formatStatus(testConfig, makeInfo({ statuses }));
  assertEquals(output.includes("backend"), true);
  assertEquals(output.includes("claude"), true);
  assertEquals(output.includes("working"), true);
  assertEquals(output.includes("frontend"), true);
  assertEquals(output.includes("codex"), true);
  assertEquals(output.includes("waiting"), true);
});

Deno.test("formatStatus pads columns consistently", () => {
  const statuses = [
    {
      name: "a",
      agent: "claude",
      state: "working" as const,
      worktree: ".w-test-app-a",
    },
    {
      name: "longname",
      agent: "codex",
      state: "gone" as const,
      worktree: ".w-test-app-longname",
    },
  ];
  const output = formatStatus(testConfig, makeInfo({ statuses }));
  const lines = output.split("\n").filter((l) =>
    l.includes("claude") || l.includes("codex")
  );
  const claudeIdx = lines[0].indexOf("claude");
  const codexIdx = lines[1].indexOf("codex");
  assertEquals(claudeIdx, codexIdx);
});

Deno.test("formatStatus shows session name", () => {
  const output = formatStatus(
    testConfig,
    makeInfo({ statuses: [defaultWorker] }),
  );
  assertEquals(output.includes("jackops-test-app"), true);
});

Deno.test("formatStatus shows daemon running", () => {
  const output = formatStatus(
    testConfig,
    makeInfo({ statuses: [defaultWorker], daemon: { running: true } }),
  );
  assertEquals(output.includes("Daemon:   running"), true);
});

Deno.test("formatStatus shows daemon stopped", () => {
  const output = formatStatus(
    testConfig,
    makeInfo({ statuses: [defaultWorker], daemon: { running: false } }),
  );
  assertEquals(output.includes("Daemon:   stopped"), true);
});

Deno.test("formatStatus shows approval mode with description", () => {
  const output = formatStatus(
    testConfig,
    makeInfo({ statuses: [defaultWorker] }),
  );
  assertEquals(
    output.includes("Approval: manual (manual approval required)"),
    true,
  );
});

Deno.test("formatStatus shows auto approval with model", () => {
  const autoConfig: Config = {
    ...testConfig,
    orchestrator: { ...testConfig.orchestrator, approval: "auto" },
  };
  const output = formatStatus(
    autoConfig,
    makeInfo({
      statuses: [defaultWorker],
      autoApprovalModel: "claude-sonnet-4-5",
    }),
  );
  assertEquals(
    output.includes("Approval: auto (LLM evaluates permission prompts)"),
    true,
  );
  assertEquals(output.includes("Model:    claude-sonnet-4-5"), true);
});

Deno.test("formatStatus shows yolo approval", () => {
  const yoloConfig: Config = {
    ...testConfig,
    orchestrator: { ...testConfig.orchestrator, approval: "yolo" },
  };
  const output = formatStatus(
    yoloConfig,
    makeInfo({ statuses: [defaultWorker] }),
  );
  assertEquals(
    output.includes("Approval: yolo (all prompts auto-approved)"),
    true,
  );
  assertEquals(output.includes("Model:"), false);
});

Deno.test("formatStatus shows task counts", () => {
  const tasks = { pending: 3, current: 1, review: 0, complete: 2, rejected: 0 };
  const output = formatStatus(
    testConfig,
    makeInfo({ statuses: [defaultWorker], tasks }),
  );
  assertEquals(
    output.includes(
      "Tasks: 3 pending, 1 current, 0 review, 2 complete, 0 rejected",
    ),
    true,
  );
});

Deno.test("formatStatus hides tasks when all zero", () => {
  const tasks = { pending: 0, current: 0, review: 0, complete: 0, rejected: 0 };
  const output = formatStatus(
    testConfig,
    makeInfo({ statuses: [defaultWorker], tasks }),
  );
  assertEquals(output.includes("Tasks:"), false);
});

Deno.test("formatStatus hides tasks when null", () => {
  const output = formatStatus(
    testConfig,
    makeInfo({ statuses: [defaultWorker], tasks: null }),
  );
  assertEquals(output.includes("Tasks:"), false);
});

Deno.test("formatJsonStatus returns structured output", () => {
  const tasks = { pending: 2, current: 1, review: 1, complete: 3, rejected: 0 };
  const statuses = [
    {
      name: "backend",
      agent: "claude",
      state: "working" as const,
      worktree: ".w-test-app-backend",
    },
  ];
  const result = formatJsonStatus(
    testConfig,
    makeInfo({ statuses, tasks, daemon: { running: true } }),
  );
  assertEquals(result.session, "jackops-test-app");
  assertEquals(result.daemon.running, true);
  assertEquals(result.workers.length, 1);
  assertEquals(result.workers[0].name, "backend");
  assertEquals(result.workers[0].branch, "jackops/test-app/backend");
  assertEquals(result.tasks.review, 1);
  assertEquals(result.tasks.pending, 2);
});

Deno.test("formatJsonStatus defaults tasks to zeros when null", () => {
  const result = formatJsonStatus(
    testConfig,
    makeInfo({ statuses: [defaultWorker], tasks: null }),
  );
  assertEquals(result.tasks.pending, 0);
  assertEquals(result.tasks.review, 0);
  assertEquals(result.tasks.complete, 0);
});
