package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/AdriaanVE/jack-in/src/daemon"
	"github.com/AdriaanVE/jack-in/src/domain"
	"github.com/AdriaanVE/jack-in/src/taskqueue"
	"github.com/spf13/cobra"
)

func init() {
	tasksAddCmd.Flags().String("desc", "", "task description")
	tasksCancelCmd.Flags().Bool("drop", false, "permanently delete instead of returning to pending")
	tasksValidateCmd.Flags().Bool("fix", false, "automatically fix detected issues (keeps most recent state)")
	tasksCmd.AddCommand(tasksInitCmd, tasksAddCmd, tasksCompleteCmd, tasksApproveCmd, tasksRejectCmd, tasksRetryCmd, tasksDropCmd, tasksCancelCmd, tasksValidateCmd)
	rootCmd.AddCommand(tasksCmd)
}

var tasksCmd = &cobra.Command{
	Use:   "tasks",
	Short: "List all tasks grouped by state",
	RunE: func(cmd *cobra.Command, args []string) error {
		base := cwd()
		entries, err := taskqueue.ListAll(base)
		if err != nil {
			return err
		}
		if len(entries) == 0 {
			fmt.Println("No tasks. Run 'jackin tasks init' to create the queue.")
			return nil
		}

		grouped := make(map[domain.TaskState][]taskqueue.TaskEntry)
		for _, e := range entries {
			grouped[e.State] = append(grouped[e.State], e)
		}

		for _, s := range domain.TaskStates {
			tasks := grouped[s]
			if len(tasks) == 0 {
				continue
			}
			fmt.Printf("\n%s (%d)\n", strings.ToUpper(string(s)), len(tasks))
			for _, e := range tasks {
				line := fmt.Sprintf("  %s  %s", e.Task.ID, e.Task.Summary)
				if e.Task.Assignee != "" {
					line += fmt.Sprintf("  [%s]", e.Task.Assignee)
				}
				fmt.Println(line)
			}
		}
		fmt.Println()
		return nil
	},
}

var tasksInitCmd = &cobra.Command{
	Use:   "init",
	Short: "Create task queue directories",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := taskqueue.Init(cwd()); err != nil {
			return err
		}
		fmt.Println("Task queue initialized.")
		return nil
	},
}

var tasksAddCmd = &cobra.Command{
	Use:   "add <summary>",
	Short: "Add a task to the pending queue",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		desc, _ := cmd.Flags().GetString("desc")
		task, err := taskqueue.Add(cwd(), args[0], desc)
		if err != nil {
			return err
		}
		fmt.Printf("Created %s: %s\n", task.ID, task.Summary)
		return nil
	},
}

var tasksCompleteCmd = &cobra.Command{
	Use:   "complete <id>",
	Short: "Force a current task to review",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		task, err := taskqueue.Review(cwd(), args[0])
		if err != nil {
			return err
		}
		fmt.Printf("Moved %s to review.\n", task.ID)
		return nil
	},
}

var tasksApproveCmd = &cobra.Command{
	Use:   "approve <id>",
	Short: "Approve a reviewed task",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		task, err := taskqueue.Approve(cwd(), args[0])
		if err != nil {
			return err
		}
		fmt.Printf("Approved %s.\n", task.ID)
		return nil
	},
}

var tasksRejectCmd = &cobra.Command{
	Use:   "reject <id> <feedback>",
	Short: "Reject a reviewed task with feedback",
	Args:  cobra.MinimumNArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		feedback := strings.Join(args[1:], " ")
		task, err := taskqueue.Reject(cwd(), args[0], feedback)
		if err != nil {
			return err
		}
		fmt.Printf("Rejected %s: %s\n", task.ID, task.Feedback)
		return nil
	},
}

var tasksRetryCmd = &cobra.Command{
	Use:   "retry <id>",
	Short: "Move a rejected task back to pending",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		task, err := taskqueue.Retry(cwd(), args[0])
		if err != nil {
			return err
		}
		fmt.Printf("Retrying %s (attempt %d).\n", task.ID, task.Retries+1)
		return nil
	},
}

var tasksDropCmd = &cobra.Command{
	Use:   "drop <id>",
	Short: "Permanently delete a rejected task",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := taskqueue.Drop(cwd(), args[0]); err != nil {
			return err
		}
		fmt.Printf("Dropped %s.\n", args[0])
		return nil
	},
}

var tasksValidateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Check task queue integrity (detect duplicates)",
	RunE: func(cmd *cobra.Command, args []string) error {
		base := cwd()
		issues, err := taskqueue.Validate(base)
		if err != nil {
			return err
		}
		if len(issues) == 0 {
			fmt.Println("Task queue is valid.")
			return nil
		}
		fmt.Printf("Found %d issue(s):\n", len(issues))
		for _, issue := range issues {
			fmt.Printf("  - %s\n", issue)
		}

		fix, _ := cmd.Flags().GetBool("fix")
		if fix {
			fmt.Println("\nAttempting to fix issues...")
			fixed, err := taskqueue.FixDuplicates(base)
			if err != nil {
				return fmt.Errorf("fix failed: %w", err)
			}
			fmt.Printf("Fixed %d duplicate(s).\n", fixed)
		} else {
			fmt.Println("\nRun with --fix to automatically resolve duplicates.")
		}
		return nil
	},
}

var tasksCancelCmd = &cobra.Command{
	Use:   "cancel <id>",
	Short: "Cancel an in-progress task (notifies worker to stop)",
	Long: `Cancel an in-progress task. The daemon will:
1. Notify the worker to stop working
2. Move the task back to pending (default) or delete it (--drop)

By default, cancelled tasks return to pending for reassignment.
Use --drop to permanently delete the task instead.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		base := cwd()
		taskID := args[0]
		drop, _ := cmd.Flags().GetBool("drop")

		// Find the task and check its state
		entry, err := taskqueue.Get(base, taskID)
		if err != nil {
			return err
		}
		if entry == nil {
			return fmt.Errorf("task %s not found", taskID)
		}

		switch entry.State {
		case domain.StateCurrent:
			// Write cancel signal - daemon handles all state transitions
			// This avoids race conditions where task gets reassigned before worker stops
			assignee := entry.Task.Assignee
			if assignee == "" {
				return fmt.Errorf("task %s has no assignee", taskID)
			}

			action := "pending" // default: return to queue
			if drop {
				action = "drop"
			}
			if err := daemon.WriteCancelSignal(base, assignee, taskID+":"+action); err != nil {
				return fmt.Errorf("writing cancel signal: %w", err)
			}
			if drop {
				fmt.Printf("Cancel requested for %s (will be deleted). Daemon will notify worker %s.\n", taskID, assignee)
			} else {
				fmt.Printf("Cancel requested for %s (will return to pending). Daemon will notify worker %s.\n", taskID, assignee)
			}

		case domain.StatePending:
			// Just delete pending tasks (no worker to notify)
			path := taskqueue.TaskFilePath(base, domain.StatePending, taskID)
			if err := os.Remove(path); err != nil {
				return fmt.Errorf("removing pending task: %w", err)
			}
			fmt.Printf("Removed pending task %s.\n", taskID)

		case domain.StateReview:
			return fmt.Errorf("task %s is in review - use 'reject' or 'approve' instead", taskID)

		case domain.StateComplete:
			return fmt.Errorf("task %s is already complete", taskID)

		case domain.StateRejected:
			return fmt.Errorf("task %s is rejected - use 'drop' or 'retry' instead", taskID)
		}

		return nil
	},
}
