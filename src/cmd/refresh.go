package cmd

import (
	"fmt"

	"github.com/AdriaanVE/jack-in/src/worktree"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(refreshCmd)
}

var refreshCmd = &cobra.Command{
	Use:   "refresh <worker>",
	Short: "Reset a worker's worktree to the target branch",
	Long: `Reset a worker's worktree to the target branch (from jack-in.yaml).

This discards all uncommitted changes and removes untracked files.
Use this to clean up a worktree before retrying a task.`,
	Args: cobra.ExactArgs(1),
	RunE: runRefresh,
}

func runRefresh(cmd *cobra.Command, args []string) error {
	base := cwd()
	cfg, err := loadConfig(base)
	if err != nil {
		return err
	}

	name := args[0]
	if !hasWorker(cfg, name) {
		return fmt.Errorf("worker %q not found; available: %s", name, workerNames(cfg))
	}

	wtPath := worktree.Path(base, cfg.Project, name)
	branch := cfg.Branch

	fmt.Printf("Refreshing worktree for '%s' to branch '%s'...\n", name, branch)

	if err := worktree.Refresh(wtPath, cfg.Project, name, branch); err != nil {
		return fmt.Errorf("refresh failed: %w", err)
	}

	fmt.Printf("Worktree '%s' refreshed.\n", name)
	return nil
}
