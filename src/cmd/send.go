package cmd

import (
	"fmt"
	"strings"

	"github.com/AdriaanVE/jack-in/src/config"
	"github.com/AdriaanVE/jack-in/src/tmux"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(sendCmd)
}

var sendCmd = &cobra.Command{
	Use:   "send <worker> <message>",
	Short: "Send a message to a worker's tmux pane",
	Args:  cobra.MinimumNArgs(2),
	RunE:  runSend,
}

func runSend(cmd *cobra.Command, args []string) error {
	base := cwd()
	cfg, err := loadConfig(base)
	if err != nil {
		return err
	}
	session := config.SessionName(cfg.Project)

	workerName := args[0]
	if !hasWorker(cfg, workerName) {
		return fmt.Errorf("worker %q not found; available: %s", workerName, workerNames(cfg))
	}
	if !tmux.HasSession(session) {
		return fmt.Errorf("session '%s' is not running; run 'jackin up' first", session)
	}

	message := strings.Join(args[1:], " ")
	target := tmux.PaneTarget(session, workerName)
	if err := tmux.SendKeys(target, message, true); err != nil {
		return fmt.Errorf("sending to %s: %w", workerName, err)
	}

	fmt.Printf("Sent to %s.\n", workerName)
	return nil
}

func hasWorker(cfg *config.Config, name string) bool {
	for _, w := range cfg.Workers {
		if w.Name == name {
			return true
		}
	}
	return false
}

func workerNames(cfg *config.Config) string {
	return strings.Join(workerNamesSlice(cfg), ", ")
}

func workerNamesSlice(cfg *config.Config) []string {
	names := make([]string, len(cfg.Workers))
	for i, w := range cfg.Workers {
		names[i] = w.Name
	}
	return names
}
