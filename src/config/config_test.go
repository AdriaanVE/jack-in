package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "jack-in.yaml")
	os.WriteFile(cfgPath, []byte(content), 0o644)
	return cfgPath
}

func TestBranchDefaultsToJackinDevelop(t *testing.T) {
	cfg, err := Load(writeConfig(t, `
project: test-proj
workers:
  - name: w1
    agent: claude
    prompt: "do stuff"
`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Branch != "jackin-develop" {
		t.Errorf("Branch = %q, want %q", cfg.Branch, "jackin-develop")
	}
}

func TestBranchFromConfig(t *testing.T) {
	cfg, err := Load(writeConfig(t, `
project: test-proj
branch: develop
workers:
  - name: w1
    agent: claude
    prompt: "do stuff"
`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Branch != "develop" {
		t.Errorf("Branch = %q, want %q", cfg.Branch, "develop")
	}
}

func TestSessionName(t *testing.T) {
	got := SessionName("my-app")
	if got != "jackin-my-app" {
		t.Errorf("SessionName = %q, want %q", got, "jackin-my-app")
	}
}

func TestDefaults(t *testing.T) {
	cfg, err := Load(writeConfig(t, `
project: proj
workers:
  - agent: claude
    prompt: "do stuff"
`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Workers[0].Name != "worker-1" {
		t.Errorf("worker name default = %q, want %q", cfg.Workers[0].Name, "worker-1")
	}
	if cfg.Workers[0].Role != "executor" {
		t.Errorf("worker role default = %q, want %q", cfg.Workers[0].Role, "executor")
	}
	if cfg.Orchestrator.PollInterval != 5000 {
		t.Errorf("poll interval default = %d, want 5000", cfg.Orchestrator.PollInterval)
	}
	if cfg.Orchestrator.MaxRetries != 2 {
		t.Errorf("max retries default = %d, want 2", cfg.Orchestrator.MaxRetries)
	}
	if cfg.Orchestrator.Approval != "manual" {
		t.Errorf("approval default = %q, want %q", cfg.Orchestrator.Approval, "manual")
	}
	if cfg.Orchestrator.Agent != "claude" {
		t.Errorf("orchestrator agent default = %q, want %q", cfg.Orchestrator.Agent, "claude")
	}
}

func TestValidation(t *testing.T) {
	tests := []struct {
		name    string
		config  string
		wantErr string
	}{
		{
			name:    "missing project",
			config:  "workers:\n  - name: w1\n    agent: claude\n    prompt: do stuff\n",
			wantErr: "'project' must be a non-empty string",
		},
		{
			name:    "invalid project name",
			config:  "project: 'bad name!'\nworkers:\n  - name: w1\n    agent: claude\n    prompt: do stuff\n",
			wantErr: "invalid project name",
		},
		{
			name:    "no workers",
			config:  "project: proj\nworkers: []\n",
			wantErr: "'workers' must be a non-empty array",
		},
		{
			name:    "too many workers",
			config:  "project: proj\nworkers:\n  - {name: w1, agent: claude, prompt: p}\n  - {name: w2, agent: claude, prompt: p}\n  - {name: w3, agent: claude, prompt: p}\n  - {name: w4, agent: claude, prompt: p}\n  - {name: w5, agent: claude, prompt: p}\n  - {name: w6, agent: claude, prompt: p}\n",
			wantErr: "maximum 5 workers",
		},
		{
			name:    "invalid worker name",
			config:  "project: proj\nworkers:\n  - {name: 'bad name!', agent: claude, prompt: p}\n",
			wantErr: "invalid worker name",
		},
		{
			name:    "duplicate worker name",
			config:  "project: proj\nworkers:\n  - {name: w1, agent: claude, prompt: p}\n  - {name: w1, agent: claude, prompt: p}\n",
			wantErr: "duplicate worker name",
		},
		{
			name:    "invalid agent type",
			config:  "project: proj\nworkers:\n  - {name: w1, agent: gpt, prompt: p}\n",
			wantErr: "invalid agent",
		},
		{
			name:    "empty worker prompt",
			config:  "project: proj\nworkers:\n  - {name: w1, agent: claude, prompt: ''}\n",
			wantErr: "'prompt' must be a non-empty string",
		},
		{
			name:    "empty task summary",
			config:  "project: proj\nworkers:\n  - {name: w1, agent: claude, prompt: p}\ntasks:\n  - summary: ''\n",
			wantErr: "'summary' must be a non-empty string",
		},
		{
			name:    "invalid orchestrator agent",
			config:  "project: proj\nworkers:\n  - {name: w1, agent: claude, prompt: p}\norchestrator:\n  agent: gpt\n",
			wantErr: "orchestrator agent",
		},
		{
			name:   "orchestrator agent false is valid",
			config: "project: proj\nworkers:\n  - {name: w1, agent: claude, prompt: p}\norchestrator:\n  agent: 'false'\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Load(writeConfig(t, tt.config))
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error %q should contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestInvalidYAML(t *testing.T) {
	_, err := Load(writeConfig(t, "{{invalid"))
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}

func TestFileNotFound(t *testing.T) {
	_, err := Load("/nonexistent/jack-in.yaml")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}
