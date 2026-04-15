package cmd

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/AdriaanVE/jack-in/src/agent"
	"github.com/AdriaanVE/jack-in/src/domain"
	"github.com/AdriaanVE/jack-in/src/quotes"
	"github.com/AdriaanVE/jack-in/src/tmux"
	"github.com/AdriaanVE/jack-in/src/worktree"
	"github.com/spf13/cobra"
)

//go:embed embed/init-instructions.md
var initInstructions string

//go:embed embed/skill.md
var skillContent string

const initSessionName = "jackin-init"

func init() {
	initCmd.Flags().String("agent", "", "agent to run init with (claude, codex, opencode, gemini)")
	initCmd.Flags().Bool("template", false, "write a template config without an LLM")
	rootCmd.AddCommand(initCmd)
}

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize a jack-in.yaml config (interactive or template)",
	RunE:  runInit,
}

func runInit(cmd *cobra.Command, args []string) error {
	templateMode, _ := cmd.Flags().GetBool("template")
	if templateMode {
		return initTemplate()
	}
	agentFlag, _ := cmd.Flags().GetString("agent")
	return initInteractive(agentFlag)
}

func initInteractive(agentFlag string) error {
	base := cwd()
	if err := requireGitRepo(base); err != nil {
		return err
	}
	if err := requireCleanGitState(base); err != nil {
		return err
	}

	// If init session already exists, offer to attach
	if tmux.HasSession(initSessionName) {
		answer := promptUser("Init session already running. [A]ttach, [R]estart, [Q]uit? [a/r/Q] ")
		switch answer {
		case "a":
			return attachSession(initSessionName)
		case "r":
			if err := tmux.KillSession(initSessionName); err != nil {
				return err
			}
		default:
			fmt.Println("Aborted.")
			os.Exit(0)
		}
	}

	// Check for existing config, state, or worktrees
	configPath := findConfigPath(base)
	jackInDir := filepath.Join(base, ".jack-in")
	hasState := dirExists(jackInDir)

	// Find any jackin worktrees (prune stale refs first)
	_ = worktree.Prune(base)
	allWorktrees, _ := worktree.List(base)
	jackinWorktrees := filterJackInWorktrees(allWorktrees, "")

	if configPath != "" || hasState || len(jackinWorktrees) > 0 {
		fmt.Println("Existing jackin setup detected:")
		if configPath != "" {
			fmt.Printf("  - %s (config)\n", filepath.Base(configPath))
		}
		if hasState {
			fmt.Println("  - .jack-in/ (tasks, signals, logs)")
		}
		if len(jackinWorktrees) > 0 {
			fmt.Printf("  - %d worktree(s)\n", len(jackinWorktrees))
		}
		fmt.Println("\nRunning init will overwrite the config and clear all tasks/state.")
		answer := promptUser("Continue? [y/N] ")
		if answer != "y" {
			fmt.Println("Aborted.")
			os.Exit(0)
		}
		// Clean .jack-in directory for fresh start
		if hasState {
			if err := os.RemoveAll(jackInDir); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to clean .jack-in/: %v\n", err)
			}
		}
		// Remove existing worktrees
		if len(jackinWorktrees) > 0 {
			fmt.Printf("Removing %d worktree(s)...\n", len(jackinWorktrees))
			for _, wt := range jackinWorktrees {
				if err := worktree.Remove(wt.Path, base); err != nil {
					fmt.Fprintf(os.Stderr, "Warning: failed to remove worktree %s: %v\n", wt.Path, err)
				}
			}
		}
	}

	// Detect or validate agent
	quotes.Print(quotes.Init)
	fmt.Println("Detecting installed agents...")
	var detected []agent.Detected
	if agentFlag != "" {
		if !domain.IsAgentType(agentFlag) {
			return fmt.Errorf("invalid agent %q, must be one of: claude, codex, opencode, gemini", agentFlag)
		}
		d := agent.Find(domain.AgentType(agentFlag))
		if d == nil {
			return fmt.Errorf("agent '%s' not found on PATH", agentFlag)
		}
		detected = []agent.Detected{*d}
	} else {
		detected = agent.DetectAll()
	}

	if len(detected) == 0 {
		fmt.Fprintln(os.Stderr, "No agent CLIs found on PATH. Install at least one of:")
		for _, a := range domain.AgentNames {
			fmt.Fprintf(os.Stderr, "  - %s\n", a)
		}
		os.Exit(1)
	}

	// Pick agent
	var initAgent domain.AgentType
	if len(detected) == 1 {
		fmt.Printf("  Found %s (%s)\n", detected[0].Agent, detected[0].Path)
		time.Sleep(2 * time.Second)
		initAgent = detected[0].Agent
	} else {
		fmt.Println("\n  Available agents:")
		for i, d := range detected {
			fmt.Printf("    [%d] %s  %s\n", i+1, d.Agent, d.Path)
		}
		answer := promptUser(fmt.Sprintf("\n  Which agent should run the setup? [1-%d] ", len(detected)))
		idx := 0
		fmt.Sscanf(answer, "%d", &idx)
		idx--
		if idx < 0 || idx >= len(detected) {
			fmt.Println("Aborted.")
			os.Exit(0)
		}
		initAgent = detected[idx].Agent
	}

	agents := make([]string, len(detected))
	for i, d := range detected {
		agents[i] = string(d.Agent)
	}

	// Read project context
	fmt.Println("Reading project context...")
	hasReadme := readmeExists(base)
	projectName := safeProjectName(base)

	// Write skill file for the init agent to copy into the project
	promptDir := filepath.Join(base, ".jack-in")
	if err := os.MkdirAll(promptDir, 0o755); err != nil {
		return fmt.Errorf("creating .jack-in dir: %w", err)
	}
	if err := os.WriteFile(filepath.Join(promptDir, "skill.md"), []byte(skillContent), 0o644); err != nil {
		return fmt.Errorf("writing skill file: %w", err)
	}

	// Assemble prompt (instructions + runtime context)
	prompt := assembleInitPrompt(agents, projectName, hasReadme)
	promptFile := filepath.Join(promptDir, "init-prompt.md")
	if err := os.WriteFile(promptFile, []byte(prompt), 0o644); err != nil {
		return fmt.Errorf("writing prompt: %w", err)
	}

	// Write minimal Claude settings so init agent doesn't inherit stale hooks
	if initAgent == domain.AgentClaude {
		if err := writeClaudeInitSettings(base); err != nil {
			return fmt.Errorf("writing claude settings: %w", err)
		}
	}

	// Create tmux session and spawn agent
	fmt.Printf("\nSpawning %s in tmux session '%s'...\n", initAgent, initSessionName)
	if err := tmux.CreateSession(initSessionName); err != nil {
		return fmt.Errorf("creating session: %w", err)
	}
	if err := tmux.RenameWindow(initSessionName, 0, "init"); err != nil {
		return err
	}

	target := initSessionName + ":init"
	shellCmd := fmt.Sprintf("cd %s && %s", agent.ShellEscape(base), agent.InitCommand(initAgent, promptFile))
	if err := tmux.SendKeys(target, shellCmd, true); err != nil {
		return err
	}

	fmt.Println("Attaching to session...")
	return attachSession(initSessionName)
}

func initTemplate() error {
	base := cwd()
	if err := requireGitRepo(base); err != nil {
		return err
	}
	if err := requireCleanGitState(base); err != nil {
		return err
	}
	projectName := safeProjectName(base)

	if path := findConfigPath(base); path != "" {
		return fmt.Errorf("%s already exists. Remove it first or run 'jackin init' to overwrite interactively", filepath.Base(path))
	}

	template := fmt.Sprintf(`# jack-in.yaml -- generated template
# Edit this file, then run: jackin up

project: %s

# Workers are named worker-1 .. worker-N by default (max 5).
# Set 'name' explicitly to override.
workers:
  - agent: claude        # claude | codex | opencode | gemini
    prompt: "implement features and fix bugs"
    role: executor       # executor | reviewer | planner

# orchestrator:
#   poll_interval: 5000  # ms
#   max_retries: 2
#   approval: manual     # manual | auto | yolo

# tasks:
#   - summary: "Explore the codebase"
#   - summary: "Add tests for core modules"
#     depends_on: ["Explore the codebase"]
`, projectName)

	if err := os.WriteFile(filepath.Join(base, "jack-in.yaml"), []byte(template), 0o644); err != nil {
		return err
	}
	fmt.Println("Written ./jack-in.yaml (template)")
	fmt.Println("Git setup:")
	fmt.Println("  - Commit jack-in.yaml (swarm config)")
	fmt.Println("  - Add to .gitignore:")
	fmt.Println("      .jack-in/")
	fmt.Println("      .w-*")
	quotes.Print(quotes.Init)
	fmt.Println("Edit the file, then run: jackin up")
	return nil
}

var safeNameRe = regexp.MustCompile(`[^a-zA-Z0-9_-]`)

func safeProjectName(base string) string {
	return safeNameRe.ReplaceAllString(filepath.Base(base), "-")
}

func readmeExists(base string) bool {
	for _, name := range []string{"README.md", "readme.md", "Readme.md"} {
		if _, err := os.Stat(filepath.Join(base, name)); err == nil {
			return true
		}
	}
	return false
}

func assembleInitPrompt(agents []string, projectName string, hasReadme bool) string {
	var b strings.Builder
	b.WriteString(initInstructions)
	b.WriteString("\n---\n\n## Runtime context\n\n")
	b.WriteString(fmt.Sprintf("**AVAILABLE AGENTS:** %s\n", strings.Join(agents, ", ")))
	b.WriteString(fmt.Sprintf("**SUGGESTED PROJECT NAME:** %s\n", projectName))

	if hasReadme {
		b.WriteString("\n**README:** This project has a README.md -- read it to understand the project.\n")
	} else {
		b.WriteString("\n**README:** No README.md found. Explore the project files yourself.\n")
	}

	return b.String()
}

func writeClaudeInitSettings(base string) error {
	settingsDir := filepath.Join(base, ".claude")
	if err := os.MkdirAll(settingsDir, 0o755); err != nil {
		return err
	}
	settings := map[string]any{
		"permissions": map[string]any{
			"allow": []string{"Bash(jackin *)"},
		},
		"hooks": map[string]any{},
	}
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(settingsDir, "settings.local.json"), append(data, '\n'), 0o644)
}
