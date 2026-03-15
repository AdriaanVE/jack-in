/** `jackops tasks` command handler. */

import * as tq from "../../task-queue.ts";

export async function tasks(
  subcommand: string | undefined,
  args: string[],
): Promise<void> {
  const base = Deno.cwd();

  switch (subcommand) {
    case "init": {
      await tq.init(base);
      console.log("Task queue initialized.");
      break;
    }
    case "add": {
      if (args.length === 0) {
        console.error(
          "Usage: jackops tasks add <summary> [--desc <description>]",
        );
        Deno.exit(1);
      }
      await tq.init(base); // ensure dirs exist
      const descIdx = args.indexOf("--desc");
      let summary: string;
      let description: string;
      if (descIdx >= 0) {
        summary = args.slice(0, descIdx).join(" ");
        description = args.slice(descIdx + 1).join(" ");
      } else {
        summary = args.join(" ");
        description = summary;
      }
      const id = tq.generateId();
      const task = await tq.create(base, { id, summary, description });
      console.log(`Created ${task.id}: ${task.summary}`);
      break;
    }
    case "complete": {
      const [taskId] = args;
      if (!taskId) {
        console.error("Usage: jackops tasks complete <id>");
        Deno.exit(1);
      }
      await tq.review(base, taskId);
      console.log(`Moved ${taskId} to review.`);
      break;
    }
    case "approve": {
      const [taskId] = args;
      if (!taskId) {
        console.error("Usage: jackops tasks approve <id>");
        Deno.exit(1);
      }
      await tq.approve(base, taskId);
      console.log(`Approved ${taskId}.`);
      break;
    }
    case "reject": {
      const [taskId, ...feedbackParts] = args;
      if (!taskId || feedbackParts.length === 0) {
        console.error("Usage: jackops tasks reject <id> <feedback>");
        Deno.exit(1);
      }
      const feedback = feedbackParts.join(" ");
      await tq.reject(base, taskId, feedback);
      console.log(`Rejected ${taskId}.`);
      break;
    }
    default: {
      // List tasks
      const entries = await tq.list(base);
      if (entries.length === 0) {
        console.log("No tasks. Run 'jackops tasks init' to set up the queue.");
        return;
      }
      const c: Record<string, number> = {};
      for (const e of entries) c[e.state] = (c[e.state] ?? 0) + 1;
      console.log(
        `Tasks: ${c.pending ?? 0} pending, ${c.current ?? 0} current, ${
          c.review ?? 0
        } review, ${c.complete ?? 0} complete, ${c.rejected ?? 0} rejected\n`,
      );
      for (const state of tq.TASK_STATES) {
        const stateEntries = entries.filter((e) => e.state === state);
        if (stateEntries.length === 0) continue;
        console.log(`[${state}]`);
        for (const e of stateEntries) {
          const assignee = e.task.assignee ? ` (${e.task.assignee})` : "";
          console.log(`  ${e.task.id}: ${e.task.summary}${assignee}`);
        }
      }
      break;
    }
  }
}
