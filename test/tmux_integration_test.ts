import { assertEquals } from "@std/assert";
import * as tmux from "../src/tmux.ts";

const TEST_SESSION = "jackops-test-tmux";

async function cleanup() {
  if (await tmux.hasSession(TEST_SESSION)) {
    await tmux.killSession(TEST_SESSION);
  }
}

Deno.test({
  name: "tmux integration",
  async fn(t) {
    await cleanup();

    try {
      await t.step(
        "hasSession returns false for nonexistent session",
        async () => {
          assertEquals(await tmux.hasSession(TEST_SESSION), false);
        },
      );

      await t.step("createSession creates a session", async () => {
        await tmux.createSession(TEST_SESSION);
        assertEquals(await tmux.hasSession(TEST_SESSION), true);
      });

      await t.step("renameWindow renames window 0", async () => {
        await tmux.renameWindow(TEST_SESSION, 0, "main");
        const windows = await tmux.listWindows(TEST_SESSION);
        assertEquals(windows[0].name, "main");
      });

      await t.step("createWindow adds a named window", async () => {
        await tmux.createWindow(TEST_SESSION, "worker1");
        const windows = await tmux.listWindows(TEST_SESSION);
        const names = windows.map((w) => w.name);
        assertEquals(names.includes("worker1"), true);
      });

      await t.step("listWindows returns all windows", async () => {
        const windows = await tmux.listWindows(TEST_SESSION);
        assertEquals(windows.length >= 2, true);
      });

      await t.step("sendKeys and capturePane round-trip", async () => {
        const target = `${TEST_SESSION}:worker1`;
        await tmux.sendKeys(target, "echo JACKOPS_TEST_MARKER");
        // Give the shell time to execute
        await new Promise((r) => setTimeout(r, 300));
        const output = await tmux.capturePane(target, 20);
        assertEquals(output.includes("JACKOPS_TEST_MARKER"), true);
      });

      await t.step("listPanes returns pane info", async () => {
        const panes = await tmux.listPanes(TEST_SESSION);
        assertEquals(panes.length >= 2, true);
        const worker = panes.find((p) => p.windowName === "worker1");
        assertEquals(worker !== undefined, true);
        assertEquals(worker!.paneDead, false);
      });

      await t.step("selectWindow switches active window", async () => {
        await tmux.selectWindow(TEST_SESSION, "main");
        const windows = await tmux.listWindows(TEST_SESSION);
        const main = windows.find((w) => w.name === "main");
        assertEquals(main?.active, true);
      });

      await t.step("killSession removes the session", async () => {
        await tmux.killSession(TEST_SESSION);
        assertEquals(await tmux.hasSession(TEST_SESSION), false);
      });
    } finally {
      await cleanup();
    }
  },
  sanitizeResources: false,
  sanitizeOps: false,
});
