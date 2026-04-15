// Package usage tracks token consumption from AI agent sessions.
package usage

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/AdriaanVE/jack-in/src/domain"
	"github.com/AdriaanVE/jack-in/src/worktree"
)

// Anthropic API pricing (USD per million tokens) - Opus 4.6
// https://www.anthropic.com/pricing
const (
	PriceInputPerMillion      = 15.0  // $15 per 1M input tokens
	PriceOutputPerMillion     = 75.0  // $75 per 1M output tokens
	PriceCacheWritePerMillion = 18.75 // 25% more than input ($15 * 1.25)
	PriceCacheReadPerMillion  = 1.50  // 90% discount ($15 * 0.10)
)

// ClaudeUsage holds token counts from Claude Code sessions.
type ClaudeUsage struct {
	InputTokens              int64 `json:"input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
}

// Total returns the sum of all token types.
func (u ClaudeUsage) Total() int64 {
	return u.InputTokens + u.OutputTokens + u.CacheCreationInputTokens
}

// CostUSD calculates the estimated cost in USD based on Anthropic API pricing.
func (u ClaudeUsage) CostUSD() float64 {
	inputCost := float64(u.InputTokens) * PriceInputPerMillion / 1_000_000
	outputCost := float64(u.OutputTokens) * PriceOutputPerMillion / 1_000_000
	cacheWriteCost := float64(u.CacheCreationInputTokens) * PriceCacheWritePerMillion / 1_000_000
	cacheReadCost := float64(u.CacheReadInputTokens) * PriceCacheReadPerMillion / 1_000_000
	return inputCost + outputCost + cacheWriteCost + cacheReadCost
}

// ToTokenUsage converts to the domain type.
func (u ClaudeUsage) ToTokenUsage() domain.TokenUsage {
	return domain.TokenUsage{
		InputTokens:  u.InputTokens + u.CacheCreationInputTokens + u.CacheReadInputTokens,
		OutputTokens: u.OutputTokens,
		CostUSD:      u.CostUSD(),
	}
}

// Tracker caches and aggregates token usage across workers and orchestrator.
type Tracker struct {
	mu           sync.RWMutex
	projectRoot  string
	projectName  string
	workers      map[string]ClaudeUsage // worker name -> usage
	orchestrator ClaudeUsage
	lastUpdate   time.Time
	cacheTTL     time.Duration
	startTime    time.Time // only count sessions after this time
}

// NewTracker creates a usage tracker for a project.
// startTime determines which session files to include (files modified after startTime minus grace period).
// Pass the tmux session creation time to include all sessions from the current run.
func NewTracker(projectRoot, projectName string, startTime time.Time) *Tracker {
	return &Tracker{
		projectRoot: projectRoot,
		projectName: projectName,
		workers:     make(map[string]ClaudeUsage),
		cacheTTL:    5 * time.Second, // refresh every 5s for responsive TUI
		startTime:   startTime,
	}
}

// Update refreshes usage data from Claude session files.
// Safe to call frequently; respects cache TTL.
func (t *Tracker) Update(workerNames []string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if time.Since(t.lastUpdate) < t.cacheTTL {
		return
	}

	// Parse orchestrator (main project dir)
	orchPath := claudeProjectPath(t.projectRoot)
	t.orchestrator = parseClaudeSessions(orchPath, t.startTime)

	// Parse each worker (worktree dirs are inside project root)
	for _, name := range workerNames {
		wtPath := filepath.Join(t.projectRoot, worktree.DirName(t.projectName, name))
		sessPath := claudeProjectPath(wtPath)
		t.workers[name] = parseClaudeSessions(sessPath, t.startTime)
	}

	t.lastUpdate = time.Now()
}

// GetWorkerUsage returns token usage for a specific worker.
func (t *Tracker) GetWorkerUsage(name string) ClaudeUsage {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.workers[name]
}

// GetOrchestratorUsage returns token usage for the orchestrator.
func (t *Tracker) GetOrchestratorUsage() ClaudeUsage {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.orchestrator
}

// GetTotalUsage returns aggregate usage across all tracked sessions.
func (t *Tracker) GetTotalUsage() ClaudeUsage {
	t.mu.RLock()
	defer t.mu.RUnlock()

	total := t.orchestrator
	for _, u := range t.workers {
		total.InputTokens += u.InputTokens
		total.OutputTokens += u.OutputTokens
		total.CacheCreationInputTokens += u.CacheCreationInputTokens
		total.CacheReadInputTokens += u.CacheReadInputTokens
	}
	return total
}

// claudeProjectPath converts a filesystem path to Claude's project directory.
// Claude stores sessions in ~/.claude/projects/<encoded-path>/
// where the path has / and . replaced with -
func claudeProjectPath(fsPath string) string {
	// Get absolute path
	absPath, err := filepath.Abs(fsPath)
	if err != nil {
		return ""
	}

	// Claude encodes paths by replacing / and . with -
	// The leading slash becomes a leading dash (kept, not stripped)
	encoded := strings.ReplaceAll(absPath, "/", "-")
	encoded = strings.ReplaceAll(encoded, ".", "-")

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	return filepath.Join(homeDir, ".claude", "projects", encoded)
}

// parseClaudeSessions reads JSONL files in a Claude project directory
// modified recently (within 20 seconds before minTime) and sums up token usage.
// The 20-second grace period covers typical startup time (animation + prompts).
func parseClaudeSessions(dir string, minTime time.Time) ClaudeUsage {
	var total ClaudeUsage

	entries, err := os.ReadDir(dir)
	if err != nil {
		return total
	}

	// Grace period: count files modified up to 20 seconds before daemon start
	gracePeriod := minTime.Add(-20 * time.Second)

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}

		info, err := entry.Info()
		if err != nil || info.ModTime().Before(gracePeriod) {
			continue
		}

		usage := parseJSONLFile(filepath.Join(dir, entry.Name()))
		total.InputTokens += usage.InputTokens
		total.OutputTokens += usage.OutputTokens
		total.CacheCreationInputTokens += usage.CacheCreationInputTokens
		total.CacheReadInputTokens += usage.CacheReadInputTokens
	}

	return total
}

// jsonlMessage is the structure of a Claude Code session message.
type jsonlMessage struct {
	Type    string `json:"type"`
	Message struct {
		Usage *struct {
			InputTokens              int64 `json:"input_tokens"`
			OutputTokens             int64 `json:"output_tokens"`
			CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
			CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
		} `json:"usage"`
	} `json:"message"`
}

// parseJSONLFile reads a single JSONL file and extracts usage data.
func parseJSONLFile(path string) ClaudeUsage {
	var total ClaudeUsage

	f, err := os.Open(path)
	if err != nil {
		return total
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	// Increase buffer size for large lines
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	for scanner.Scan() {
		var msg jsonlMessage
		if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil {
			continue
		}

		// Only assistant messages have usage data
		if msg.Type != "assistant" || msg.Message.Usage == nil {
			continue
		}

		u := msg.Message.Usage
		total.InputTokens += u.InputTokens
		total.OutputTokens += u.OutputTokens
		total.CacheCreationInputTokens += u.CacheCreationInputTokens
		total.CacheReadInputTokens += u.CacheReadInputTokens
	}

	return total
}
