import { assertEquals } from "@std/assert";
import { join } from "@std/path";
import * as daemon from "../src/daemon.ts";

async function makeTempDir(): Promise<string> {
  return await Deno.realPath(
    await Deno.makeTempDir({ prefix: "jackops-daemon-int-" }),
  );
}

// --- stop-hook.sh ---

Deno.test("stop-hook.sh creates signal file", async () => {
  const dir = await makeTempDir();
  try {
    await daemon.initSignals(dir);
    const hookPath = join(dir, ".jackops", "stop-hook.sh");
    const signalDir = join(dir, ".jackops", "signals");
    const cmd = new Deno.Command("sh", {
      args: [hookPath, signalDir, "test-worker"],
    });
    const { success } = await cmd.output();
    assertEquals(success, true);
    const stat = await Deno.stat(join(signalDir, "test-worker.done"));
    assertEquals(stat.isFile, true);
  } finally {
    await Deno.remove(dir, { recursive: true });
  }
});

Deno.test("stop-hook.sh fails when signal dir missing", async () => {
  const dir = await makeTempDir();
  try {
    await daemon.initSignals(dir);
    const hookPath = join(dir, ".jackops", "stop-hook.sh");
    const cmd = new Deno.Command("sh", {
      args: [hookPath, "/nonexistent/path", "test-worker"],
    });
    const { success } = await cmd.output();
    // touch should fail and propagate non-zero exit
    assertEquals(success, false);
  } finally {
    await Deno.remove(dir, { recursive: true });
  }
});

// --- stop-hook.ts (transcript-aware) ---

function makeTranscript(
  messages: Array<
    { role: string; content: Array<{ type: string; text?: string }> }
  >,
): string {
  return messages
    .map((msg) => JSON.stringify({ message: msg }))
    .join("\n");
}

async function runStopHookTs(
  dir: string,
  workerName: string,
  transcriptContent: string,
  taskId: string | null,
): Promise<boolean> {
  const hookPath = join(dir, ".jackops", "stop-hook.ts");
  const signalDir = join(dir, ".jackops", "signals");
  const currentTaskDir = join(dir, ".jackops", "current-task");

  // Write transcript
  const transcriptPath = join(dir, ".jackops", "transcript.jsonl");
  await Deno.writeTextFile(transcriptPath, transcriptContent);

  // Write current-task file if taskId provided
  await Deno.mkdir(currentTaskDir, { recursive: true });
  if (taskId) {
    await Deno.writeTextFile(join(currentTaskDir, workerName), taskId);
  }

  const stdinData = JSON.stringify({ transcript_path: transcriptPath });
  const cmd = new Deno.Command("deno", {
    args: [
      "run",
      "--allow-read",
      "--allow-write",
      hookPath,
      signalDir,
      workerName,
      currentTaskDir,
    ],
    stdin: "piped",
    stdout: "null",
    stderr: "null",
  });
  const child = cmd.spawn();
  const writer = child.stdin.getWriter();
  await writer.write(new TextEncoder().encode(stdinData));
  await writer.close();
  await child.status;

  // Check if signal was created
  try {
    await Deno.stat(join(signalDir, `${workerName}.done`));
    return true;
  } catch {
    return false;
  }
}

Deno.test("stop-hook.ts signals when transcript has matching completion marker", async () => {
  const dir = await makeTempDir();
  try {
    await daemon.initSignals(dir);
    const transcript = makeTranscript([
      {
        role: "assistant",
        content: [{
          type: "text",
          text: "I've finished the task.\n\nJACKOPS_TASK_COMPLETE:task-001",
        }],
      },
    ]);
    const signaled = await runStopHookTs(dir, "w1", transcript, "task-001");
    assertEquals(signaled, true);
  } finally {
    await Deno.remove(dir, { recursive: true });
  }
});

Deno.test("stop-hook.ts does NOT signal when marker has wrong task ID", async () => {
  const dir = await makeTempDir();
  try {
    await daemon.initSignals(dir);
    const transcript = makeTranscript([
      {
        role: "assistant",
        content: [{
          type: "text",
          text: "Done.\n\nJACKOPS_TASK_COMPLETE:task-999",
        }],
      },
    ]);
    const signaled = await runStopHookTs(dir, "w1", transcript, "task-001");
    assertEquals(signaled, false);
  } finally {
    await Deno.remove(dir, { recursive: true });
  }
});

Deno.test("stop-hook.ts does NOT signal when no marker present", async () => {
  const dir = await makeTempDir();
  try {
    await daemon.initSignals(dir);
    const transcript = makeTranscript([
      {
        role: "assistant",
        content: [{ type: "text", text: "Let me check the tests next." }],
      },
    ]);
    const signaled = await runStopHookTs(dir, "w1", transcript, "task-001");
    assertEquals(signaled, false);
  } finally {
    await Deno.remove(dir, { recursive: true });
  }
});

Deno.test("stop-hook.ts does NOT signal when no current task file", async () => {
  const dir = await makeTempDir();
  try {
    await daemon.initSignals(dir);
    const transcript = makeTranscript([
      {
        role: "assistant",
        content: [{
          type: "text",
          text: "Done.\n\nJACKOPS_TASK_COMPLETE:task-001",
        }],
      },
    ]);
    const signaled = await runStopHookTs(dir, "w1", transcript, null);
    assertEquals(signaled, false);
  } finally {
    await Deno.remove(dir, { recursive: true });
  }
});

Deno.test("stop-hook.ts only checks last assistant message", async () => {
  const dir = await makeTempDir();
  try {
    await daemon.initSignals(dir);
    // Old marker in first message, no marker in last
    const transcript = makeTranscript([
      {
        role: "assistant",
        content: [{
          type: "text",
          text: "Done.\n\nJACKOPS_TASK_COMPLETE:task-001",
        }],
      },
      {
        role: "user",
        content: [{ type: "text", text: "Actually, fix the tests too." }],
      },
      {
        role: "assistant",
        content: [{ type: "text", text: "OK, fixing tests now." }],
      },
    ]);
    const signaled = await runStopHookTs(dir, "w1", transcript, "task-001");
    assertEquals(signaled, false);
  } finally {
    await Deno.remove(dir, { recursive: true });
  }
});

Deno.test("stop-hook.ts ignores tool_use-only assistant messages", async () => {
  const dir = await makeTempDir();
  try {
    await daemon.initSignals(dir);
    // Last assistant message has only tool_use, text with marker is earlier
    const transcript = makeTranscript([
      {
        role: "assistant",
        content: [{
          type: "text",
          text: "Done.\n\nJACKOPS_TASK_COMPLETE:task-001",
        }],
      },
      { role: "user", content: [{ type: "text", text: "tool result" }] },
      {
        role: "assistant",
        content: [{ type: "tool_use" }],
      },
    ]);
    const signaled = await runStopHookTs(dir, "w1", transcript, "task-001");
    // The last assistant message has no text, so extractLastAssistantText
    // returns null — no signal
    assertEquals(signaled, false);
  } finally {
    await Deno.remove(dir, { recursive: true });
  }
});

// --- signal lifecycle (end-to-end via hook) ---

Deno.test("signal lifecycle: hook creates -> hasSignal -> clear -> gone", async () => {
  const dir = await makeTempDir();
  try {
    await daemon.initSignals(dir);
    const name = "lifecycle-worker";

    // Initially no signal
    assertEquals(await daemon.hasSignal(dir, name), false);

    // Create signal via stop hook
    const hookPath = join(dir, ".jackops", "stop-hook.sh");
    const signalDir = join(dir, ".jackops", "signals");
    const cmd = new Deno.Command("sh", {
      args: [hookPath, signalDir, name],
    });
    await cmd.output();

    // Signal exists
    assertEquals(await daemon.hasSignal(dir, name), true);

    // Clear it
    await daemon.clearSignal(dir, name);
    assertEquals(await daemon.hasSignal(dir, name), false);
  } finally {
    await Deno.remove(dir, { recursive: true });
  }
});
