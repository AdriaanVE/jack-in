package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"time"

	"github.com/AdriaanVE/jack-in/src/domain"
)

// Token usage counters (cumulative, atomic for thread safety)
var (
	totalInputTokens  atomic.Int64
	totalOutputTokens atomic.Int64
)

// Sonnet pricing for watchdog evaluations (USD per million tokens)
const (
	sonnetInputPerMillion  = 3.0
	sonnetOutputPerMillion = 15.0
)

// GetTokenUsage returns the cumulative token usage with Sonnet pricing.
func GetTokenUsage() domain.TokenUsage {
	input := totalInputTokens.Load()
	output := totalOutputTokens.Load()
	cost := float64(input)*sonnetInputPerMillion/1_000_000 +
		float64(output)*sonnetOutputPerMillion/1_000_000
	return domain.TokenUsage{
		InputTokens:  input,
		OutputTokens: output,
		CostUSD:      cost,
	}
}

// ResetTokenUsage resets the counters to zero.
func ResetTokenUsage() {
	totalInputTokens.Store(0)
	totalOutputTokens.Store(0)
}

// Pane evaluation status constants.
const (
	PaneStatusWorking      = "working"
	PaneStatusPermission   = "permission_prompt"
	PaneStatusWaitingInput = "waiting_for_input"
	PaneStatusError        = "error"
	PaneStatusIdle         = "idle"
)

// PaneEvaluation is the structured result from the LLM pane evaluator.
type PaneEvaluation struct {
	Status            string `json:"status"`
	SafeToApprove     bool   `json:"safe_to_approve"`
	ApprovalKeystroke string `json:"approval_keystroke"`
	ResponseText      string `json:"response_text"`
	Reason            string `json:"reason"`
}

type apiConfig struct {
	baseURL  string
	apiKey   string
	model    string
	headless bool // use claude CLI instead of API
}

// getAPIConfig auto-detects the LLM provider from environment variables.
// Priority: direct Anthropic API > Azure Foundry > headless Claude Code CLI.
func getAPIConfig() apiConfig {
	model := os.Getenv("ANTHROPIC_DEFAULT_SONNET_MODEL")
	if model == "" {
		model = "claude-sonnet-4-5"
	}

	// Direct Anthropic API
	if apiKey := os.Getenv("ANTHROPIC_API_KEY"); apiKey != "" {
		return apiConfig{
			baseURL: "https://api.anthropic.com/v1/messages",
			apiKey:  apiKey,
			model:   model,
		}
	}

	// Azure Foundry
	resource := os.Getenv("ANTHROPIC_FOUNDRY_RESOURCE")
	foundryKey := os.Getenv("ANTHROPIC_FOUNDRY_API_KEY")
	if resource != "" && foundryKey != "" {
		return apiConfig{
			baseURL: fmt.Sprintf("https://%s.services.ai.azure.com/anthropic/v1/messages", resource),
			apiKey:  foundryKey,
			model:   model,
		}
	}

	// Fallback: headless Claude Code CLI
	return apiConfig{headless: true}
}

var paneEvalTool = map[string]any{
	"name":        "pane_evaluation",
	"description": "Evaluate terminal output to determine the agent's current state",
	"input_schema": map[string]any{
		"type": "object",
		"properties": map[string]any{
			"status": map[string]any{
				"type":        "string",
				"enum":        []string{"working", "permission_prompt", "waiting_for_input", "error", "idle"},
				"description": "working = agent is actively processing; permission_prompt = agent is waiting for user approval of a specific action; waiting_for_input = agent is asking a question or waiting for confirmation; error = agent hit an error; idle = agent is at prompt with nothing to do",
			},
			"safe_to_approve": map[string]any{
				"type":        "boolean",
				"description": "Only relevant when status=permission_prompt. True if the requested action is safe to auto-approve.",
			},
			"approval_keystroke": map[string]any{
				"type":        "string",
				"description": "Keystroke to send to approve the prompt (e.g., 'Enter', 'y', 'Y', '1'). Only relevant when status=permission_prompt and safe_to_approve=true",
			},
			"response_text": map[string]any{
				"type":        "string",
				"description": "Text message to send when status=waiting_for_input. Empty string when not applicable.",
			},
			"reason": map[string]any{
				"type":        "string",
				"description": "Brief explanation of the assessment",
			},
		},
		"required": []string{"status", "safe_to_approve", "approval_keystroke", "response_text", "reason"},
	},
}

var validPaneStatuses = map[string]bool{
	PaneStatusWorking: true, PaneStatusPermission: true, PaneStatusWaitingInput: true,
	PaneStatusError: true, PaneStatusIdle: true,
}

const systemPromptTemplate = `You are a security reviewer for an AI coding agent orchestrator.
You are evaluating terminal output from a %s coding agent that was assigned a task.
Determine the agent's current state and what action to take.

Statuses:
- working: agent is actively processing, no action needed
- permission_prompt: agent is waiting for approval of a specific action (tool use, file access, shell command)
- waiting_for_input: agent asked a question or is waiting for confirmation to proceed with an approach
- error: agent hit an error and is not making progress
- idle: agent is at prompt with nothing to do

AGENT-SPECIFIC PERMISSION PROMPTS:
- Claude: shows "Allow [action]? [y/n]" or tool use approval prompts
- Codex: shows "Would you like to run the following command?" with numbered options:
  "› 1. Yes, proceed (y)"
  "2. Yes, and don't ask again..."
  "3. No, and tell Codex what to do differently (esc)"
  For Codex, use approval_keystroke="y" or "1" to approve.

IMPORTANT: You can ONLY approve permission prompts or answer simple yes/no questions. You must NEVER send freeform commands or complex instructions to the agent.

For permission_prompt: evaluate safety and provide approval_keystroke if safe.
For waiting_for_input: provide a short response_text to unblock the agent.

Safety rules:
- File reads, writes, and non-destructive shell commands are SAFE to approve
- Git add, commit, and diff commands are SAFE
- Test commands (pytest, npm test, etc.) are SAFE
- Lint/format commands (ruff, eslint, prettier) are SAFE
- rm -rf, force push, git reset --hard, drop table, and similar destructive ops are UNSAFE
- Network requests to unknown/external destinations are UNSAFE
- Installing packages or running untrusted code is UNSAFE
- If uncertain, mark as unsafe`

// EvaluatePane calls the LLM to evaluate a tmux pane's content.
func EvaluatePane(paneContent, taskSummary, agentType string) (*PaneEvaluation, error) {
	cfg := getAPIConfig()

	if cfg.headless {
		return evaluatePaneHeadless(paneContent, taskSummary, agentType)
	}

	return evaluatePaneAPI(cfg, paneContent, taskSummary, agentType)
}

// evaluatePaneAPI uses the Anthropic API with tool_use for structured output.
func evaluatePaneAPI(cfg apiConfig, paneContent, taskSummary, agentType string) (*PaneEvaluation, error) {
	systemPrompt := fmt.Sprintf(systemPromptTemplate, agentType)
	userMessage := fmt.Sprintf("Task assigned: %s\n\nTerminal output (last 30 lines):\n```\n%s\n```", taskSummary, paneContent)

	body := map[string]any{
		"model":       cfg.model,
		"max_tokens":  512,
		"system":      systemPrompt,
		"messages":    []map[string]any{{"role": "user", "content": userMessage}},
		"tools":       []any{paneEvalTool},
		"tool_choice": map[string]any{"type": "tool", "name": "pane_evaluation"},
	}

	bodyJSON, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "POST", cfg.baseURL, bytes.NewReader(bodyJSON))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("x-api-key", cfg.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("API request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
		return nil, fmt.Errorf("Anthropic API %d: %s", resp.StatusCode, string(errBody))
	}

	var data struct {
		Content []struct {
			Type  string          `json:"type"`
			Input json.RawMessage `json:"input"`
		} `json:"content"`
		Usage struct {
			InputTokens  int64 `json:"input_tokens"`
			OutputTokens int64 `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	// Track token usage
	if data.Usage.InputTokens > 0 {
		totalInputTokens.Add(data.Usage.InputTokens)
	}
	if data.Usage.OutputTokens > 0 {
		totalOutputTokens.Add(data.Usage.OutputTokens)
	}

	for _, block := range data.Content {
		if block.Type == "tool_use" {
			var result PaneEvaluation
			if err := json.Unmarshal(block.Input, &result); err != nil {
				return nil, fmt.Errorf("decoding tool_use input: %w", err)
			}
			if !validPaneStatuses[result.Status] {
				result.Status = PaneStatusWorking
			}
			return &result, nil
		}
	}

	return nil, fmt.Errorf("no tool_use block in API response")
}

const headlessPromptTemplate = `You are evaluating terminal output from a %s coding agent.

Task assigned: %s

Terminal output (last 30 lines):
%s

Determine the agent's current state:
- working: agent is actively processing
- permission_prompt: agent is waiting for approval (tool use, file access, shell command)
- waiting_for_input: agent asked a question or waiting for confirmation
- error: agent hit an error
- idle: agent is at prompt with nothing to do

AGENT-SPECIFIC PERMISSION PROMPTS:
- Claude: "Allow [action]? [y/n]" or tool approval prompts
- Codex: "Would you like to run the following command?" with "› 1. Yes, proceed (y)" options. Use keystroke "y" or "1".

For permission_prompt: is it safe to approve? (file reads/writes, git add/commit/diff, test/lint commands = SAFE; rm -rf, force push, drop table = UNSAFE)

Respond with ONLY this JSON, no markdown fences or extra text:
{"status":"working|permission_prompt|waiting_for_input|error|idle","safe_to_approve":true|false,"approval_keystroke":"Enter|y|Y|1|","response_text":"","reason":"brief explanation"}`

// evaluatePaneHeadless uses Claude Code CLI as a fallback when no API key is available.
func evaluatePaneHeadless(paneContent, taskSummary, agentType string) (*PaneEvaluation, error) {
	prompt := fmt.Sprintf(headlessPromptTemplate, agentType, taskSummary, paneContent)

	// Try up to 2 times (initial + 1 retry on parse failure)
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		result, err := runHeadlessClaude(prompt)
		if err == nil {
			return result, nil
		}
		lastErr = err
	}

	return nil, fmt.Errorf("headless evaluation failed after retry: %w", lastErr)
}

func runHeadlessClaude(prompt string) (*PaneEvaluation, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "claude", "--print", "-p", prompt)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("claude CLI: %w", err)
	}

	// Extract JSON from response (may have surrounding text)
	jsonStr := extractJSON(string(out))
	if jsonStr == "" {
		return nil, fmt.Errorf("no JSON found in response: %s", string(out))
	}

	var result PaneEvaluation
	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		return nil, fmt.Errorf("parsing JSON: %w (raw: %s)", err, jsonStr)
	}

	if !validPaneStatuses[result.Status] {
		result.Status = PaneStatusWorking
	}

	return &result, nil
}

// extractJSON finds the first JSON object in a string.
// Note: This uses brace-depth counting and does not handle escaped braces
// inside JSON string values. Sufficient for Claude CLI output parsing.
func extractJSON(s string) string {
	start := strings.Index(s, "{")
	if start == -1 {
		return ""
	}
	// Find matching closing brace
	depth := 0
	for i := start; i < len(s); i++ {
		switch s[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return s[start : i+1]
			}
		}
	}
	return ""
}
