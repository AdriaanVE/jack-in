package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/AdriaanVE/jack-in/src/config"
	"github.com/AdriaanVE/jack-in/src/domain"
	"github.com/AdriaanVE/jack-in/src/taskqueue"
	"github.com/AdriaanVE/jack-in/src/tmux"
	"github.com/AdriaanVE/jack-in/src/worktree"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(resetCmd)
}

var resetCmd = &cobra.Command{
	Use:   "reset <worker>",
	Short: "Kill and respawn a worker agent",
	Args:  cobra.ExactArgs(1),
	RunE:  runReset,
}

func runReset(cmd *cobra.Command, args []string) error {
	base := cwd()
	cfg, err := loadConfig(base)
	if err != nil {
		return err
	}

	name := args[0]
	if !hasWorker(cfg, name) {
		return fmt.Errorf("worker %q not found; available: %s", name, workerNames(cfg))
	}

	session := config.SessionName(cfg.Project)
	if !tmux.HasSession(session) {
		return fmt.Errorf("session '%s' is not running; run 'jackin up' first", session)
	}

	// Find the worker config to rebuild the spawn command
	var wCfg *config.WorkerConfig
	for i := range cfg.Workers {
		if cfg.Workers[i].Name == name {
			wCfg = &cfg.Workers[i]
			break
		}
	}

	wt := worktree.Path(base, cfg.Project, name)
	shellCmd := workerShellCmd(wCfg, wt, cfg.StartupInstructions)

	fmt.Printf("Resetting worker '%s'...\n", name)

	// Unclaim any active task so the daemon can reassign it
	currentTasks, _ := taskqueue.List(base, domain.StateCurrent)
	for _, entry := range currentTasks {
		if entry.Task.Assignee == name {
			if _, err := taskqueue.Unclaim(base, entry.Task.ID); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to unclaim task %s: %v\n", entry.Task.ID, err)
			} else {
				fmt.Printf("Unclaimed task '%s' (moved back to pending).\n", entry.Task.Summary)
			}
			break
		}
	}

	_ = tmux.KillWindow(session, name)

	if err := tmux.CreateWindowDetached(session, name); err != nil {
		return fmt.Errorf("creating window: %w", err)
	}

	target := session + ":" + name
	_ = tmux.SetPaneOption(target+".0", "@jackin_role", name)

	if err := tmux.SendKeys(target, shellCmd, true); err != nil {
		return fmt.Errorf("sending spawn command: %w", err)
	}

	// Dismiss startup prompt for non-Claude agents
	if wCfg.Agent != domain.AgentClaude {
		time.Sleep(3 * time.Second)
		_ = tmux.SendKeys(target, "", true)
	}

	fmt.Printf("Worker '%s' reset successfully.\n", name)
	return nil
}
