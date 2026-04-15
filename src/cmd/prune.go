package cmd

import (
	"fmt"

	"github.com/AdriaanVE/jack-in/src/worktree"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(pruneCmd)
}

var pruneCmd = &cobra.Command{
	Use:   "prune",
	Short: "Remove worktrees that don't match any configured worker",
	RunE:  runPrune,
}

func runPrune(cmd *cobra.Command, args []string) error {
	base := cwd()
	cfg, err := loadConfig(base)
	if err != nil {
		return err
	}

	stale, err := worktree.Stale(base, cfg.Project, workerNamesSlice(cfg))
	if err != nil {
		return fmt.Errorf("detecting stale worktrees: %w", err)
	}

	if len(stale) == 0 {
		fmt.Println("No orphaned worktrees found.")
		return nil
	}

	pruneOrphans(base, stale, false)
	return nil
}
