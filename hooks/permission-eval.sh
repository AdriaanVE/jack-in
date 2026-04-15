#!/bin/bash
# Claude Code PermissionRequest hook; calls an LLM to evaluate safety.
# Safe actions are auto-approved. Unsafe actions fall through to user prompt.

# Source credentials if env file exists (optional -- env vars may already be set)
# Set JACKIN_ENV_FILE to point to your credentials file, or pre-export the
# ANTHROPIC_FOUNDRY_RESOURCE and ANTHROPIC_FOUNDRY_API_KEY environment variables.
if [ -n "${JACKIN_ENV_FILE}" ] && [ -f "${JACKIN_ENV_FILE}" ]; then
  source "${JACKIN_ENV_FILE}" >&2
fi

INPUT=$(cat)

TOOL_NAME=$(echo "$INPUT" | jq -r '.tool_name // "unknown"')
TOOL_INPUT=$(echo "$INPUT" | jq -c '.tool_input // {}')

RESOURCE="${ANTHROPIC_FOUNDRY_RESOURCE}"
API_KEY="${ANTHROPIC_FOUNDRY_API_KEY}"
MODEL="${ANTHROPIC_DEFAULT_SONNET_MODEL:-claude-sonnet-4-5}"

# Fail-open: if no credentials, let the normal dialog show
if [ -z "$API_KEY" ] || [ -z "$RESOURCE" ]; then
  exit 0
fi

BASE_URL="https://${RESOURCE}.services.ai.azure.com/anthropic/v1/messages"

PROMPT="You are a security reviewer for a coding assistant. Evaluate whether this tool call is safe to execute automatically.

Tool: ${TOOL_NAME}
Input: ${TOOL_INPUT}

Consider:
1. Is this a destructive operation (rm -rf, force push, drop table, git reset --hard, etc.)?
2. Does it access or modify sensitive files (.env, credentials, private keys, secrets)?
3. Does it make network requests to untrusted or external destinations?
4. Could it cause data loss or irreversible damage?
5. Does it install packages or run untrusted code?

Respond ONLY with a JSON object, no other text:
- If safe: {\"ok\": true}
- If dangerous: {\"ok\": false, \"reason\": \"brief explanation of the risk\"}"

RESPONSE=$(curl -s --max-time 15 "${BASE_URL}" \
  -H "x-api-key: ${API_KEY}" \
  -H "Content-Type: application/json" \
  -H "anthropic-version: 2023-06-01" \
  -d "$(jq -n \
    --arg model "$MODEL" \
    --arg prompt "$PROMPT" \
    '{
      model: $model,
      max_tokens: 256,
      messages: [{ role: "user", content: $prompt }]
    }')")

# Extract text from Anthropic Messages API response
TEXT=$(echo "$RESPONSE" | jq -r '.content[0].text // empty' 2>/dev/null)

# If API call failed, fail-open
if [ -z "$TEXT" ]; then
  exit 0
fi

# Parse model decision -- handle both raw JSON and markdown-wrapped JSON
OK=$(echo "$TEXT" | jq -r '.ok // empty' 2>/dev/null)
if [ -z "$OK" ]; then
  EXTRACTED=$(echo "$TEXT" | sed -n 's/.*```json\s*//;s/```.*//;p' | jq -r '.ok // empty' 2>/dev/null)
  OK="${EXTRACTED}"
fi

if [ "$OK" = "true" ]; then
  # Safe -> auto-approve
  jq -n '{
    hookSpecificOutput: {
      hookEventName: "PermissionRequest",
      decision: {
        behavior: "allow"
      }
    }
  }'
else
  # Unsafe -> warn user, let normal permission dialog show
  REASON=$(echo "$TEXT" | jq -r '.reason // "Model flagged this as potentially unsafe"' 2>/dev/null)
  jq -n --arg reason "$REASON" '{
    systemMessage: ("Permission reviewer: " + $reason)
  }'
fi
