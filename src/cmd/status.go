package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/AdriaanVE/jack-in/src/config"
	"github.com/AdriaanVE/jack-in/src/daemon"
	"github.com/AdriaanVE/jack-in/src/domain"
	"github.com/AdriaanVE/jack-in/src/taskqueue"
	"github.com/AdriaanVE/jack-in/src/tmux"
	"github.com/AdriaanVE/jack-in/src/worktree"
	"github.com/spf13/cobra"
)

var statusJSON bool

func init() {
	statusCmd.Flags().BoolVar(&statusJSON, "json", false, "Output as JSON")
	rootCmd.AddCommand(statusCmd)
}

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show worker, daemon, and task status",
	RunE:  runStatus,
}

// shells that indicate a worker process has exited back to shell.
var shells = map[string]bool{"bash": true, "zsh": true, "fish": true, "sh": true}

type workerStatus struct {
	Name   string `json:"name"`
	Agent  string `json:"agent"`
	State  string `json:"state"`
	Task   string `json:"task,omitempty"`
	Branch string `json:"branch,omitempty"`
}

type statusOutput struct {
	Session        string            `json:"session"`
	Uptime         string            `json:"uptime,omitempty"`
	Approval       string            `json:"approval"`
	ApprovalSource string            `json:"approval_source"`
	Daemon         bool              `json:"daemon"`
	Workers        []workerStatus    `json:"workers"`
	Tasks          domain.TaskCounts `json:"tasks"`
}

func runStatus(cmd *cobra.Command, args []string) error {
	base := cwd()
	cfg, err := loadConfig(base)
	if err != nil {
		return err
	}
	session := config.SessionName(cfg.Project)

	if !tmux.HasSession(session) {
		if statusJSON {
			out, _ := json.MarshalIndent(statusOutput{Session: session}, "", "  ")
			fmt.Println(string(out))
			return nil
		}
		fmt.Printf("Session '%s' is not running. Run 'jackin up' first.\n", session)
		return nil
	}

	panes, err := tmux.ListPanes(session)
	if err != nil {
		return fmt.Errorf("listing panes: %w", err)
	}
	paneByWindow := make(map[string]tmux.PaneInfo)
	for _, p := range panes {
		paneByWindow[p.WindowName] = p
	}

	// Detect daemon: pane 0 of dashboard-orchestrator running a non-shell command.
	// Target pane 0 explicitly because the window has two panes (daemon + orchestrator)
	// and the paneByWindow map only retains the last pane per window name.
	daemonRunning := false
	for _, p := range panes {
		if p.WindowName == "dashboard-orchestrator" && p.PaneIndex == 0 {
			daemonRunning = !p.PaneDead && p.CurrentCommand != "" && !shells[p.CurrentCommand]
			break
		}
	}

	var uptimeStr string
	if created, err := tmux.SessionCreated(session); err == nil {
		uptimeStr = time.Since(time.Unix(created, 0)).Truncate(time.Second).String()
	}

	approval, approvalSource := resolveApproval(base, cfg)

	counts, countsErr := taskqueue.Counts(base)
	if countsErr != nil {
		fmt.Fprintf(os.Stderr, "warning: reading task counts: %v\n", countsErr)
	}

	workers := make([]workerStatus, 0, len(cfg.Workers))
	for _, w := range cfg.Workers {
		ws := workerStatus{
			Name:   w.Name,
			Agent:  string(w.Agent),
			Branch: worktree.BranchName(cfg.Project, w.Name),
		}

		p, paneExists := paneByWindow[w.Name]
		switch {
		case !paneExists:
			ws.State = "gone"
		case p.PaneDead || shells[p.CurrentCommand]:
			ws.State = "stopped"
		default:
			taskData, err := os.ReadFile(daemon.CurrentTaskFilePath(base, w.Name))
			if err == nil && len(taskData) > 0 {
				ws.State = "working"
				ws.Task = string(taskData)
			} else {
				ws.State = "waiting"
			}
		}

		workers = append(workers, ws)
	}

	out := statusOutput{
		Session:        session,
		Uptime:         uptimeStr,
		Approval:       string(approval),
		ApprovalSource: approvalSource,
		Daemon:         daemonRunning,
		Workers:        workers,
		Tasks:          counts,
	}

	if statusJSON {
		b, _ := json.MarshalIndent(out, "", "  ")
		fmt.Println(string(b))
		return nil
	}

	// Human-readable output
	fmt.Printf("Session:  %s", session)
	if uptimeStr != "" {
		fmt.Printf("  (up %s)", uptimeStr)
	}
	fmt.Println()

	fmt.Printf("Daemon:   %s\n", boolLabel(daemonRunning, "running", "stopped"))
	approvalLabel := string(approval)
	if approval == domain.ApprovalAuto {
		model := os.Getenv("ANTHROPIC_DEFAULT_SONNET_MODEL")
		if model == "" {
			model = "claude-sonnet-4-5"
		}
		approvalLabel += " (" + model + ")"
	}
	fmt.Printf("Approval: %s (%s)\n", approvalLabel, approvalSource)

	fmt.Println()
	fmt.Println("Workers:")
	for _, w := range workers {
		line := fmt.Sprintf("  %-12s %-10s %s", w.Name, w.Agent, w.State)
		if w.Task != "" {
			line += "  task=" + w.Task
		}
		fmt.Println(line)
	}

	fmt.Println()
	fmt.Printf("Tasks: %d pending, %d current, %d review, %d complete, %d rejected\n",
		counts.Pending, counts.Current, counts.Review, counts.Complete, counts.Rejected)

	return nil
}

func boolLabel(b bool, trueLabel, falseLabel string) string {
	if b {
		return trueLabel
	}
	return falseLabel
}
