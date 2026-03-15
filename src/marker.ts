/** Completion marker utilities shared by daemon and stop-hook. */

export const MARKER_PREFIX = "JACKOPS_TASK_COMPLETE:";

export function completionMarker(taskId: string): string {
  return `${MARKER_PREFIX}${taskId}`;
}

/** Strip common formatting noise that LLMs wrap around markers. */
export function stripFormatting(line: string): string {
  let s = line.trim();
  // Strip markdown code fences
  if (s.startsWith("```") && s.endsWith("```")) {
    s = s.slice(3, -3).trim();
  } else if (s.startsWith("```")) {
    s = s.slice(3).trim();
  } else if (s.endsWith("```")) {
    s = s.slice(0, -3).trim();
  }
  // Strip backticks
  if (s.startsWith("`") && s.endsWith("`")) s = s.slice(1, -1).trim();
  // Strip quotes
  if (
    (s.startsWith('"') && s.endsWith('"')) ||
    (s.startsWith("'") && s.endsWith("'"))
  ) {
    s = s.slice(1, -1).trim();
  }
  // Strip trailing punctuation
  if (s.endsWith(".")) s = s.slice(0, -1).trim();
  return s;
}

/** Check if text contains the completion marker for the given task. */
export function hasCompletionMarker(text: string, taskId: string): boolean {
  const expected = `${MARKER_PREFIX}${taskId}`;
  const lines = text.split("\n");

  // Single pass: scan last 10 non-empty lines from the end.
  // Try cheap includes() first, then expensive stripFormatting() on first 5.
  let checked = 0;
  for (let i = lines.length - 1; i >= 0 && checked < 10; i--) {
    const trimmed = lines[i].trim();
    if (!trimmed) continue;
    checked++;
    if (trimmed.includes(expected)) return true;
    if (checked <= 5 && stripFormatting(trimmed) === expected) return true;
  }

  return false;
}
