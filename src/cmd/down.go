package cmd

import (
	"fmt"
	"os"

	"github.com/AdriaanVE/jack-in/src/config"
	"github.com/AdriaanVE/jack-in/src/tmux"
	"github.com/AdriaanVE/jack-in/src/worktree"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(downCmd)
}

var downCmd = &cobra.Command{
	Use:   "down",
	Short: "Kill session and clean up worktrees",
	RunE:  runDown,
}

func runDown(cmd *cobra.Command, args []string) error {
	base := cwd()

	// Try loading config for project name, but don't require it
	var project string
	cfg, err := loadConfig(base)
	if err == nil {
		project = cfg.Project
	}

	// Kill project session
	if project != "" {
		session := config.SessionName(project)
		if tmux.HasSession(session) {
			if err := tmux.KillSession(session); err != nil {
				return fmt.Errorf("killing session: %w", err)
			}
			fmt.Printf("Killed tmux session '%s'.\n", session)
		} else {
			fmt.Printf("No active session '%s'.\n", session)
		}
	} else {
		fmt.Println("No config found. Skipping tmux session cleanup.")
	}

	// Kill init session if it exists
	if tmux.HasSession(initSessionName) {
		if err := tmux.KillSession(initSessionName); err != nil {
			return fmt.Errorf("killing init session: %w", err)
		}
		fmt.Printf("Killed init session '%s'.\n", initSessionName)
	}

	// Find jackin worktrees
	allWorktrees, err := worktree.List(base)
	if err != nil {
		return fmt.Errorf("listing worktrees: %w", err)
	}
	jackinWTs := filterJackInWorktrees(allWorktrees, project)

	if len(jackinWTs) == 0 {
		fmt.Println("No worktrees to clean up.")
		return nil
	}

	// Check for dirty worktrees and prompt before removing
	fmt.Println("\nWorktrees to remove:")
	anyDirty := printWorktreeStatuses(jackinWTs)
	if anyDirty {
		answer := promptUser("\nWorktrees contain uncommitted changes. Remove anyway? [y/N] ")
		if answer != "y" {
			fmt.Println("Worktrees kept.")
			return nil
		}
	}

	removed, errs := worktree.RemoveAll(base, jackinWTs)
	for _, e := range errs {
		fmt.Fprintf(os.Stderr, "  %v\n", e)
	}
	fmt.Printf("Removed %d worktree%s.\n", len(removed), plural(len(removed)))
	return nil
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
