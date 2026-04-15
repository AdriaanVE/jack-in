package usage

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestClaudeProjectPath(t *testing.T) {
	// Test that path encoding works correctly
	path := claudeProjectPath("/Users/test/project")
	if !filepath.IsAbs(path) {
		t.Errorf("expected absolute path, got %q", path)
	}
	if !contains(path, "Users-test-project") {
		t.Errorf("expected encoded path in %q", path)
	}
}

func TestParseJSONLFile_NotExists(t *testing.T) {
	usage := parseJSONLFile("/nonexistent/file.jsonl")
	if usage.Total() != 0 {
		t.Errorf("expected zero usage for nonexistent file, got %d", usage.Total())
	}
}

func TestParseJSONLFile_Valid(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "test.jsonl")

	// Write a minimal JSONL with usage data
	content := `{"type":"user","message":{}}
{"type":"assistant","message":{"usage":{"input_tokens":100,"output_tokens":50,"cache_creation_input_tokens":10,"cache_read_input_tokens":5}}}
{"type":"assistant","message":{"usage":{"input_tokens":200,"output_tokens":100}}}
`
	if err := os.WriteFile(f, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	usage := parseJSONLFile(f)
	if usage.InputTokens != 300 {
		t.Errorf("input_tokens = %d, want 300", usage.InputTokens)
	}
	if usage.OutputTokens != 150 {
		t.Errorf("output_tokens = %d, want 150", usage.OutputTokens)
	}
	if usage.CacheCreationInputTokens != 10 {
		t.Errorf("cache_creation = %d, want 10", usage.CacheCreationInputTokens)
	}
}

func TestParseClaudeSessions_FiltersByTime(t *testing.T) {
	dir := t.TempDir()

	// Create an old file (should be skipped - outside 20s grace period)
	oldFile := filepath.Join(dir, "old.jsonl")
	content := `{"type":"assistant","message":{"usage":{"input_tokens":1000,"output_tokens":500}}}
`
	if err := os.WriteFile(oldFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	// Set old modification time to 1 minute ago (outside 20s grace period)
	oldTime := time.Now().Add(-1 * time.Minute)
	os.Chtimes(oldFile, oldTime, oldTime)

	// Create a new file (should be counted - within grace period)
	newFile := filepath.Join(dir, "new.jsonl")
	if err := os.WriteFile(newFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	// Parse with minTime = now (grace period extends 20s back)
	minTime := time.Now()
	usage := parseClaudeSessions(dir, minTime)

	// Should only count the new file (old file is outside grace period)
	if usage.InputTokens != 1000 {
		t.Errorf("input_tokens = %d, want 1000 (only new file)", usage.InputTokens)
	}
}

func TestTracker_Update(t *testing.T) {
	tracker := NewTracker("/tmp/nonexistent-project", "test-project", time.Now())
	tracker.Update([]string{"worker-1", "worker-2"})

	// Should not panic, just return zero usage
	usage := tracker.GetTotalUsage()
	if usage.Total() != 0 {
		t.Errorf("expected zero total for nonexistent project")
	}
}

func TestClaudeUsage_CostUSD(t *testing.T) {
	// Opus 4.6 pricing
	usage := ClaudeUsage{
		InputTokens:              1_000_000, // 1M input = $15
		OutputTokens:             1_000_000, // 1M output = $75
		CacheCreationInputTokens: 1_000_000, // 1M cache write = $18.75
		CacheReadInputTokens:     1_000_000, // 1M cache read = $1.50
	}

	cost := usage.CostUSD()
	expected := 15.0 + 75.0 + 18.75 + 1.50 // = $110.25

	if cost < expected-0.01 || cost > expected+0.01 {
		t.Errorf("CostUSD() = %.2f, want %.2f", cost, expected)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsAt(s, substr))
}

func containsAt(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
