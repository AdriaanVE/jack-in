import { assertEquals } from "@std/assert";
import {
  isJackopsWorktree,
  WORKTREE_PREFIX,
  worktreeDir,
  worktreePath,
} from "../src/worktree.ts";
import type { WorktreeInfo } from "../src/worktree.ts";

Deno.test("WORKTREE_PREFIX is .w-", () => {
  assertEquals(WORKTREE_PREFIX, ".w-");
});

Deno.test("worktreeDir includes project and name", () => {
  assertEquals(worktreeDir("myapp", "backend"), ".w-myapp-backend");
});

Deno.test("worktreePath joins base with worktreeDir", () => {
  assertEquals(
    worktreePath("/home/user/repo", "myapp", "backend"),
    "/home/user/repo/.w-myapp-backend",
  );
});

Deno.test("isJackopsWorktree matches .w- prefix", () => {
  const entry: WorktreeInfo = {
    path: "/repo/.w-myapp-backend",
    branch: "refs/heads/jackops/myapp/backend",
    bare: false,
  };
  assertEquals(isJackopsWorktree(entry), true);
});

Deno.test("isJackopsWorktree rejects non-.w- paths", () => {
  const entry: WorktreeInfo = {
    path: "/repo/some-other-dir",
    branch: "refs/heads/main",
    bare: false,
  };
  assertEquals(isJackopsWorktree(entry), false);
});

Deno.test("isJackopsWorktree filters by project", () => {
  const entryA: WorktreeInfo = {
    path: "/repo/.w-projectA-worker1",
    branch: "refs/heads/jackops/projectA/worker1",
    bare: false,
  };
  const entryB: WorktreeInfo = {
    path: "/repo/.w-projectB-worker1",
    branch: "refs/heads/jackops/projectB/worker1",
    bare: false,
  };

  assertEquals(isJackopsWorktree(entryA, "projectA"), true);
  assertEquals(isJackopsWorktree(entryA, "projectB"), false);
  assertEquals(isJackopsWorktree(entryB, "projectB"), true);
  assertEquals(isJackopsWorktree(entryB, "projectA"), false);
});

Deno.test("isJackopsWorktree without project matches all .w- entries", () => {
  const entry: WorktreeInfo = {
    path: "/repo/.w-anything-here",
    branch: "",
    bare: false,
  };
  assertEquals(isJackopsWorktree(entry), true);
});

Deno.test("isJackopsWorktree handles bare worktree", () => {
  const entry: WorktreeInfo = {
    path: "/repo",
    branch: "",
    bare: true,
  };
  assertEquals(isJackopsWorktree(entry), false);
});
