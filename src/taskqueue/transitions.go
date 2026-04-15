package taskqueue

import (
	"fmt"
	"os"
	"time"

	"github.com/AdriaanVE/jack-in/src/domain"
)

// moveTask reads a task from one state, optionally patches it, writes to the
// destination state, then removes the source. Write-before-delete: a crash
// between the two ops leaves a duplicate rather than losing the task.
func moveTask(base, id string, from, to domain.TaskState, patch func(*Task)) (*Task, error) {
	src := taskPath(base, from, id)
	task, err := readTask(src)
	if err != nil {
		return nil, fmt.Errorf("reading task %s from %s: %w", id, from, err)
	}
	return moveTaskPreread(base, task, from, to, patch)
}

// moveTaskPreread moves a pre-read task to a new state, avoiding a second file read.
func moveTaskPreread(base string, task *Task, from, to domain.TaskState, patch func(*Task)) (*Task, error) {
	src := taskPath(base, from, task.ID)
	task.TransitionedAt = time.Now()
	if patch != nil {
		patch(task)
	}
	if err := writeTask(base, to, task); err != nil {
		return nil, fmt.Errorf("writing task %s to %s: %w", task.ID, to, err)
	}
	if err := os.Remove(src); err != nil {
		return nil, fmt.Errorf("removing task %s from %s: %w", task.ID, from, err)
	}
	return task, nil
}

// Claim moves a pending task to current and assigns it to a worker.
func Claim(base, taskID, assignee string) (*Task, error) {
	if err := ValidateTaskID(taskID); err != nil {
		return nil, err
	}
	return moveTask(base, taskID, domain.StatePending, domain.StateCurrent, func(t *Task) {
		t.Assignee = assignee
	})
}

// Review moves a current task to review.
func Review(base, taskID string) (*Task, error) {
	if err := ValidateTaskID(taskID); err != nil {
		return nil, err
	}
	return moveTask(base, taskID, domain.StateCurrent, domain.StateReview, nil)
}

// Approve moves a reviewed task to complete.
func Approve(base, taskID string) (*Task, error) {
	if err := ValidateTaskID(taskID); err != nil {
		return nil, err
	}
	return moveTask(base, taskID, domain.StateReview, domain.StateComplete, nil)
}

// Reject moves a reviewed task to rejected with feedback.
func Reject(base, taskID, feedback string) (*Task, error) {
	if err := ValidateTaskID(taskID); err != nil {
		return nil, err
	}
	return moveTask(base, taskID, domain.StateReview, domain.StateRejected, func(t *Task) {
		t.Feedback = feedback
	})
}

// MaxRetries is the maximum number of times a task can be retried.
const MaxRetries = 3

// ErrMaxRetriesExceeded is returned when a task has been retried too many times.
var ErrMaxRetriesExceeded = fmt.Errorf("task has reached maximum retries (%d)", MaxRetries)

// Retry moves a rejected task back to current for the same worker to fix.
// Keeps the assignee and feedback so the worker knows what to address.
// Returns ErrMaxRetriesExceeded if the task has already been retried MaxRetries times.
func Retry(base, taskID string) (*Task, error) {
	if err := ValidateTaskID(taskID); err != nil {
		return nil, err
	}
	// Check current retry count before moving
	src := taskPath(base, domain.StateRejected, taskID)
	task, err := readTask(src)
	if err != nil {
		return nil, fmt.Errorf("reading task %s: %w", taskID, err)
	}
	if task.Retries >= MaxRetries {
		return nil, ErrMaxRetriesExceeded
	}

	return moveTaskPreread(base, task, domain.StateRejected, domain.StateCurrent, func(t *Task) {
		t.Retries++
		// Keep Assignee and Feedback so worker can iterate on the fix
	})
}

// Unclaim moves a current task back to pending, clearing its assignee.
func Unclaim(base, taskID string) (*Task, error) {
	if err := ValidateTaskID(taskID); err != nil {
		return nil, err
	}
	return moveTask(base, taskID, domain.StateCurrent, domain.StatePending, func(t *Task) {
		t.Assignee = ""
	})
}

// Drop permanently deletes a rejected task.
func Drop(base, taskID string) error {
	if err := ValidateTaskID(taskID); err != nil {
		return err
	}
	path := taskPath(base, domain.StateRejected, taskID)
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("removing rejected task %s: %w", taskID, err)
	}
	return nil
}

// Cancel removes a task from the current state and returns the assignee.
// Use this when a user wants to stop an in-progress task.
func Cancel(base, taskID string) (assignee string, err error) {
	if err := ValidateTaskID(taskID); err != nil {
		return "", err
	}
	path := taskPath(base, domain.StateCurrent, taskID)
	task, err := readTask(path)
	if err != nil {
		return "", fmt.Errorf("reading current task %s: %w", taskID, err)
	}
	assignee = task.Assignee
	if err := os.Remove(path); err != nil {
		return "", fmt.Errorf("removing current task %s: %w", taskID, err)
	}
	return assignee, nil
}
