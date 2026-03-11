import { assertEquals } from "@std/assert";
import * as worktree from "../src/worktree.ts";
import { exec } from "../src/subprocess.ts";

const PROJECT = "testproj";

async function makeTempGitRepo(): Promise<string> {
  const dir = await Deno.realPath(
    await Deno.makeTempDir({ prefix: "jackops-test-" }),
  );
  const run = async (args: string[]) => {
    const r = await exec("git", args, dir);
    if (!r.success) throw new Error(`git ${args[0]} failed: ${r.stderr}`);
  };
  await run(["init"]);
  await Deno.writeTextFile(`${dir}/README.md`, "test");
  await run(["add", "README.md"]);
  await run(["commit", "-m", "init"]);
  return dir;
}

async function removeTempRepo(dir: string) {
  await Deno.remove(dir, { recursive: true });
}

Deno.test({
  name: "worktree integration",
  async fn(t) {
    const repo = await makeTempGitRepo();

    try {
      await t.step("create makes a worktree", async () => {
        const wt = await worktree.create(repo, PROJECT, "w1");
        assertEquals(wt, worktree.worktreePath(repo, PROJECT, "w1"));

        // Verify directory exists
        const stat = await Deno.stat(wt);
        assertEquals(stat.isDirectory, true);

        // Verify it shows up in list
        const entries = await worktree.list(repo);
        const found = entries.find((e) => e.path === wt);
        assertEquals(found !== undefined, true);
        assertEquals(found!.branch.includes(`jackops/${PROJECT}/w1`), true);
      });

      await t.step("create handles existing branch gracefully", async () => {
        // Remove the worktree but keep the branch
        await worktree.removeByPath(
          worktree.worktreePath(repo, PROJECT, "w1"),
          repo,
        );
        // Re-create should succeed (branch already exists)
        const wt = await worktree.create(repo, PROJECT, "w1");
        const stat = await Deno.stat(wt);
        assertEquals(stat.isDirectory, true);
      });

      await t.step("create second worktree", async () => {
        const wt = await worktree.create(repo, PROJECT, "w2");
        const stat = await Deno.stat(wt);
        assertEquals(stat.isDirectory, true);
      });

      await t.step("list returns all worktrees", async () => {
        const entries = await worktree.list(repo);
        // Main worktree + w1 + w2
        assertEquals(entries.length >= 3, true);
      });

      await t.step("isJackopsWorktree filters correctly", async () => {
        const entries = await worktree.list(repo);
        const jackops = entries.filter((e) =>
          worktree.isJackopsWorktree(e, PROJECT)
        );
        assertEquals(jackops.length, 2);
      });

      await t.step("isJackopsWorktree filters by project", async () => {
        const entries = await worktree.list(repo);
        const other = entries.filter((e) =>
          worktree.isJackopsWorktree(e, "otherproject")
        );
        assertEquals(other.length, 0);
      });

      await t.step("removeByPath removes a worktree", async () => {
        const wt = worktree.worktreePath(repo, PROJECT, "w2");
        await worktree.removeByPath(wt, repo);

        // Verify directory is gone
        try {
          await Deno.stat(wt);
          throw new Error("expected NotFound");
        } catch (e) {
          assertEquals(e instanceof Deno.errors.NotFound, true);
        }
      });

      await t.step("cleanup removes all project worktrees", async () => {
        // w1 should still exist
        const removed = await worktree.cleanup(repo, PROJECT);
        assertEquals(removed.length, 1);

        const entries = await worktree.list(repo);
        const remaining = entries.filter((e) =>
          worktree.isJackopsWorktree(e, PROJECT)
        );
        assertEquals(remaining.length, 0);
      });
    } finally {
      await removeTempRepo(repo);
    }
  },
  sanitizeResources: false,
  sanitizeOps: false,
});
