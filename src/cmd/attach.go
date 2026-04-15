package cmd

import (
	"fmt"

	"github.com/AdriaanVE/jack-in/src/config"
	"github.com/AdriaanVE/jack-in/src/tmux"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(attachCmd)
}

var attachCmd = &cobra.Command{
	Use:   "attach [worker]",
	Short: "Attach to a worker's tmux window (default: dashboard)",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runAttach,
}

func runAttach(cmd *cobra.Command, args []string) error {
	base := cwd()
	cfg, err := loadConfig(base)
	if err != nil {
		return err
	}
	session := config.SessionName(cfg.Project)

	if !tmux.HasSession(session) {
		return fmt.Errorf("session '%s' is not running; run 'jackin up' first", session)
	}

	if len(args) == 0 {
		return attachSession(session)
	}

	target := args[0]
	if target == "orchestrator" {
		return attachSession(session + ":dashboard-orchestrator")
	}
	if !hasWorker(cfg, target) {
		return fmt.Errorf("worker %q not found; available: orchestrator, %s", target, workerNames(cfg))
	}
	return attachSession(session + ":" + target)
}
