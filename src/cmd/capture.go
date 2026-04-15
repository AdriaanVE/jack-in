package cmd

import (
	"fmt"

	"github.com/AdriaanVE/jack-in/src/config"
	"github.com/AdriaanVE/jack-in/src/tmux"
	"github.com/spf13/cobra"
)

func init() {
	captureCmd.Flags().IntP("lines", "n", 50, "number of lines to capture")
	rootCmd.AddCommand(captureCmd)
}

var captureCmd = &cobra.Command{
	Use:   "capture <worker|orchestrator>",
	Short: "Capture a worker or orchestrator pane output",
	Args:  cobra.ExactArgs(1),
	RunE:  runCapture,
}

func runCapture(cmd *cobra.Command, args []string) error {
	base := cwd()
	cfg, err := loadConfig(base)
	if err != nil {
		return err
	}
	session := config.SessionName(cfg.Project)

	if !tmux.HasSession(session) {
		return fmt.Errorf("session '%s' is not running", session)
	}

	role := args[0]
	if role != "orchestrator" && !hasWorker(cfg, role) {
		return fmt.Errorf("unknown role %q; use 'orchestrator' or worker name: %s", role, workerNames(cfg))
	}

	// Use pane ID via @jackin_role for reliable targeting
	target := tmux.PaneTarget(session, role)
	lines, _ := cmd.Flags().GetInt("lines")

	content, err := tmux.CapturePane(target, lines)
	if err != nil {
		return fmt.Errorf("capture failed: %w", err)
	}

	fmt.Print(content)
	return nil
}
