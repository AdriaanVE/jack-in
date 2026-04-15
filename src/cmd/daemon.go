package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"

	tea "charm.land/bubbletea/v2"
	"github.com/AdriaanVE/jack-in/src/agent"
	"github.com/AdriaanVE/jack-in/src/config"
	"github.com/AdriaanVE/jack-in/src/daemon"
	"github.com/AdriaanVE/jack-in/src/domain"
	"github.com/AdriaanVE/jack-in/src/ui"
	"github.com/AdriaanVE/jack-in/src/worktree"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func init() {
	daemonCmd.Flags().String("approval", "", "override approval mode (manual, auto, yolo)")
	rootCmd.AddCommand(daemonCmd)
}

var daemonCmd = &cobra.Command{
	Use:    "daemon",
	Short:  "Run the daemon loop (used internally by jackin up)",
	Hidden: true,
	RunE:   runDaemon,
}

func runDaemon(cmd *cobra.Command, args []string) error {
	base := cwd()
	cfg, err := loadConfig(base)
	if err != nil {
		return err
	}

	if approvalFlag, _ := cmd.Flags().GetString("approval"); approvalFlag != "" {
		cfg.Orchestrator.Approval = domain.ApprovalMode(approvalFlag)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	go func() {
		<-sigCh
		cancel()
	}()

	// If running in a TTY, launch the TUI dashboard
	if term.IsTerminal(int(os.Stdin.Fd())) {
		return runDaemonWithTUI(ctx, cfg, base)
	}

	return daemon.Run(ctx, daemon.Options{
		Config: cfg,
		Base:   base,
	})
}

func runDaemonWithTUI(ctx context.Context, cfg *config.Config, base string) error {
	events := make(chan any, 64)
	session := config.SessionName(cfg.Project)
	approval := cfg.Orchestrator.Approval

	// Build worker init list for TUI cards
	var orchAgent domain.AgentType
	var orchSpawnCmd string
	if cfg.Orchestrator.OrchestratorEnabled() {
		orchAgent = domain.AgentType(cfg.Orchestrator.Agent)
		promptFile := filepath.Join(base, ".jack-in", "orchestrator-prompt.md")
		orchSpawnCmd = fmt.Sprintf("cd %s && %s", agent.ShellEscape(base), agent.InitCommand(orchAgent, promptFile))
	}
	workers := make([]ui.WorkerInit, 0, len(cfg.Workers))
	for _, w := range cfg.Workers {
		if w.Role == domain.RoleExecutor {
			wt := worktree.Path(base, cfg.Project, w.Name)
			workers = append(workers, ui.WorkerInit{
				Name:     w.Name,
				Agent:    w.Agent,
				SpawnCmd: workerShellCmd(&w, wt, cfg.StartupInstructions),
			})
		}
	}

	// Run daemon in background goroutine; daemon sends domain.StateEvent
	// and domain.LogEvent directly on the events channel.
	go func() {
		_ = daemon.Run(ctx, daemon.Options{
			Config: cfg,
			Base:   base,
			Events: events,
		})
		close(events)
	}()

	// Run TUI in main goroutine (Bubbletea needs main thread)
	model := ui.NewModel(session, base, approval, orchAgent, orchSpawnCmd, workers, events)
	p := tea.NewProgram(model)
	_, err := p.Run()
	return err
}
