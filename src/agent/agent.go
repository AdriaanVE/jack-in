package agent

import (
	"os/exec"
	"strings"

	"github.com/AdriaanVE/jack-in/src/domain"
)

// ShellEscape single-quotes a string for safe shell interpolation.
func ShellEscape(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

// SpawnCommand builds a shell command to launch an agent with a prompt.
func SpawnCommand(agentType domain.AgentType, prompt string, startupDefault, startupCodex string) string {
	fullPrompt := prompt
	if agentType == domain.AgentCodex && startupCodex != "" {
		fullPrompt = startupCodex + "\n\n" + prompt
	} else if startupDefault != "" {
		fullPrompt = startupDefault + "\n\n" + prompt
	}

	escaped := ShellEscape(fullPrompt)
	switch agentType {
	case domain.AgentCodex:
		return "codex --full-auto " + escaped
	case domain.AgentOpencode:
		return "opencode run " + escaped
	case domain.AgentGemini:
		return "gemini " + escaped
	default: // claude
		return "claude " + escaped
	}
}

// InitCommand builds a shell command for spawning an agent with a prompt file.
func InitCommand(agentType domain.AgentType, promptFile string) string {
	file := ShellEscape(promptFile)
	return string(agentType) + " \"$(cat " + file + ")\""
}

type Detected struct {
	Agent domain.AgentType
	Path  string
}

// Find checks if an agent CLI is available on PATH.
func Find(agentType domain.AgentType) *Detected {
	path, err := exec.LookPath(string(agentType))
	if err != nil {
		return nil
	}
	return &Detected{Agent: agentType, Path: path}
}

// DetectAll finds all installed agent CLIs in preference order.
func DetectAll() []Detected {
	var found []Detected
	for _, a := range domain.AgentNames {
		if d := Find(a); d != nil {
			found = append(found, *d)
		}
	}
	return found
}
