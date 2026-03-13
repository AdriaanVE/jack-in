import { assertEquals } from "@std/assert";
import * as tq from "../src/task-queue.ts";

async function makeTempDir(): Promise<string> {
  return await Deno.realPath(
    await Deno.makeTempDir({ prefix: "jackops-tq-" }),
  );
}

const TASK_A = {
  id: "task-001",
  summary: "Add auth module",
  description: "Implement JWT authentication",
  files: ["src/auth.ts"],
  acceptance: ["tokens are validated", "expired tokens rejected"],
  createdBy: "planner",
};

const TASK_B = {
  id: "task-002",
  summary: "Add tests",
  description: "Write tests for auth module",
  depends_on: ["task-001"],
  createdBy: "planner",
};

Deno.test("init creates task directories", async () => {
  const dir = await makeTempDir();
  try {
    await tq.init(dir);
    for (const state of tq.TASK_STATES) {
      const stat = await Deno.stat(`${dir}/.jackops/tasks/${state}`);
      assertEquals(stat.isDirectory, true);
    }
  } finally {
    await Deno.remove(dir, { recursive: true });
  }
});

Deno.test("init is idempotent", async () => {
  const dir = await makeTempDir();
  try {
    await tq.init(dir);
    await tq.init(dir);
    for (const state of tq.TASK_STATES) {
      const stat = await Deno.stat(`${dir}/.jackops/tasks/${state}`);
      assertEquals(stat.isDirectory, true);
    }
  } finally {
    await Deno.remove(dir, { recursive: true });
  }
});

Deno.test("create writes task to pending", async () => {
  const dir = await makeTempDir();
  try {
    await tq.init(dir);
    const task = await tq.create(dir, TASK_A);
    assertEquals(task.id, "task-001");
    assertEquals(task.retries, 0);

    const entry = await tq.get(dir, "task-001");
    assertEquals(entry !== null, true);
    assertEquals(entry!.state, "pending");
    assertEquals(entry!.task.summary, "Add auth module");
    assertEquals(entry!.task.files, ["src/auth.ts"]);
  } finally {
    await Deno.remove(dir, { recursive: true });
  }
});

Deno.test("claim moves task to current with assignee", async () => {
  const dir = await makeTempDir();
  try {
    await tq.init(dir);
    await tq.create(dir, TASK_A);

    const task = await tq.claim(dir, "task-001", "impl-1");
    assertEquals(task.assignee, "impl-1");

    const entry = await tq.get(dir, "task-001");
    assertEquals(entry!.state, "current");
    assertEquals(entry!.task.assignee, "impl-1");
  } finally {
    await Deno.remove(dir, { recursive: true });
  }
});

Deno.test("complete moves task to complete", async () => {
  const dir = await makeTempDir();
  try {
    await tq.init(dir);
    await tq.create(dir, TASK_A);
    await tq.claim(dir, "task-001", "impl-1");

    await tq.complete(dir, "task-001");

    const entry = await tq.get(dir, "task-001");
    assertEquals(entry!.state, "complete");
    assertEquals(entry!.task.assignee, "impl-1");
  } finally {
    await Deno.remove(dir, { recursive: true });
  }
});

Deno.test("review moves current task to review", async () => {
  const dir = await makeTempDir();
  try {
    await tq.init(dir);
    await tq.create(dir, TASK_A);
    await tq.claim(dir, "task-001", "impl-1");

    await tq.review(dir, "task-001");

    const entry = await tq.get(dir, "task-001");
    assertEquals(entry!.state, "review");
    assertEquals(entry!.task.assignee, "impl-1");
  } finally {
    await Deno.remove(dir, { recursive: true });
  }
});

Deno.test("approve moves reviewed task to complete", async () => {
  const dir = await makeTempDir();
  try {
    await tq.init(dir);
    await tq.create(dir, TASK_A);
    await tq.claim(dir, "task-001", "impl-1");
    await tq.review(dir, "task-001");

    await tq.approve(dir, "task-001");

    const entry = await tq.get(dir, "task-001");
    assertEquals(entry!.state, "complete");
    assertEquals(entry!.task.assignee, "impl-1");
  } finally {
    await Deno.remove(dir, { recursive: true });
  }
});

Deno.test("reject moves reviewed task to rejected with feedback", async () => {
  const dir = await makeTempDir();
  try {
    await tq.init(dir);
    await tq.create(dir, TASK_A);
    await tq.claim(dir, "task-001", "impl-1");
    await tq.review(dir, "task-001");

    const task = await tq.reject(dir, "task-001", "missing error handling");

    assertEquals(task.feedback, "missing error handling");

    const entry = await tq.get(dir, "task-001");
    assertEquals(entry!.state, "rejected");
    assertEquals(entry!.task.feedback, "missing error handling");
  } finally {
    await Deno.remove(dir, { recursive: true });
  }
});

Deno.test("retry moves rejected task back to pending", async () => {
  const dir = await makeTempDir();
  try {
    await tq.init(dir);
    await tq.create(dir, TASK_A);
    await tq.claim(dir, "task-001", "impl-1");
    await tq.review(dir, "task-001");
    await tq.reject(dir, "task-001", "needs work");

    const task = await tq.retry(dir, "task-001");
    assertEquals(task.retries, 1);
    assertEquals(task.assignee, undefined);
    assertEquals(task.feedback, undefined);

    const entry = await tq.get(dir, "task-001");
    assertEquals(entry!.state, "pending");
  } finally {
    await Deno.remove(dir, { recursive: true });
  }
});

Deno.test("list returns all tasks across states", async () => {
  const dir = await makeTempDir();
  try {
    await tq.init(dir);
    await tq.create(dir, TASK_A);
    await tq.create(dir, TASK_B);
    await tq.claim(dir, "task-001", "impl-1");

    const all = await tq.list(dir);
    assertEquals(all.length, 2);

    const pending = await tq.list(dir, "pending");
    assertEquals(pending.length, 1);
    assertEquals(pending[0].task.id, "task-002");

    const current = await tq.list(dir, "current");
    assertEquals(current.length, 1);
    assertEquals(current[0].task.id, "task-001");
  } finally {
    await Deno.remove(dir, { recursive: true });
  }
});

Deno.test("list returns empty for uninitialized dir", async () => {
  const dir = await makeTempDir();
  try {
    const all = await tq.list(dir);
    assertEquals(all.length, 0);
  } finally {
    await Deno.remove(dir, { recursive: true });
  }
});

Deno.test("get returns null for nonexistent task", async () => {
  const dir = await makeTempDir();
  try {
    await tq.init(dir);
    const entry = await tq.get(dir, "nonexistent");
    assertEquals(entry, null);
  } finally {
    await Deno.remove(dir, { recursive: true });
  }
});

Deno.test("counts returns task counts by state", async () => {
  const dir = await makeTempDir();
  try {
    await tq.init(dir);
    await tq.create(dir, TASK_A);
    await tq.create(dir, TASK_B);
    await tq.claim(dir, "task-001", "impl-1");

    const c = await tq.counts(dir);
    assertEquals(c.pending, 1);
    assertEquals(c.current, 1);
    assertEquals(c.review, 0);
    assertEquals(c.complete, 0);
    assertEquals(c.rejected, 0);
  } finally {
    await Deno.remove(dir, { recursive: true });
  }
});

Deno.test("ready filters by dependency completion", async () => {
  const dir = await makeTempDir();
  try {
    await tq.init(dir);
    await tq.create(dir, TASK_A);
    await tq.create(dir, TASK_B); // depends on task-001

    // task-001 is pending, so task-002 is not ready
    let readyTasks = await tq.ready(dir);
    assertEquals(readyTasks.length, 1);
    assertEquals(readyTasks[0].id, "task-001");

    // Complete task-001
    await tq.claim(dir, "task-001", "impl-1");
    await tq.complete(dir, "task-001");

    // Now task-002 is ready
    readyTasks = await tq.ready(dir);
    assertEquals(readyTasks.length, 1);
    assertEquals(readyTasks[0].id, "task-002");
  } finally {
    await Deno.remove(dir, { recursive: true });
  }
});

Deno.test("ready returns tasks with no dependencies", async () => {
  const dir = await makeTempDir();
  try {
    await tq.init(dir);
    await tq.create(dir, TASK_A); // no depends_on

    const readyTasks = await tq.ready(dir);
    assertEquals(readyTasks.length, 1);
    assertEquals(readyTasks[0].id, "task-001");
  } finally {
    await Deno.remove(dir, { recursive: true });
  }
});

Deno.test("full lifecycle: create -> claim -> review -> reject -> retry -> claim -> review -> approve", async () => {
  const dir = await makeTempDir();
  try {
    await tq.init(dir);
    await tq.create(dir, TASK_A);

    // First attempt
    await tq.claim(dir, "task-001", "impl-1");
    await tq.review(dir, "task-001");
    await tq.reject(dir, "task-001", "missing tests");

    // Retry
    const retried = await tq.retry(dir, "task-001");
    assertEquals(retried.retries, 1);

    // Second attempt
    await tq.claim(dir, "task-001", "impl-2");
    await tq.review(dir, "task-001");
    await tq.approve(dir, "task-001");

    const entry = await tq.get(dir, "task-001");
    assertEquals(entry!.state, "complete");
    assertEquals(entry!.task.assignee, "impl-2");
    assertEquals(entry!.task.retries, 1);
  } finally {
    await Deno.remove(dir, { recursive: true });
  }
});
