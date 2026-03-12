/**
 * LLM evaluation module for permission decisions.
 * Uses Anthropic Messages API with tool_use for structured output.
 *
 * TODO: make model configurable in jackops.yaml
 * TODO: support other API providers (OpenAI, Azure OpenAI, etc.)
 */

// --- Types ---

export interface PermissionDecision {
  allow: boolean;
  reason: string;
}

export interface PaneEvaluation {
  status:
    | "working"
    | "permission_prompt"
    | "waiting_for_input"
    | "error"
    | "idle";
  safe_to_approve: boolean;
  approval_keystroke: string;
  response_text: string;
  reason: string;
}

// --- API config ---

interface ApiConfig {
  baseUrl: string;
  apiKey: string;
  model: string;
}

function getApiConfig(): ApiConfig {
  const resource = Deno.env.get("ANTHROPIC_FOUNDRY_RESOURCE");
  const apiKey = Deno.env.get("ANTHROPIC_FOUNDRY_API_KEY");
  const model = Deno.env.get("ANTHROPIC_DEFAULT_SONNET_MODEL") ??
    "claude-sonnet-4-5";

  if (!apiKey || !resource) {
    throw new Error(
      "Missing ANTHROPIC_FOUNDRY_API_KEY or ANTHROPIC_FOUNDRY_RESOURCE env vars",
    );
  }

  return {
    baseUrl: `https://${resource}.services.ai.azure.com/anthropic/v1/messages`,
    apiKey,
    model,
  };
}

// --- Tool schemas ---

const PANE_EVAL_TOOL = {
  name: "pane_evaluation",
  description:
    "Evaluate terminal output to determine the agent's current state",
  input_schema: {
    type: "object" as const,
    properties: {
      status: {
        type: "string",
        enum: [
          "working",
          "permission_prompt",
          "waiting_for_input",
          "error",
          "idle",
        ],
        description:
          "working = agent is actively processing; permission_prompt = agent is waiting for user approval of a specific action; waiting_for_input = agent is asking a question or waiting for user to confirm an approach; error = agent hit an error; idle = agent is at prompt with nothing to do",
      },
      safe_to_approve: {
        type: "boolean",
        description:
          "Only relevant when status=permission_prompt. True if the requested action is safe to auto-approve (file read/write, non-destructive shell commands). False for destructive ops (rm -rf, force push, drop table, etc.)",
      },
      approval_keystroke: {
        type: "string",
        description:
          "Keystroke to send to approve the prompt (e.g., 'Enter', 'y', 'Y', '1'). Only relevant when status=permission_prompt and safe_to_approve=true",
      },
      response_text: {
        type: "string",
        description:
          "Text message to send when status=waiting_for_input. Should be a short, direct instruction to unblock the agent (e.g., 'yes, proceed', 'go ahead'). Empty string when not applicable.",
      },
      reason: {
        type: "string",
        description: "Brief explanation of the assessment",
      },
    },
    required: [
      "status",
      "safe_to_approve",
      "approval_keystroke",
      "response_text",
      "reason",
    ],
  },
};

// --- API call ---

async function callApi(
  config: ApiConfig,
  systemPrompt: string,
  userMessage: string,
  tool: typeof PANE_EVAL_TOOL,
): Promise<Record<string, unknown>> {
  const body = {
    model: config.model,
    max_tokens: 512,
    system: systemPrompt,
    messages: [{ role: "user", content: userMessage }],
    tools: [tool],
    tool_choice: { type: "tool", name: tool.name },
  };

  const resp = await fetch(config.baseUrl, {
    method: "POST",
    headers: {
      "x-api-key": config.apiKey,
      "Content-Type": "application/json",
      "anthropic-version": "2023-06-01",
    },
    body: JSON.stringify(body),
    signal: AbortSignal.timeout(30_000),
  });

  if (!resp.ok) {
    const text = await resp.text();
    throw new Error(`Anthropic API ${resp.status}: ${text}`);
  }

  const data = await resp.json();
  const toolBlock = data.content?.find(
    (b: Record<string, unknown>) => b.type === "tool_use",
  );
  if (!toolBlock?.input) {
    throw new Error("No tool_use block in API response");
  }
  return toolBlock.input;
}

// --- Public functions ---

/**
 * Evaluate a tmux pane's content to determine if the agent is stuck
 * on a permission prompt and whether it's safe to auto-approve.
 */
export async function evaluatePane(
  paneContent: string,
  taskSummary: string,
  agentType: string,
): Promise<PaneEvaluation> {
  const config = getApiConfig();

  const systemPrompt =
    `You are a security reviewer for an AI coding agent orchestrator.
You are evaluating terminal output from a ${agentType} coding agent that was assigned a task.
Determine the agent's current state and what action to take.

Statuses:
- working: agent is actively processing, no action needed
- permission_prompt: agent is waiting for approval of a specific action (tool use, file access, shell command)
- waiting_for_input: agent asked a question or is waiting for confirmation to proceed with an approach
- error: agent hit an error
- idle: agent is at prompt with nothing to do

For permission_prompt: evaluate safety and provide approval_keystroke if safe.
For waiting_for_input: provide a short response_text to unblock the agent (e.g., "yes, proceed", "go ahead with that approach").

Safety rules:
- File reads, writes, and non-destructive shell commands are SAFE to approve
- Git add, commit, and diff commands are SAFE
- rm -rf, force push, git reset --hard, drop table, and similar destructive ops are UNSAFE
- Network requests to unknown/external destinations are UNSAFE
- Installing packages or running untrusted code is UNSAFE
- If uncertain, mark as unsafe`;

  const userMessage = `Task assigned: ${taskSummary}

Terminal output (last 30 lines):
\`\`\`
${paneContent}
\`\`\``;

  const result = await callApi(
    config,
    systemPrompt,
    userMessage,
    PANE_EVAL_TOOL,
  );

  const VALID_STATUSES = new Set([
    "working",
    "permission_prompt",
    "waiting_for_input",
    "error",
    "idle",
  ]);
  const rawStatus = result.status as string;

  return {
    status: VALID_STATUSES.has(rawStatus)
      ? (rawStatus as PaneEvaluation["status"])
      : "working",
    safe_to_approve: result.safe_to_approve === true,
    approval_keystroke: (result.approval_keystroke as string) ?? "",
    response_text: (result.response_text as string) ?? "",
    reason: (result.reason as string) ?? "",
  };
}
