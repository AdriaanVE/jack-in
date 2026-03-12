/** Filesystem-based task queue. Tasks are JSON files moved between directories. */

import { join } from "@std/path";

export type TaskState = "pending" | "current" | "complete" | "rejected";
export type TaskCounts = Record<TaskState, number>;

export const TASK_STATES: TaskState[] = [
  "pending",
  "current",
  "complete",
  "rejected",
];

export interface TaskSeed {
  summary: string;
  description?: string;
  files?: string[];
  acceptance?: string[];
  depends_on?: string[];
}

export interface Task {
  id: string;
  summary: string;
  description: string;
  files?: string[];
  acceptance?: string[];
  depends_on?: string[];
  assignee?: string;
  feedback?: string;
  createdBy?: string;
  retries: number;
}

export interface TaskEntry {
  task: Task;
  state: TaskState;
}

const TASKS_DIR = ".jackops/tasks";
let _idCounter = 0;

/** Generate a unique task ID. */
export function generateId(): string {
  return `task-${Date.now()}-${_idCounter++}`;
}

function stateDir(base: string, state: TaskState): string {
  return join(base, TASKS_DIR, state);
}

function taskPath(base: string, state: TaskState, id: string): string {
  return join(base, TASKS_DIR, state, `${id}.json`);
}

/** Create the tasks/ directory structure. */
export async function init(base: string): Promise<void> {
  for (const state of TASK_STATES) {
    await Deno.mkdir(stateDir(base, state), { recursive: true });
  }
}

/** Write a new task to pending/. */
export async function create(
  base: string,
  task: Omit<Task, "retries">,
): Promise<Task> {
  const full: Task = { ...task, retries: 0 };
  const path = taskPath(base, "pending", full.id);
  await Deno.writeTextFile(path, JSON.stringify(full, null, 2) + "\n");
  return full;
}

/** Read a task file. */
async function readTask(path: string): Promise<Task> {
  const text = await Deno.readTextFile(path);
  return JSON.parse(text) as Task;
}

/** Atomically move a task between states and optionally update fields. */
async function moveTask(
  base: string,
  id: string,
  from: TaskState,
  to: TaskState,
  update?: Partial<Task> | ((task: Task) => Partial<Task>),
): Promise<Task> {
  const src = taskPath(base, from, id);
  const dst = taskPath(base, to, id);
  const task = await readTask(src);
  const patch = typeof update === "function" ? update(task) : update;
  const updated = patch ? { ...task, ...patch } : task;
  // Write to destination first, then remove source.
  // If we crash between write and remove, we have a duplicate --
  // but that's better than losing the task.
  await Deno.writeTextFile(dst, JSON.stringify(updated, null, 2) + "\n");
  await Deno.remove(src);
  return updated;
}

/** Claim a pending task for a worker. Moves pending/ -> current/. */
export function claim(
  base: string,
  taskId: string,
  assignee: string,
): Promise<Task> {
  return moveTask(base, taskId, "pending", "current", { assignee });
}

/** Mark a current task as complete. Moves current/ -> complete/. */
export function complete(base: string, taskId: string): Promise<Task> {
  return moveTask(base, taskId, "current", "complete");
}

/** Unclaim a current task, moving it back to pending. */
export function unclaim(base: string, taskId: string): Promise<Task> {
  return moveTask(base, taskId, "current", "pending", {
    assignee: undefined,
  });
}

/** Reject a current task with feedback. Moves current/ -> rejected/. */
export function reject(
  base: string,
  taskId: string,
  feedback: string,
): Promise<Task> {
  return moveTask(base, taskId, "current", "rejected", { feedback });
}

/** Retry a rejected task. Moves rejected/ -> pending/, increments retries. */
export function retry(base: string, taskId: string): Promise<Task> {
  return moveTask(base, taskId, "rejected", "pending", (task) => ({
    retries: task.retries + 1,
    assignee: undefined,
    feedback: undefined,
  }));
}

/** List tasks in a given state (or all states). */
export async function list(
  base: string,
  state?: TaskState,
): Promise<TaskEntry[]> {
  const states = state ? [state] : TASK_STATES;
  const entries: TaskEntry[] = [];

  for (const s of states) {
    const dir = stateDir(base, s);
    try {
      for await (const entry of Deno.readDir(dir)) {
        if (!entry.isFile || !entry.name.endsWith(".json")) continue;
        const task = await readTask(join(dir, entry.name));
        entries.push({ task, state: s });
      }
    } catch (e) {
      if (e instanceof Deno.errors.NotFound) continue;
      throw e;
    }
  }

  return entries;
}

/** Get a specific task by ID, searching all states. */
export async function get(
  base: string,
  taskId: string,
): Promise<TaskEntry | null> {
  for (const s of TASK_STATES) {
    try {
      const task = await readTask(taskPath(base, s, taskId));
      return { task, state: s };
    } catch (e) {
      if (e instanceof Deno.errors.NotFound) continue;
      throw e;
    }
  }
  return null;
}

/** List pending tasks whose dependencies are all complete. */
export async function ready(base: string): Promise<Task[]> {
  const [pending, completed] = await Promise.all([
    list(base, "pending"),
    list(base, "complete"),
  ]);
  const completeIds = new Set(completed.map((e) => e.task.id));

  return pending
    .filter((e) => {
      const deps = e.task.depends_on ?? [];
      return deps.every((d) => completeIds.has(d));
    })
    .map((e) => e.task);
}

/** Count tasks by state (counts dir entries, avoids parsing JSON). */
export async function counts(
  base: string,
): Promise<TaskCounts> {
  const result: TaskCounts = {
    pending: 0,
    current: 0,
    complete: 0,
    rejected: 0,
  };
  for (const s of TASK_STATES) {
    const dir = stateDir(base, s);
    try {
      for await (const entry of Deno.readDir(dir)) {
        if (entry.isFile && entry.name.endsWith(".json")) {
          result[s]++;
        }
      }
    } catch (e) {
      if (e instanceof Deno.errors.NotFound) continue;
      throw e;
    }
  }
  return result;
}

/** Seed tasks from config, skipping duplicates by summary. Returns count seeded. */
export async function seed(base: string, tasks: TaskSeed[]): Promise<number> {
  await init(base);
  const existing = await list(base);
  const existingSummaries = new Set(existing.map((e) => e.task.summary));
  let seeded = 0;
  for (const t of tasks) {
    if (existingSummaries.has(t.summary)) continue;
    await create(base, {
      id: generateId(),
      summary: t.summary,
      description: t.description ?? t.summary,
      files: t.files,
      acceptance: t.acceptance,
      depends_on: t.depends_on,
    });
    seeded++;
  }
  return seeded;
}
