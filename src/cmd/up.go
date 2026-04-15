package cmd

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/AdriaanVE/jack-in/src/agent"
	"github.com/AdriaanVE/jack-in/src/config"
	"github.com/AdriaanVE/jack-in/src/daemon"
	"github.com/AdriaanVE/jack-in/src/domain"
	"github.com/AdriaanVE/jack-in/src/taskqueue"
	"github.com/AdriaanVE/jack-in/src/tmux"
	"github.com/AdriaanVE/jack-in/src/ui"
	"github.com/AdriaanVE/jack-in/src/worktree"
	"github.com/spf13/cobra"
)

//go:embed embed/worker-instructions.md
var workerInstructions string

//go:embed embed/orchestrator-instructions.md
var orchestratorInstructions string

func init() {
	upCmd.Flags().Bool("no-orchestrator", false, "skip starting the daemon")
	upCmd.Flags().Bool("no-orchestrator-agent", false, "skip starting the orchestrator agent")
	upCmd.Flags().Bool("no-animation", false, "skip the startup animation")
	upCmd.Flags().String("approval", "", "override approval mode (manual, auto, yolo)")
	rootCmd.AddCommand(upCmd)
}

var upCmd = &cobra.Command{
	Use:   "up",
	Short: "Spawn workers in tmux with git worktrees",
	RunE:  runUp,
}

func runUp(cmd *cobra.Command, args []string) error {
	base := cwd()
	if err := requireGitRepo(base); err != nil {
		return err
	}
	if err := requireCleanGitState(base); err != nil {
		return err
	}
	cfg, err := loadConfig(base)
	if err != nil {
		return err
	}

	noOrchestrator, _ := cmd.Flags().GetBool("no-orchestrator")
	noOrchAgent, _ := cmd.Flags().GetBool("no-orchestrator-agent")
	noAnimation, _ := cmd.Flags().GetBool("no-animation")
	if approvalFlag, _ := cmd.Flags().GetString("approval"); approvalFlag != "" {
		cfg.Orchestrator.Approval = domain.ApprovalMode(approvalFlag)
	}

	session := config.SessionName(cfg.Project)
	approval := cfg.Orchestrator.Approval

	// Prune stale worktree references before listing
	_ = worktree.Prune(base)

	// Fetch worktree list once for the entire function
	allWorktrees, err := worktree.List(base)
	if err != nil {
		return fmt.Errorf("listing worktrees: %w", err)
	}
	stale := filterJackInWorktrees(allWorktrees, cfg.Project)

	// Handle existing session
	if tmux.HasSession(session) {
		answer := promptUser(
			fmt.Sprintf("Session '%s' is already running.\n[A]ttach, [R]estart (kill + reset worktrees), [Q]uit? [a/r/Q] ", session),
		)
		switch answer {
		case "a":
			return attachSession(session)
		case "r":
			if err := tmux.KillSession(session); err != nil {
				return fmt.Errorf("killing session: %w", err)
			}
			fmt.Printf("Killed session '%s'.\n", session)
			if err := resetWorktrees(stale); err != nil {
				return err
			}
		default:
			fmt.Println("Aborted.")
			os.Exit(1)
		}
	} else if len(stale) > 0 {
		// Coming from jackin init: reset worktrees automatically for a clean start
		if tmux.HasSession(initSessionName) {
			fmt.Println("Starting fresh from init. Resetting worktrees...")
			if err := resetWorktrees(stale); err != nil {
				return err
			}
		} else {
			fmt.Println("Existing worktrees found from a previous run:")
			anyDirty := printWorktreeStatuses(stale)
			if anyDirty {
				answer := promptUserWithTimeout("[R]eset worktrees, [C]ontinue (reuse as-is), [A]bort to inspect? [r/c/A]", "c", 5)
				switch answer {
				case "r":
					if err := resetWorktrees(stale); err != nil {
						return err
					}
				case "c":
					fmt.Println("Reusing existing worktrees.")
				default:
					fmt.Println("Aborted.")
					os.Exit(1)
				}
			} else {
				fmt.Println("All clean. Reusing existing worktrees.")
			}
		}
	}

	// Prune orphaned worktrees that don't match any configured worker
	orphans := worktree.StaleFrom(allWorktrees, cfg.Project, workerNamesSlice(cfg))
	if len(orphans) > 0 {
		pruneOrphans(base, orphans, true)
	}

	// Start animation in background while workers boot
	animDone := make(chan struct{})
	if !noAnimation {
		go func() {
			ui.RunMatrixIntro(4500 * time.Millisecond)
			close(animDone)
		}()
	} else {
		close(animDone)
	}

	// --- Worker setup runs in parallel with animation ---

	// Init signal directories and install hook scripts
	if err := daemon.InitSignalDirs(base); err != nil {
		<-animDone
		return fmt.Errorf("init signals: %w", err)
	}
	if err := daemon.InstallHooks(base); err != nil {
		<-animDone
		return fmt.Errorf("install hooks: %w", err)
	}

	if err := tmux.CreateSession(session); err != nil {
		<-animDone
		return fmt.Errorf("creating session: %w", err)
	}
	if err := tmux.RenameWindow(session, 0, "dashboard-orchestrator"); err != nil {
		<-animDone
		return fmt.Errorf("renaming window: %w", err)
	}

	// Build reusable worktree map from pre-fetched list (skip prunable entries)
	reusable := make(map[string]string, len(stale))
	for _, e := range stale {
		if !e.Prunable {
			reusable[filepath.Base(e.Path)] = e.Path
		}
	}

	var nonClaudeTargets []string

	for _, w := range cfg.Workers {
		dirName := worktree.DirName(cfg.Project, w.Name)
		var wt string
		if existing, ok := reusable[dirName]; ok {
			wt = existing
		} else {
			var createErr error
			wt, createErr = worktree.Create(base, cfg.Project, w.Name)
			if createErr != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to create worktree for %s: %v\n", w.Name, createErr)
				continue
			}
		}

		// Write Claude settings BEFORE spawning the worker
		if w.Agent == domain.AgentClaude {
			_ = daemon.WriteClaudeSettings(wt, base, w.Name, approval, nil)
		}

		if err := tmux.CreateWindow(session, w.Name); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to create window for %s: %v\n", w.Name, err)
			continue
		}

		target := session + ":" + w.Name
		shellCmd := workerShellCmd(&w, wt, cfg.StartupInstructions)
		_ = tmux.SendKeys(target, shellCmd, true)
		_ = tmux.SetPaneOption(target+".0", "@jackin_role", w.Name)

		if w.Agent != domain.AgentClaude {
			nonClaudeTargets = append(nonClaudeTargets, target)
		}
	}

	// Dismiss startup prompts for non-Claude agents (3s delay hidden by animation)
	if len(nonClaudeTargets) > 0 {
		time.Sleep(3 * time.Second)
		for _, target := range nonClaudeTargets {
			tmux.SendKeys(target, "", true)
		}
	}

	// Wait for animation to finish before printing status
	<-animDone

	fmt.Printf("Starting swarm for '%s' with %d workers...\n", cfg.Project, len(cfg.Workers))
	fmt.Printf("Tasks:    %s\n", filepath.Join(base, ".jack-in", "tasks"))
	fmt.Printf("Approval: %s\n", approval)

	// Seed tasks from config
	if len(cfg.Tasks) > 0 {
		seeds := make([]taskqueue.TaskSeed, len(cfg.Tasks))
		for i, t := range cfg.Tasks {
			seeds[i] = taskqueue.TaskSeed{
				Summary:     t.Summary,
				Description: t.Description,
				Files:       t.Files,
				Acceptance:  t.Acceptance,
				DependsOn:   t.DependsOn,
			}
		}
		seeded, err := taskqueue.Seed(base, seeds)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to seed tasks: %v\n", err)
		} else if seeded > 0 {
			fmt.Printf("\nSeeded %d tasks from config.\n", seeded)
		}
	}

	// Start daemon in top pane
	if !noOrchestrator {
		dashTarget := session + ":dashboard-orchestrator"
		daemonCmd := buildDaemonCommand(approval)
		if err := tmux.SendKeys(dashTarget, daemonCmd, true); err != nil {
			return fmt.Errorf("starting daemon: %w", err)
		}
		_ = tmux.SetPaneOption(dashTarget, "@jackin_role", "daemon")
		fmt.Println("Daemon started in dashboard-orchestrator pane.")
	}

	// Start orchestrator agent in bottom pane
	if err := startOrchestratorAgent(session, base, cfg, approval, noOrchAgent); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: %v\n", err)
	}

	_ = tmux.SelectWindow(session, "dashboard-orchestrator")

	// Seamless transition from jackin init: switch the user's terminal to the
	// new session, kill the init session (which kills the init agent), and
	// return immediately -- no attach needed since the user is already there.
	if tmux.HasSession(initSessionName) {
		if os.Getenv("TMUX") != "" {
			_ = tmux.SwitchClient(session)
		}
		_ = tmux.KillSession(initSessionName)
		fmt.Printf("Killed init session '%s'.\n", initSessionName)
		fmt.Printf("Swarm running in tmux session '%s'.\n", session)
		return nil
	}

	fmt.Printf("\nSwarm running in tmux session '%s'. Attaching...\n", session)
	return attachSession(session)
}

func startOrchestratorAgent(session, base string, cfg *config.Config, approval domain.ApprovalMode, skip bool) error {
	orchAgent := domain.AgentType(cfg.Orchestrator.Agent)
	if skip || !cfg.Orchestrator.OrchestratorEnabled() {
		return nil
	}

	if orchAgent == domain.AgentClaude {
		if err := daemon.MergeClaudeSettings(base, base, "orchestrator", approval, daemon.OrchestratorPermissions); err != nil {
			return fmt.Errorf("orchestrator Claude settings: %w", err)
		}
	}

	promptFile, err := writeOrchestratorPrompt(base, cfg)
	if err != nil {
		return fmt.Errorf("orchestrator prompt: %w", err)
	}

	if err := tmux.SplitWindow(session, "dashboard-orchestrator", 70); err != nil {
		return fmt.Errorf("split window: %w", err)
	}

	orchTarget := session + ":dashboard-orchestrator.1"
	_ = tmux.SetPaneOption(orchTarget, "@jackin_role", "orchestrator")

	orchCmd := fmt.Sprintf("cd %s && %s", agent.ShellEscape(base), agent.InitCommand(orchAgent, promptFile))
	if err := tmux.SendKeys(orchTarget, orchCmd, true); err != nil {
		return fmt.Errorf("send orchestrator command: %w", err)
	}

	fmt.Printf("Orchestrator agent (%s) started in dashboard-orchestrator pane.\n", orchAgent)
	return nil
}

func buildWorkerPrompt(rolePrompt string, startup *config.StartupInstructions) string {
	var parts []string

	if startup != nil && startup.Default != "" {
		parts = append(parts, startup.Default)
	}

	parts = append(parts, workerInstructions)
	parts = append(parts, "---\n\nYour role: "+rolePrompt+"\n\n"+
		"Read the project README and familiarize yourself with the codebase.\n"+
		"Do not start making changes yet. The daemon will assign you specific tasks.\n"+
		"Wait for task assignments.")

	return strings.Join(parts, "\n\n")
}

// llmEnvVars are forwarded from the current process to the daemon shell
// so the LLM evaluator can authenticate.
var llmEnvVars = []string{
	"ANTHROPIC_API_KEY",
	"ANTHROPIC_FOUNDRY_RESOURCE",
	"ANTHROPIC_FOUNDRY_API_KEY",
	"ANTHROPIC_DEFAULT_SONNET_MODEL",
}

func buildDaemonCommand(approval domain.ApprovalMode) string {
	exe, err := os.Executable()
	if err != nil {
		exe = "jackin"
	}

	// Forward LLM env vars into the tmux pane's shell
	var envPrefix string
	for _, key := range llmEnvVars {
		if val := os.Getenv(key); val != "" {
			envPrefix += key + "=" + agent.ShellEscape(val) + " "
		}
	}

	cmd := envPrefix + agent.ShellEscape(exe) + " daemon"
	if approval != "" {
		cmd += " --approval " + agent.ShellEscape(string(approval))
	}
	return cmd
}

func writeOrchestratorPrompt(base string, cfg *config.Config) (string, error) {
	worktrees := make([]string, len(cfg.Workers))
	var dirtyInfo []string
	for i, w := range cfg.Workers {
		wt := worktree.Path(base, cfg.Project, w.Name)
		worktrees[i] = wt
		if n := worktree.DirtyCount(wt); n > 0 {
			dirtyInfo = append(dirtyInfo, fmt.Sprintf("  - %s: %d uncommitted changes", w.Name, n))
		}
	}

	parts := []string{
		orchestratorInstructions,
		"",
		"## Current status",
		"",
		fmt.Sprintf("Run `jackin status --json` to get the current swarm state. The project root is: %s", base),
		"",
		fmt.Sprintf("Target branch for merging approved work: `%s`", cfg.Branch),
		"",
		fmt.Sprintf("Worker worktrees are at: %s", strings.Join(worktrees, ", ")),
	}

	if len(dirtyInfo) > 0 {
		parts = append(parts, "", "## Dirty worktrees (from previous run)", "")
		parts = append(parts, "The following worktrees have uncommitted changes. Check if these need attention:")
		parts = append(parts, dirtyInfo...)
	}

	prompt := strings.Join(parts, "\n")

	promptDir := filepath.Join(base, ".jack-in")
	if err := os.MkdirAll(promptDir, 0o755); err != nil {
		return "", err
	}
	promptFile := filepath.Join(promptDir, "orchestrator-prompt.md")
	if err := os.WriteFile(promptFile, []byte(prompt), 0o644); err != nil {
		return "", err
	}
	return promptFile, nil
}

func resetWorktrees(entries []worktree.Info) error {
	for _, e := range entries {
		if err := worktree.Reset(e.Path); err != nil {
			return fmt.Errorf("resetting worktree %s: %w", e.Path, err)
		}
	}
	fmt.Println("Worktrees reset.")
	return nil
}
