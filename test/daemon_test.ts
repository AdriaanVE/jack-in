import { assertEquals, assertRejects, assertStringIncludes } from "@std/assert";
import { join } from "@std/path";
import * as daemon from "../src/daemon.ts";
import * as tq from "../src/task-queue.ts";

async function makeTempDir(): Promise<string> {
  return await Deno.realPath(
    await Deno.makeTempDir({ prefix: "jackops-daemon-" }),
  );
}

const TASK_FULL = {
  id: "task-001",
  summary: "Add auth module",
  description: "Implement JWT authentication",
  files: ["src/auth.ts", "src/middleware.ts"],
  acceptance: ["tokens validated", "expired tokens rejected"],
  retries: 0,
};

const TASK_MINIMAL = {
  id: "task-002",
  summary: "Fix typo",
  description: "Fix typo in README",
  retries: 0,
};

const TASK_WITH_FEEDBACK = {
  id: "task-003",
  summary: "Refactor config",
  description: "Extract config loading",
  feedback: "Missing error handling for invalid YAML",
  retries: 1,
};

// --- signalPath ---

Deno.test("signalPath returns correct path", () => {
  const p = daemon.signalPath("/project", "worker-1");
  assertEquals(p, "/project/.jackops/signals/worker-1.done");
});

// --- hasSignal / clearSignal ---

Deno.test("hasSignal returns false when no signal file", async () => {
  const dir = await makeTempDir();
  try {
    assertEquals(await daemon.hasSignal(dir, "w1"), false);
  } finally {
    await Deno.remove(dir, { recursive: true });
  }
});

Deno.test("hasSignal returns true after signal file created", async () => {
  const dir = await makeTempDir();
  try {
    const sigDir = join(dir, ".jackops", "signals");
    await Deno.mkdir(sigDir, { recursive: true });
    await Deno.writeTextFile(join(sigDir, "w1.done"), "");
    assertEquals(await daemon.hasSignal(dir, "w1"), true);
  } finally {
    await Deno.remove(dir, { recursive: true });
  }
});

Deno.test("clearSignal removes signal file", async () => {
  const dir = await makeTempDir();
  try {
    const sigDir = join(dir, ".jackops", "signals");
    await Deno.mkdir(sigDir, { recursive: true });
    await Deno.writeTextFile(join(sigDir, "w1.done"), "");
    assertEquals(await daemon.hasSignal(dir, "w1"), true);
    await daemon.clearSignal(dir, "w1");
    assertEquals(await daemon.hasSignal(dir, "w1"), false);
  } finally {
    await Deno.remove(dir, { recursive: true });
  }
});

Deno.test("clearSignal is idempotent for missing files", async () => {
  const dir = await makeTempDir();
  try {
    // No signal dir exists, should not throw (NotFound is expected)
    await daemon.clearSignal(dir, "w1");
    await daemon.clearSignal(dir, "w1");
  } finally {
    await Deno.remove(dir, { recursive: true });
  }
});

// --- formatTaskPrompt ---

Deno.test("formatTaskPrompt includes summary and description", () => {
  const result = daemon.formatTaskPrompt(TASK_MINIMAL, "w1", "claude", "/base");
  assertStringIncludes(result, "# Task: Fix typo");
  assertStringIncludes(result, "Fix typo in README");
});

Deno.test("formatTaskPrompt includes files section", () => {
  const result = daemon.formatTaskPrompt(TASK_FULL, "w1", "claude", "/base");
  assertStringIncludes(result, "## Files likely involved");
  assertStringIncludes(result, "- src/auth.ts");
  assertStringIncludes(result, "- src/middleware.ts");
});

Deno.test("formatTaskPrompt includes acceptance criteria", () => {
  const result = daemon.formatTaskPrompt(TASK_FULL, "w1", "claude", "/base");
  assertStringIncludes(result, "## Acceptance criteria");
  assertStringIncludes(result, "- tokens validated");
  assertStringIncludes(result, "- expired tokens rejected");
});

Deno.test("formatTaskPrompt includes feedback", () => {
  const result = daemon.formatTaskPrompt(
    TASK_WITH_FEEDBACK,
    "w1",
    "claude",
    "/base",
  );
  assertStringIncludes(result, "## Feedback from previous review");
  assertStringIncludes(result, "Missing error handling for invalid YAML");
});

Deno.test("formatTaskPrompt omits files section when empty", () => {
  const result = daemon.formatTaskPrompt(TASK_MINIMAL, "w1", "claude", "/base");
  assertEquals(result.includes("## Files likely involved"), false);
});

Deno.test("formatTaskPrompt omits feedback section when absent", () => {
  const result = daemon.formatTaskPrompt(TASK_FULL, "w1", "claude", "/base");
  assertEquals(result.includes("## Feedback"), false);
});

Deno.test("formatTaskPrompt adds signal instruction for non-Claude agents", () => {
  const result = daemon.formatTaskPrompt(TASK_FULL, "w1", "codex", "/base");
  assertStringIncludes(result, "IMPORTANT: When you are completely done");
  assertStringIncludes(result, "touch /base/.jackops/signals/w1.done");
});

Deno.test("formatTaskPrompt adds completion marker for Claude agents", () => {
  const result = daemon.formatTaskPrompt(TASK_FULL, "w1", "claude", "/base");
  assertStringIncludes(result, "JACKOPS_TASK_COMPLETE:task-001");
  assertStringIncludes(result, "output exactly this on its own line");
});

Deno.test("formatTaskPrompt completion marker is per-task", () => {
  const r1 = daemon.formatTaskPrompt(TASK_FULL, "w1", "claude", "/base");
  const r2 = daemon.formatTaskPrompt(TASK_MINIMAL, "w1", "claude", "/base");
  assertStringIncludes(r1, "JACKOPS_TASK_COMPLETE:task-001");
  assertStringIncludes(r2, "JACKOPS_TASK_COMPLETE:task-002");
  assertEquals(r1.includes("JACKOPS_TASK_COMPLETE:task-002"), false);
});

Deno.test("formatTaskPrompt Claude gets marker not touch instruction", () => {
  const result = daemon.formatTaskPrompt(TASK_FULL, "w1", "claude", "/base");
  assertStringIncludes(result, "JACKOPS_TASK_COMPLETE:");
  assertEquals(result.includes("touch /base/.jackops/signals"), false);
});

Deno.test("formatTaskPrompt non-Claude gets touch and marker", () => {
  const result = daemon.formatTaskPrompt(TASK_FULL, "w1", "codex", "/base");
  assertStringIncludes(result, "touch /base/.jackops/signals/w1.done");
  assertStringIncludes(result, "JACKOPS_TASK_COMPLETE:task-001");
});

// --- completionMarker ---

Deno.test("completionMarker returns prefixed task ID", () => {
  assertEquals(
    daemon.completionMarker("task-001"),
    "JACKOPS_TASK_COMPLETE:task-001",
  );
});

Deno.test("completionMarker prefix constant matches", () => {
  const marker = daemon.completionMarker("abc");
  assertEquals(marker.startsWith(daemon.COMPLETION_MARKER_PREFIX), true);
});

// --- currentTaskPath ---

Deno.test("currentTaskPath returns correct path", () => {
  const p = daemon.currentTaskPath("/project", "worker-1");
  assertEquals(p, "/project/.jackops/current-task/worker-1");
});

// --- writeCurrentTask / clearCurrentTask ---

Deno.test("writeCurrentTask writes task ID to file", async () => {
  const dir = await makeTempDir();
  try {
    await daemon.writeCurrentTask(dir, "w1", "task-123");
    const content = await Deno.readTextFile(
      daemon.currentTaskPath(dir, "w1"),
    );
    assertEquals(content, "task-123");
  } finally {
    await Deno.remove(dir, { recursive: true });
  }
});

Deno.test("writeCurrentTask overwrites previous task ID", async () => {
  const dir = await makeTempDir();
  try {
    await daemon.writeCurrentTask(dir, "w1", "task-001");
    await daemon.writeCurrentTask(dir, "w1", "task-002");
    const content = await Deno.readTextFile(
      daemon.currentTaskPath(dir, "w1"),
    );
    assertEquals(content, "task-002");
  } finally {
    await Deno.remove(dir, { recursive: true });
  }
});

Deno.test("clearCurrentTask removes task file", async () => {
  const dir = await makeTempDir();
  try {
    await daemon.writeCurrentTask(dir, "w1", "task-123");
    await daemon.clearCurrentTask(dir, "w1");
    let exists = true;
    try {
      await Deno.stat(daemon.currentTaskPath(dir, "w1"));
    } catch (e) {
      if (e instanceof Deno.errors.NotFound) exists = false;
      else throw e;
    }
    assertEquals(exists, false);
  } finally {
    await Deno.remove(dir, { recursive: true });
  }
});

Deno.test("clearCurrentTask is idempotent for missing files", async () => {
  const dir = await makeTempDir();
  try {
    await daemon.clearCurrentTask(dir, "w1");
    await daemon.clearCurrentTask(dir, "w1");
  } finally {
    await Deno.remove(dir, { recursive: true });
  }
});

// --- writeTaskPrompt ---

Deno.test("writeTaskPrompt writes prompt file to disk", async () => {
  const dir = await makeTempDir();
  try {
    const path = await daemon.writeTaskPrompt(
      dir,
      TASK_FULL,
      "w1",
      "claude",
    );
    assertEquals(path, join(dir, ".jackops/prompts/task-001.md"));
    const content = await Deno.readTextFile(path);
    assertStringIncludes(content, "# Task: Add auth module");
  } finally {
    await Deno.remove(dir, { recursive: true });
  }
});

// --- initSignals ---

Deno.test("initSignals creates signal directory, current-task dir, and copies hooks", async () => {
  const dir = await makeTempDir();
  try {
    await daemon.initSignals(dir);
    const sigDir = await Deno.stat(join(dir, ".jackops", "signals"));
    assertEquals(sigDir.isDirectory, true);
    const ctDir = await Deno.stat(join(dir, ".jackops", "current-task"));
    assertEquals(ctDir.isDirectory, true);
    const stopHookTs = await Deno.stat(join(dir, ".jackops", "stop-hook.ts"));
    assertEquals(stopHookTs.isFile, true);
    const stopHookSh = await Deno.stat(join(dir, ".jackops", "stop-hook.sh"));
    assertEquals(stopHookSh.isFile, true);
    const permEval = await Deno.stat(
      join(dir, ".jackops", "permission-eval.sh"),
    );
    assertEquals(permEval.isFile, true);
    const yoloHook = await Deno.stat(
      join(dir, ".jackops", "yolo-approve.sh"),
    );
    assertEquals(yoloHook.isFile, true);
  } finally {
    await Deno.remove(dir, { recursive: true });
  }
});

// --- writeClaudeSettings ---

Deno.test("writeClaudeSettings manual: Stop hook only, no PermissionRequest", async () => {
  const dir = await makeTempDir();
  const worktree = join(dir, "worktree");
  await Deno.mkdir(worktree, { recursive: true });
  try {
    await daemon.writeClaudeSettings(worktree, dir, "w1", "manual");
    const settings = JSON.parse(
      await Deno.readTextFile(
        join(worktree, ".claude", "settings.local.json"),
      ),
    );
    assertEquals(settings.hooks.Stop.length, 1);
    assertStringIncludes(
      settings.hooks.Stop[0].hooks[0].command,
      "stop-hook.ts",
    );
    assertStringIncludes(
      settings.hooks.Stop[0].hooks[0].command,
      "current-task",
    );
    assertEquals(settings.hooks.PermissionRequest, undefined);
  } finally {
    await Deno.remove(dir, { recursive: true });
  }
});

Deno.test("writeClaudeSettings auto: Stop + PermissionRequest with LLM eval", async () => {
  const dir = await makeTempDir();
  const worktree = join(dir, "worktree");
  await Deno.mkdir(worktree, { recursive: true });
  try {
    await daemon.writeClaudeSettings(worktree, dir, "w1", "auto");
    const settings = JSON.parse(
      await Deno.readTextFile(
        join(worktree, ".claude", "settings.local.json"),
      ),
    );
    assertEquals(settings.hooks.Stop.length, 1);
    assertEquals(settings.hooks.PermissionRequest.length, 1);
    const permCmd = settings.hooks.PermissionRequest[0].hooks[0].command;
    assertStringIncludes(permCmd, "permission-eval.sh");
    assertEquals(settings.hooks.PermissionRequest[0].hooks[0].timeout, 20);
  } finally {
    await Deno.remove(dir, { recursive: true });
  }
});

Deno.test("writeClaudeSettings yolo: Stop + PermissionRequest with yolo hook", async () => {
  const dir = await makeTempDir();
  const worktree = join(dir, "worktree");
  await Deno.mkdir(worktree, { recursive: true });
  try {
    await daemon.writeClaudeSettings(worktree, dir, "w1", "yolo");
    const settings = JSON.parse(
      await Deno.readTextFile(
        join(worktree, ".claude", "settings.local.json"),
      ),
    );
    assertEquals(settings.hooks.Stop.length, 1);
    assertEquals(settings.hooks.PermissionRequest.length, 1);
    const permCmd = settings.hooks.PermissionRequest[0].hooks[0].command;
    assertStringIncludes(permCmd, "yolo-approve.sh");
    // yolo has no timeout
    assertEquals(
      settings.hooks.PermissionRequest[0].hooks[0].timeout,
      undefined,
    );
  } finally {
    await Deno.remove(dir, { recursive: true });
  }
});

Deno.test("writeClaudeSettings defaults to manual", async () => {
  const dir = await makeTempDir();
  const worktree = join(dir, "worktree");
  await Deno.mkdir(worktree, { recursive: true });
  try {
    await daemon.writeClaudeSettings(worktree, dir, "w1");
    const settings = JSON.parse(
      await Deno.readTextFile(
        join(worktree, ".claude", "settings.local.json"),
      ),
    );
    assertEquals(settings.hooks.PermissionRequest, undefined);
  } finally {
    await Deno.remove(dir, { recursive: true });
  }
});

Deno.test("writeClaudeSettings includes jackops in allowed permissions", async () => {
  const dir = await makeTempDir();
  const worktree = join(dir, "worktree");
  await Deno.mkdir(worktree, { recursive: true });
  try {
    await daemon.writeClaudeSettings(worktree, dir, "w1");
    const settings = JSON.parse(
      await Deno.readTextFile(
        join(worktree, ".claude", "settings.local.json"),
      ),
    );
    assertEquals(Array.isArray(settings.permissions?.allow), true);
    assertEquals(settings.permissions.allow.includes("Bash(jackops *)"), true);
  } finally {
    await Deno.remove(dir, { recursive: true });
  }
});

Deno.test("writeClaudeSettings shell-escapes paths with spaces", async () => {
  const dir = await makeTempDir();
  const base = join(dir, "my project");
  const worktree = join(base, "worktree");
  await Deno.mkdir(worktree, { recursive: true });
  try {
    await daemon.writeClaudeSettings(worktree, base, "w1", "auto");
    const raw = await Deno.readTextFile(
      join(worktree, ".claude", "settings.local.json"),
    );
    const settings = JSON.parse(raw);
    const stopCmd: string = settings.hooks.Stop[0].hooks[0].command;
    assertStringIncludes(stopCmd, "'");
    assertStringIncludes(
      stopCmd,
      `'${join(base, ".jackops", "stop-hook.ts")}'`,
    );
    assertStringIncludes(stopCmd, `'${join(base, ".jackops", "signals")}'`);
    assertStringIncludes(stopCmd, "'w1'");
    assertStringIncludes(
      stopCmd,
      `'${join(base, ".jackops", "current-task")}'`,
    );
  } finally {
    await Deno.remove(dir, { recursive: true });
  }
});

// --- mergeClaudeSettings ---

Deno.test("mergeClaudeSettings creates settings when none exist", async () => {
  const dir = await makeTempDir();
  try {
    await daemon.mergeClaudeSettings(dir, dir, "orchestrator", "auto");
    const settings = JSON.parse(
      await Deno.readTextFile(join(dir, ".claude", "settings.local.json")),
    );
    assertEquals(settings.permissions.allow.includes("Bash(jackops *)"), true);
    assertEquals(settings.hooks.Stop.length, 1);
    assertEquals(settings.hooks.PermissionRequest.length, 1);
  } finally {
    await Deno.remove(dir, { recursive: true });
  }
});

Deno.test("mergeClaudeSettings preserves existing permissions", async () => {
  const dir = await makeTempDir();
  try {
    const settingsDir = join(dir, ".claude");
    await Deno.mkdir(settingsDir, { recursive: true });
    await Deno.writeTextFile(
      join(settingsDir, "settings.local.json"),
      JSON.stringify({
        permissions: { allow: ["Bash(git *)"] },
      }),
    );
    await daemon.mergeClaudeSettings(dir, dir, "orchestrator");
    const settings = JSON.parse(
      await Deno.readTextFile(join(settingsDir, "settings.local.json")),
    );
    assertEquals(settings.permissions.allow.includes("Bash(git *)"), true);
    assertEquals(settings.permissions.allow.includes("Bash(jackops *)"), true);
  } finally {
    await Deno.remove(dir, { recursive: true });
  }
});

Deno.test("mergeClaudeSettings deduplicates jackops permission", async () => {
  const dir = await makeTempDir();
  try {
    const settingsDir = join(dir, ".claude");
    await Deno.mkdir(settingsDir, { recursive: true });
    await Deno.writeTextFile(
      join(settingsDir, "settings.local.json"),
      JSON.stringify({
        permissions: { allow: ["Bash(jackops *)"] },
      }),
    );
    await daemon.mergeClaudeSettings(dir, dir, "orchestrator");
    const settings = JSON.parse(
      await Deno.readTextFile(join(settingsDir, "settings.local.json")),
    );
    const count = settings.permissions.allow.filter(
      (s: string) => s === "Bash(jackops *)",
    ).length;
    assertEquals(count, 1);
  } finally {
    await Deno.remove(dir, { recursive: true });
  }
});

Deno.test("mergeClaudeSettings preserves existing hooks", async () => {
  const dir = await makeTempDir();
  try {
    const settingsDir = join(dir, ".claude");
    await Deno.mkdir(settingsDir, { recursive: true });
    await Deno.writeTextFile(
      join(settingsDir, "settings.local.json"),
      JSON.stringify({
        hooks: {
          Stop: [{
            matcher: "*",
            hooks: [{ type: "command", command: "echo hi" }],
          }],
        },
      }),
    );
    await daemon.mergeClaudeSettings(dir, dir, "orchestrator");
    const settings = JSON.parse(
      await Deno.readTextFile(join(settingsDir, "settings.local.json")),
    );
    // Should have both the existing hook and the jackops hook
    assertEquals(settings.hooks.Stop.length, 2);
    assertStringIncludes(settings.hooks.Stop[0].hooks[0].command, "echo hi");
    assertStringIncludes(settings.hooks.Stop[1].hooks[0].command, "stop-hook");
  } finally {
    await Deno.remove(dir, { recursive: true });
  }
});

// --- formatTaskPrompt: no-confirmation instruction ---

Deno.test("formatTaskPrompt includes no-confirmation instruction for Claude", () => {
  const result = daemon.formatTaskPrompt(TASK_FULL, "w1", "claude", "/base");
  assertStringIncludes(result, "Do not ask for confirmation before proceeding");
});

Deno.test("formatTaskPrompt includes no-confirmation instruction for non-Claude", () => {
  const result = daemon.formatTaskPrompt(TASK_FULL, "w1", "codex", "/base");
  assertStringIncludes(result, "Do not ask for confirmation before proceeding");
});

// --- taskMessage ---

Deno.test("taskMessage returns inline prompt for small tasks", async () => {
  const dir = await makeTempDir();
  try {
    const msg = await daemon.taskMessage(dir, TASK_MINIMAL, "w1", "claude");
    assertStringIncludes(msg, "# Task: Fix typo");
    assertEquals(msg.includes(".jackops/prompts"), false);
  } finally {
    await Deno.remove(dir, { recursive: true });
  }
});

Deno.test("taskMessage falls back to file path for large tasks", async () => {
  const dir = await makeTempDir();
  try {
    const largeTask = {
      ...TASK_FULL,
      description: "x".repeat(daemon.MAX_SENDKEYS_BYTES),
    };
    const msg = await daemon.taskMessage(dir, largeTask, "w1", "claude");
    assertStringIncludes(msg, "Read and complete the task described in");
    assertStringIncludes(msg, ".jackops/prompts");
  } finally {
    await Deno.remove(dir, { recursive: true });
  }
});

// --- unclaim (task-queue) ---

Deno.test("unclaim moves claimed task back to pending", async () => {
  const dir = await makeTempDir();
  try {
    await tq.init(dir);
    await tq.create(dir, { id: "t1", summary: "test", description: "d" });
    await tq.claim(dir, "t1", "worker-1");
    // Task is in current
    const before = await tq.get(dir, "t1");
    assertEquals(before?.state, "current");
    assertEquals(before?.task.assignee, "worker-1");
    // Unclaim
    await tq.unclaim(dir, "t1");
    const after = await tq.get(dir, "t1");
    assertEquals(after?.state, "pending");
    assertEquals(after?.task.assignee, undefined);
  } finally {
    await Deno.remove(dir, { recursive: true });
  }
});

Deno.test("unclaim fails for task not in current", async () => {
  const dir = await makeTempDir();
  try {
    await tq.init(dir);
    await tq.create(dir, { id: "t1", summary: "test", description: "d" });
    // Task is in pending, not current
    await assertRejects(() => tq.unclaim(dir, "t1"));
  } finally {
    await Deno.remove(dir, { recursive: true });
  }
});

// --- paneContainsMarker ---

Deno.test("paneContainsMarker detects marker in pane content", () => {
  const pane = `Some output here
JACKOPS_TASK_COMPLETE:task-001
>`;
  assertEquals(daemon.paneContainsMarker(pane, "task-001"), true);
});

Deno.test("paneContainsMarker returns false for different task ID", () => {
  const pane = `Some output here
JACKOPS_TASK_COMPLETE:task-001
>`;
  assertEquals(daemon.paneContainsMarker(pane, "task-002"), false);
});

Deno.test("paneContainsMarker returns false when no marker present", () => {
  const pane = `Agent is working...
> some command output`;
  assertEquals(daemon.paneContainsMarker(pane, "task-001"), false);
});

Deno.test("paneContainsMarker detects marker surrounded by other text", () => {
  const pane = `lots of output
here is JACKOPS_TASK_COMPLETE:task-abc inline
more output`;
  assertEquals(daemon.paneContainsMarker(pane, "task-abc"), true);
});

// --- tier constants ---

Deno.test("TIER2_TIMEOUT_MS < TIER3_TIMEOUT_MS", () => {
  assertEquals(daemon.TIER2_TIMEOUT_MS < daemon.TIER3_TIMEOUT_MS, true);
});

Deno.test("MAX_LLM_EVALS is a positive integer", () => {
  assertEquals(daemon.MAX_LLM_EVALS > 0, true);
  assertEquals(Number.isInteger(daemon.MAX_LLM_EVALS), true);
});

// --- approval mode file ---

Deno.test("readApprovalMode returns null when no file exists", async () => {
  const dir = await makeTempDir();
  try {
    assertEquals(await daemon.readApprovalMode(dir), null);
  } finally {
    await Deno.remove(dir, { recursive: true });
  }
});

Deno.test("writeApprovalMode + readApprovalMode round-trip", async () => {
  const dir = await makeTempDir();
  try {
    await daemon.writeApprovalMode(dir, "yolo");
    assertEquals(await daemon.readApprovalMode(dir), "yolo");
    await daemon.writeApprovalMode(dir, "manual");
    assertEquals(await daemon.readApprovalMode(dir), "manual");
  } finally {
    await Deno.remove(dir, { recursive: true });
  }
});

Deno.test("readApprovalMode ignores invalid mode", async () => {
  const dir = await makeTempDir();
  try {
    await Deno.mkdir(join(dir, ".jackops"), { recursive: true });
    await Deno.writeTextFile(
      join(dir, daemon.APPROVAL_MODE_FILE),
      "invalid\n",
    );
    assertEquals(await daemon.readApprovalMode(dir), null);
  } finally {
    await Deno.remove(dir, { recursive: true });
  }
});

// --- mergeClaudeSettings removes stale hooks ---

Deno.test("mergeClaudeSettings removes PermissionRequest when switching to manual", async () => {
  const dir = await makeTempDir();
  try {
    // First merge with auto (adds PermissionRequest)
    await daemon.mergeClaudeSettings(dir, dir, "orchestrator", "auto");
    let settings = JSON.parse(
      await Deno.readTextFile(join(dir, ".claude", "settings.local.json")),
    );
    assertEquals(settings.hooks.PermissionRequest !== undefined, true);

    // Now merge with manual (should remove PermissionRequest)
    await daemon.mergeClaudeSettings(dir, dir, "orchestrator", "manual");
    settings = JSON.parse(
      await Deno.readTextFile(join(dir, ".claude", "settings.local.json")),
    );
    assertEquals(settings.hooks.PermissionRequest, undefined);
    // Stop hook should still be there
    assertEquals(settings.hooks.Stop.length, 1);
  } finally {
    await Deno.remove(dir, { recursive: true });
  }
});

Deno.test("mergeClaudeSettings preserves non-jackops PermissionRequest hooks", async () => {
  const dir = await makeTempDir();
  try {
    const settingsDir = join(dir, ".claude");
    await Deno.mkdir(settingsDir, { recursive: true });
    // Write settings with a user's own PermissionRequest hook + jackops auto hook
    await Deno.writeTextFile(
      join(settingsDir, "settings.local.json"),
      JSON.stringify({
        hooks: {
          PermissionRequest: [
            {
              matcher: "*",
              hooks: [{ type: "command", command: "my-custom-hook.sh" }],
            },
            {
              matcher: "*",
              hooks: [{
                type: "command",
                command: "/path/.jackops/yolo-approve.sh",
              }],
            },
          ],
        },
      }),
    );

    // Switch to manual — should remove jackops hook but keep user hook
    await daemon.mergeClaudeSettings(dir, dir, "orchestrator", "manual");
    const settings = JSON.parse(
      await Deno.readTextFile(join(settingsDir, "settings.local.json")),
    );
    assertEquals(settings.hooks.PermissionRequest.length, 1);
    assertStringIncludes(
      settings.hooks.PermissionRequest[0].hooks[0].command,
      "my-custom-hook.sh",
    );
  } finally {
    await Deno.remove(dir, { recursive: true });
  }
});
