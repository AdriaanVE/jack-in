package cmd

import (
	"fmt"

	"github.com/AdriaanVE/jack-in/src/daemon"
	"github.com/AdriaanVE/jack-in/src/domain"
	"github.com/AdriaanVE/jack-in/src/worktree"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(approvalCmd)
}

var approvalCmd = &cobra.Command{
	Use:   "approval [mode]",
	Short: "Show or switch approval mode (manual, auto, yolo)",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runApproval,
}

func runApproval(cmd *cobra.Command, args []string) error {
	base := cwd()
	cfg, err := loadConfig(base)
	if err != nil {
		return err
	}

	if len(args) == 0 {
		mode, source := resolveApproval(base, cfg)
		fmt.Printf("Approval mode: %s (%s)\n", mode, source)
		return nil
	}

	// Switch mode
	mode := domain.ApprovalMode(args[0])
	if !domain.IsApprovalMode(string(mode)) {
		return fmt.Errorf("invalid mode %q: must be manual, auto, or yolo", args[0])
	}

	var switched []string
	var skipped []string

	for _, w := range cfg.Workers {
		if w.Agent != domain.AgentClaude {
			skipped = append(skipped, w.Name)
			continue
		}
		wt := worktree.Path(base, cfg.Project, w.Name)
		if err := daemon.WriteClaudeSettings(wt, base, w.Name, mode, nil); err != nil {
			return fmt.Errorf("updating settings for %s: %w", w.Name, err)
		}
		switched = append(switched, w.Name)
	}

	// Update orchestrator settings if it uses Claude
	if cfg.Orchestrator.Agent == string(domain.AgentClaude) {
		if err := daemon.MergeClaudeSettings(base, base, "orchestrator", mode, daemon.OrchestratorPermissions); err != nil {
			return fmt.Errorf("updating orchestrator settings: %w", err)
		}
	}

	// Write runtime mode file
	if err := daemon.WriteApprovalMode(base, mode); err != nil {
		return fmt.Errorf("writing approval mode: %w", err)
	}

	if len(switched) > 0 {
		fmt.Printf("Switched to %s: %v\n", mode, switched)
	}
	if len(skipped) > 0 {
		fmt.Printf("Skipped (non-Claude): %v\n", skipped)
	}
	return nil
}
