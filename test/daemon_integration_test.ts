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
