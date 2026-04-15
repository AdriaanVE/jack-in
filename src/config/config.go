package config

import (
	"fmt"
	"os"
	"regexp"

	"github.com/AdriaanVE/jack-in/src/domain"
	"gopkg.in/yaml.v3"
)

const MaxWorkers = 5

var safeName = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

type WorkerConfig struct {
	Name   string            `yaml:"name"`
	Agent  domain.AgentType  `yaml:"agent"`
	Prompt string            `yaml:"prompt"`
	Role   domain.WorkerRole `yaml:"role"`
}

type TaskConfig struct {
	Summary     string   `yaml:"summary"`
	Description string   `yaml:"description,omitempty"`
	Files       []string `yaml:"files,omitempty"`
	Acceptance  []string `yaml:"acceptance,omitempty"`
	DependsOn   []string `yaml:"depends_on,omitempty"`
}

type OrchestratorConfig struct {
	PollInterval int                 `yaml:"poll_interval"`
	MaxRetries   int                 `yaml:"max_retries"`
	Approval     domain.ApprovalMode `yaml:"approval"`
	Agent        string              `yaml:"agent"` // agent type string or "false"
}

type StartupInstructions struct {
	Default string `yaml:"default"`
	Codex   string `yaml:"codex"`
}

type Config struct {
	Project             string               `yaml:"project"`
	Branch              string               `yaml:"branch,omitempty"`
	Workers             []WorkerConfig       `yaml:"workers"`
	Tasks               []TaskConfig         `yaml:"tasks,omitempty"`
	Orchestrator        OrchestratorConfig   `yaml:"orchestrator"`
	StartupInstructions *StartupInstructions `yaml:"startup_instructions,omitempty"`
}

var DefaultStartupInstructions = StartupInstructions{
	Default: "Read README.md if it exists, then follow the instructions below.",
	Codex:   "Read ~/.codex/AGENTS.md and README.md if they exist, then follow the instructions below.",
}

func SessionName(project string) string {
	return "jackin-" + project
}

// OrchestratorEnabled returns true if an orchestrator agent is configured.
func (o *OrchestratorConfig) OrchestratorEnabled() bool {
	return o.Agent != "" && o.Agent != "false"
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	applyDefaults(&cfg)

	if err := validate(&cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func validate(cfg *Config) error {
	if cfg.Project == "" {
		return fmt.Errorf("config: 'project' must be a non-empty string")
	}
	if !safeName.MatchString(cfg.Project) {
		return fmt.Errorf("config: invalid project name %q: must be alphanumeric, hyphens, underscores", cfg.Project)
	}
	if len(cfg.Workers) == 0 {
		return fmt.Errorf("config: 'workers' must be a non-empty array")
	}
	if len(cfg.Workers) > MaxWorkers {
		return fmt.Errorf("config: maximum %d workers allowed, got %d", MaxWorkers, len(cfg.Workers))
	}

	names := make(map[string]bool)
	for _, w := range cfg.Workers {
		if !safeName.MatchString(w.Name) {
			return fmt.Errorf("config: invalid worker name %q", w.Name)
		}
		if names[w.Name] {
			return fmt.Errorf("config: duplicate worker name %q", w.Name)
		}
		names[w.Name] = true

		if !domain.IsAgentType(string(w.Agent)) {
			return fmt.Errorf("config: worker %q has invalid agent %q", w.Name, w.Agent)
		}
		if !domain.IsWorkerRole(string(w.Role)) {
			return fmt.Errorf("config: worker %q has invalid role %q", w.Name, w.Role)
		}
		if w.Prompt == "" {
			return fmt.Errorf("config: worker %q 'prompt' must be a non-empty string", w.Name)
		}
	}

	for i, t := range cfg.Tasks {
		if t.Summary == "" {
			return fmt.Errorf("config: task[%d] 'summary' must be a non-empty string", i)
		}
	}

	if !domain.IsApprovalMode(string(cfg.Orchestrator.Approval)) {
		return fmt.Errorf("config: invalid approval mode %q", cfg.Orchestrator.Approval)
	}

	if cfg.Orchestrator.OrchestratorEnabled() {
		if !domain.IsAgentType(cfg.Orchestrator.Agent) {
			return fmt.Errorf("config: orchestrator agent %q is invalid", cfg.Orchestrator.Agent)
		}
	}

	return nil
}

func applyDefaults(cfg *Config) {
	if cfg.Branch == "" {
		cfg.Branch = "jackin-develop"
	}
	if cfg.Orchestrator.PollInterval == 0 {
		cfg.Orchestrator.PollInterval = 5000
	}
	if cfg.Orchestrator.MaxRetries == 0 {
		cfg.Orchestrator.MaxRetries = 2
	}
	if cfg.Orchestrator.Approval == "" {
		cfg.Orchestrator.Approval = domain.ApprovalManual
	}
	if cfg.Orchestrator.Agent == "" {
		cfg.Orchestrator.Agent = string(domain.AgentClaude)
	}

	for i := range cfg.Workers {
		if cfg.Workers[i].Name == "" {
			cfg.Workers[i].Name = fmt.Sprintf("worker-%d", i+1)
		}
		if cfg.Workers[i].Role == "" {
			cfg.Workers[i].Role = domain.RoleExecutor
		}
	}

	if cfg.StartupInstructions == nil {
		si := DefaultStartupInstructions
		cfg.StartupInstructions = &si
	}
}
