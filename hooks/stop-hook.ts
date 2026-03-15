#!/usr/bin/env -S deno run --allow-read --allow-write
/**
 * Claude Code Stop hook -- transcript-aware completion detection.
 *
 * Reads the session transcript from stdin JSON and checks whether the
 * last assistant message ends with JACKOPS_TASK_COMPLETE:<taskId>.
 * Only touches the signal file when the marker matches the current task.
 *
 * Usage (set by writeClaudeSettings):
 *   echo '{"transcript_path":"/path/to/transcript.jsonl",...}' |
 *     stop-hook.ts <signal_dir> <worker_name> <current_task_dir>
 *
 * The current task ID is read from <current_task_dir>/<worker_name>.
 */

// --- Marker detection (inlined — this hook is copied to target projects) ---

const MARKER_PREFIX = "JACKOPS_TASK_COMPLETE:";

function stripFormatting(line: string): string {
  let s = line.trim();
  if (s.startsWith("```") && s.endsWith("```")) {
    s = s.slice(3, -3).trim();
  } else if (s.startsWith("```")) {
    s = s.slice(3).trim();
  } else if (s.endsWith("```")) {
    s = s.slice(0, -3).trim();
  }
  if (s.startsWith("`") && s.endsWith("`")) s = s.slice(1, -1).trim();
  if (
    (s.startsWith('"') && s.endsWith('"')) ||
    (s.startsWith("'") && s.endsWith("'"))
  ) {
    s = s.slice(1, -1).trim();
  }
  if (s.endsWith(".")) s = s.slice(0, -1).trim();
  return s;
}

function hasCompletionMarker(text: string, taskId: string): boolean {
  const expected = `${MARKER_PREFIX}${taskId}`;
  const lines = text.split("\n");
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

// --- Read stdin JSON ---

async function readStdin(): Promise<string> {
  const chunks: Uint8Array[] = [];
  for await (const chunk of Deno.stdin.readable) {
    chunks.push(chunk);
  }
  let totalLen = 0;
  for (const c of chunks) totalLen += c.length;
  const merged = new Uint8Array(totalLen);
  let offset = 0;
  for (const c of chunks) {
    merged.set(c, offset);
    offset += c.length;
  }
  return new TextDecoder().decode(merged);
}

// --- Parse last assistant text from transcript JSONL ---

interface TranscriptEntry {
  message?: {
    role?: string;
    content?: Array<{ type: string; text?: string }>;
  };
}

function extractLastAssistantText(lines: string[]): string | null {
  // Scan backwards to find the last assistant message
  for (let i = lines.length - 1; i >= 0; i--) {
    const line = lines[i].trim();
    if (!line) continue;
    let entry: TranscriptEntry;
    try {
      entry = JSON.parse(line);
    } catch {
      continue;
    }
    const msg = entry.message;
    if (!msg || msg.role !== "assistant") continue;

    // Found the last assistant message — extract text parts
    const content = msg.content;
    if (!Array.isArray(content)) return null;
    const textParts = content
      .filter((c) => c.type === "text" && c.text)
      .map((c) => c.text!);
    return textParts.length > 0 ? textParts.join("\n") : null;
  }
  return null;
}

// --- Main ---

async function main(): Promise<void> {
  const [signalDir, workerName, currentTaskDir] = Deno.args;
  if (!signalDir || !workerName || !currentTaskDir) {
    // Missing args — fall through silently (hook should not block Claude)
    return;
  }

  // Read current task ID from file
  let taskId: string;
  try {
    taskId = (
      await Deno.readTextFile(`${currentTaskDir}/${workerName}`)
    ).trim();
  } catch {
    // No current task file — worker is idle, nothing to signal
    return;
  }
  if (!taskId) return;

  // Read stdin for hook event data
  let stdinData: string;
  try {
    stdinData = await readStdin();
  } catch {
    return;
  }

  let transcriptPath: string;
  try {
    const event = JSON.parse(stdinData);
    transcriptPath = event.transcript_path;
  } catch {
    return;
  }
  if (!transcriptPath) return;

  // Read last portion of transcript (avoid reading huge files)
  let transcriptContent: string;
  try {
    transcriptContent = await Deno.readTextFile(transcriptPath);
  } catch {
    return;
  }

  // Only check the last 100 lines for performance
  const allLines = transcriptContent.split("\n");
  const tailLines = allLines.slice(-100);

  const lastText = extractLastAssistantText(tailLines);
  if (!lastText) return;

  if (hasCompletionMarker(lastText, taskId)) {
    // Task is complete — touch signal file
    const signalPath = `${signalDir}/${workerName}.done`;
    await Deno.writeTextFile(signalPath, "");
  }
}

main().catch(() => {
  // Never block Claude — swallow all errors
});
