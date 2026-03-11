import { assertEquals } from "@std/assert";
import { exec } from "../src/subprocess.ts";
import * as tmux from "../src/tmux.ts";
import * as worktree from "../src/worktree.ts";

const PROJECT = "e2e-test";
const SESSION = `jackops-${PROJECT}`;

// Tests the full CLI flow. Agent CLIs (claude/codex) don't need to be
// installed -- the spawned commands fail harmlessly in their tmux panes.
const TEST_CONFIG = `
project: ${PROJECT}
workers:
  - name: worker1
    agent: claude
    prompt: "test prompt 1"
  - name: worker2
    agent: codex
    prompt: "test prompt 2"
`;

async function makeTempGitRepo(): Promise<string> {
  const dir = await Deno.realPath(
    await Deno.makeTempDir({ prefix: "jackops-e2e-" }),
  );
  const run = async (args: string[]) => {
    const r = await exec("git", args, dir);
    if (!r.success) throw new Error(`git ${args[0]} failed: ${r.stderr}`);
  };
  await run(["init"]);
  await Deno.writeTextFile(`${dir}/README.md`, "test");
  await run(["add", "README.md"]);
  await run(["commit", "-m", "init"]);
  await Deno.writeTextFile(`${dir}/jackops.yaml`, TEST_CONFIG);
  return dir;
}

async function removeTempRepo(dir: string) {
  await Deno.remove(dir, { recursive: true });
}

async function cleanup() {
  if (await tmux.hasSession(SESSION)) {
    await tmux.killSession(SESSION);
  }
}

const CLI = new URL("../src/cli.ts", import.meta.url).pathname;

async function jackops(
  args: string[],
  cwd: string,
  stdin?: string,
): Promise<{ code: number; stdout: string; stderr: string }> {
  const cmd = new Deno.Command("deno", {
    args: [
      "run",
      "--allow-run",
      "--allow-read",
      "--allow-write",
      "--allow-env",
      CLI,
      ...args,
    ],
    cwd,
    stdout: "piped",
    stderr: "piped",
    stdin: stdin !== undefined ? "piped" : "null",
  });

  const proc = cmd.spawn();

  if (stdin !== undefined) {
    const writer = proc.stdin.getWriter();
    await writer.write(new TextEncoder().encode(stdin));
    await writer.close();
  }

  const output = await proc.output();
  const decoder = new TextDecoder();
  return {
    code: output.code,
    stdout: decoder.decode(output.stdout).trimEnd(),
    stderr: decoder.decode(output.stderr).trimEnd(),
  };
}

Deno.test({
  name: "e2e: full up/status/down cycle",
  async fn(t) {
    await cleanup();
    const repo = await makeTempGitRepo();

    try {
      await t.step("up creates session and worktrees", async () => {
        const result = await jackops(["up"], repo);
        assertEquals(result.code, 0, `stderr: ${result.stderr}`);
        assertEquals(result.stdout.includes("Starting swarm"), true);
        assertEquals(result.stdout.includes("worker1"), true);
        assertEquals(result.stdout.includes("worker2"), true);

        // Verify tmux session exists
        assertEquals(await tmux.hasSession(SESSION), true);

        // Verify windows were created
        const windows = await tmux.listWindows(SESSION);
        const names = windows.map((w) => w.name);
        assertEquals(names.includes("dashboard"), true);
        assertEquals(names.includes("worker1"), true);
        assertEquals(names.includes("worker2"), true);

        // Verify worktrees exist
        const entries = await worktree.list(repo);
        const jtrees = entries.filter((e) =>
          worktree.isJackopsWorktree(e, PROJECT)
        );
        assertEquals(jtrees.length, 2);
      });

      await t.step("up fails if session already exists", async () => {
        const result = await jackops(["up"], repo);
        assertEquals(result.code, 1);
        assertEquals(result.stderr.includes("already exists"), true);
      });

      await t.step("status shows workers", async () => {
        const result = await jackops(["status"], repo);
        assertEquals(result.code, 0, `stderr: ${result.stderr}`);
        assertEquals(result.stdout.includes(PROJECT), true);
        assertEquals(result.stdout.includes("worker1"), true);
        assertEquals(result.stdout.includes("worker2"), true);
      });

      await t.step("send delivers message to worker", async () => {
        const result = await jackops([
          "send",
          "worker1",
          "echo HELLO_FROM_TEST",
        ], repo);
        assertEquals(result.code, 0, `stderr: ${result.stderr}`);
        assertEquals(result.stdout.includes("Sent to worker1"), true);

        // Give time for the command to execute
        await new Promise((r) => setTimeout(r, 500));

        const output = await tmux.capturePane(`${SESSION}:worker1`, 30);
        assertEquals(output.includes("HELLO_FROM_TEST"), true);
      });

      await t.step("send fails for unknown worker", async () => {
        const result = await jackops(["send", "nonexistent", "hello"], repo);
        assertEquals(result.code, 1);
        assertEquals(result.stderr.includes("Unknown worker"), true);
      });

      await t.step("down kills session and cleans worktrees", async () => {
        const result = await jackops(["down"], repo, "y\n");
        assertEquals(result.code, 0, `stderr: ${result.stderr}`);
        assertEquals(result.stdout.includes("Killed tmux session"), true);
        assertEquals(result.stdout.includes("Removed"), true);

        // Verify session is gone
        assertEquals(await tmux.hasSession(SESSION), false);

        // Verify worktrees are gone
        const entries = await worktree.list(repo);
        const remaining = entries.filter((e) =>
          worktree.isJackopsWorktree(e, PROJECT)
        );
        assertEquals(remaining.length, 0);
      });
    } finally {
      await cleanup();
      await removeTempRepo(repo);
    }
  },
  sanitizeResources: false,
  sanitizeOps: false,
});

Deno.test({
  name: "e2e: no config file gives clear error",
  async fn() {
    const dir = await Deno.makeTempDir({ prefix: "jackops-e2e-noconfig-" });
    const result = await jackops(["status"], dir);
    assertEquals(result.code, 1);
    assertEquals(result.stderr.includes("No jackops.yaml"), true);
    await Deno.remove(dir, { recursive: true });
  },
  sanitizeResources: false,
  sanitizeOps: false,
});

Deno.test({
  name: "e2e: help shows usage",
  async fn() {
    const dir = await Deno.makeTempDir({ prefix: "jackops-e2e-help-" });
    const result = await jackops([], dir);
    assertEquals(result.code, 0);
    assertEquals(result.stdout.includes("JACKOPS"), true);
    assertEquals(result.stdout.includes("jackops up"), true);
    await Deno.remove(dir, { recursive: true });
  },
  sanitizeResources: false,
  sanitizeOps: false,
});
