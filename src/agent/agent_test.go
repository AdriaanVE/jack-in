package agent

import (
	"testing"

	"github.com/AdriaanVE/jack-in/src/domain"
)

func TestShellEscape(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"hello", "'hello'"},
		{"", "''"},
		{"it's", "'it'\\''s'"},
		{"a'b'c", "'a'\\''b'\\''c'"},
		{"spaces here", "'spaces here'"},
		{"$VAR", "'$VAR'"},
		{`"quoted"`, `'"quoted"'`},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := ShellEscape(tt.input)
			if got != tt.want {
				t.Errorf("ShellEscape(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestSpawnCommand(t *testing.T) {
	tests := []struct {
		name           string
		agent          domain.AgentType
		prompt         string
		startupDefault string
		startupCodex   string
		wantPrefix     string
	}{
		{
			name:       "claude default",
			agent:      domain.AgentClaude,
			prompt:     "do stuff",
			wantPrefix: "claude ",
		},
		{
			name:       "codex full-auto",
			agent:      domain.AgentCodex,
			prompt:     "do stuff",
			wantPrefix: "codex --full-auto ",
		},
		{
			name:       "opencode run",
			agent:      domain.AgentOpencode,
			prompt:     "do stuff",
			wantPrefix: "opencode run ",
		},
		{
			name:       "gemini",
			agent:      domain.AgentGemini,
			prompt:     "do stuff",
			wantPrefix: "gemini ",
		},
		{
			name:           "startup instructions prepended for claude",
			agent:          domain.AgentClaude,
			prompt:         "do stuff",
			startupDefault: "Read README.md",
			wantPrefix:     "claude 'Read README.md\n\ndo stuff'",
		},
		{
			name:           "codex uses codex-specific startup",
			agent:          domain.AgentCodex,
			prompt:         "do stuff",
			startupDefault: "default instruction",
			startupCodex:   "codex instruction",
			wantPrefix:     "codex --full-auto 'codex instruction\n\ndo stuff'",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SpawnCommand(tt.agent, tt.prompt, tt.startupDefault, tt.startupCodex)
			if tt.wantPrefix != "" && got != tt.wantPrefix {
				// For simple cases, check exact match; for prefix cases, just verify it starts right
				if len(got) < len(tt.wantPrefix) || got[:len(tt.wantPrefix)] != tt.wantPrefix[:min(len(got), len(tt.wantPrefix))] {
					t.Errorf("SpawnCommand() = %q, want prefix %q", got, tt.wantPrefix)
				}
			}
		})
	}
}

func TestInitCommand(t *testing.T) {
	got := InitCommand(domain.AgentClaude, "/tmp/prompt.md")
	want := "claude \"$(cat '/tmp/prompt.md')\""
	if got != want {
		t.Errorf("InitCommand() = %q, want %q", got, want)
	}

	got = InitCommand(domain.AgentCodex, "/tmp/prompt.md")
	want = "codex \"$(cat '/tmp/prompt.md')\""
	if got != want {
		t.Errorf("InitCommand(codex) = %q, want %q", got, want)
	}
}
