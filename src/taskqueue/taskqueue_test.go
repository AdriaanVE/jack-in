package taskqueue

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/AdriaanVE/jack-in/src/domain"
)

func tmpBase(t *testing.T) string {
	t.Helper()
	return t.TempDir()
}

func TestInitCreatesDirectories(t *testing.T) {
	base := tmpBase(t)
	if err := Init(base); err != nil {
		t.Fatal(err)
	}
	for _, s := range domain.TaskStates {
		dir := filepath.Join(base, tasksDir, string(s))
		info, err := os.Stat(dir)
		if err != nil {
			t.Fatalf("expected dir %s to exist: %v", s, err)
		}
		if !info.IsDir() {
			t.Fatalf("expected %s to be a directory", s)
		}
	}
}

func TestAddAndGet(t *testing.T) {
	base := tmpBase(t)
	Init(base)

	task, err := Add(base, "Fix login bug", "Users can't log in with SSO")
	if err != nil {
		t.Fatal(err)
	}
	if task.Summary != "Fix login bug" {
		t.Fatalf("expected summary 'Fix login bug', got %q", task.Summary)
	}
	if task.Description != "Users can't log in with SSO" {
		t.Fatalf("expected description, got %q", task.Description)
	}
	if task.Retries != 0 {
		t.Fatalf("expected retries=0, got %d", task.Retries)
	}

	entry, err := Get(base, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if entry == nil {
		t.Fatal("expected task entry, got nil")
	}
	if entry.State != domain.StatePending {
		t.Fatalf("expected state pending, got %s", entry.State)
	}
	if entry.Task.Summary != "Fix login bug" {
		t.Fatalf("expected summary match, got %q", entry.Task.Summary)
	}
}

func TestAddDefaultsDescriptionToSummary(t *testing.T) {
	base := tmpBase(t)
	Init(base)

	task, err := Add(base, "Quick fix", "")
	if err != nil {
		t.Fatal(err)
	}
	if task.Description != "Quick fix" {
		t.Fatalf("expected description to default to summary, got %q", task.Description)
	}
}

func TestGetNotFound(t *testing.T) {
	base := tmpBase(t)
	Init(base)

	entry, err := Get(base, "nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	if entry != nil {
		t.Fatalf("expected nil for nonexistent task, got %+v", entry)
	}
}

func TestListByState(t *testing.T) {
	base := tmpBase(t)
	Init(base)
	Add(base, "Task A", "")
	Add(base, "Task B", "")

	entries, err := List(base, domain.StatePending)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 pending tasks, got %d", len(entries))
	}

	entries, err = List(base, domain.StateCurrent)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected 0 current tasks, got %d", len(entries))
	}
}

func TestListAll(t *testing.T) {
	base := tmpBase(t)
	Init(base)
	Add(base, "Task A", "")
	Add(base, "Task B", "")

	entries, err := ListAll(base)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 total tasks, got %d", len(entries))
	}
}

func TestCounts(t *testing.T) {
	base := tmpBase(t)
	Init(base)
	Add(base, "Task A", "")
	Add(base, "Task B", "")
	Add(base, "Task C", "")

	c, err := Counts(base)
	if err != nil {
		t.Fatal(err)
	}
	if c.Pending != 3 {
		t.Fatalf("expected 3 pending, got %d", c.Pending)
	}
	if c.Current != 0 || c.Review != 0 || c.Complete != 0 || c.Rejected != 0 {
		t.Fatalf("expected all other counts=0, got %+v", c)
	}
}

func TestFullLifecycle(t *testing.T) {
	base := tmpBase(t)
	Init(base)

	task, _ := Add(base, "Implement feature", "")

	// Claim (pending -> current)
	claimed, err := Claim(base, task.ID, "scout")
	if err != nil {
		t.Fatal(err)
	}
	if claimed.Assignee != "scout" {
		t.Fatalf("expected assignee 'scout', got %q", claimed.Assignee)
	}
	entry, _ := Get(base, task.ID)
	if entry.State != domain.StateCurrent {
		t.Fatalf("expected current, got %s", entry.State)
	}

	// Review (current -> review)
	_, err = Review(base, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	entry, _ = Get(base, task.ID)
	if entry.State != domain.StateReview {
		t.Fatalf("expected review, got %s", entry.State)
	}

	// Approve (review -> complete)
	_, err = Approve(base, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	entry, _ = Get(base, task.ID)
	if entry.State != domain.StateComplete {
		t.Fatalf("expected complete, got %s", entry.State)
	}
}

func TestRejectAndRetry(t *testing.T) {
	base := tmpBase(t)
	Init(base)

	task, _ := Add(base, "Write docs", "")
	Claim(base, task.ID, "coder")
	Review(base, task.ID)

	// Reject (review -> rejected)
	rejected, err := Reject(base, task.ID, "missing examples")
	if err != nil {
		t.Fatal(err)
	}
	if rejected.Feedback != "missing examples" {
		t.Fatalf("expected feedback, got %q", rejected.Feedback)
	}
	entry, _ := Get(base, task.ID)
	if entry.State != domain.StateRejected {
		t.Fatalf("expected rejected, got %s", entry.State)
	}

	// Retry (rejected -> current, keeping assignee and feedback for iteration)
	retried, err := Retry(base, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if retried.Retries != 1 {
		t.Fatalf("expected retries=1, got %d", retried.Retries)
	}
	if retried.Assignee != "coder" {
		t.Fatalf("expected assignee kept as 'coder', got %q", retried.Assignee)
	}
	if retried.Feedback != "missing examples" {
		t.Fatalf("expected feedback kept, got %q", retried.Feedback)
	}
	entry, _ = Get(base, task.ID)
	if entry.State != domain.StateCurrent {
		t.Fatalf("expected current after retry, got %s", entry.State)
	}
}

func TestRetryMaxLimit(t *testing.T) {
	base := tmpBase(t)
	Init(base)

	task, _ := Add(base, "Flaky task", "")
	Claim(base, task.ID, "coder")

	// Retry MaxRetries times (should all succeed)
	for i := 0; i < MaxRetries; i++ {
		Review(base, task.ID)
		Reject(base, task.ID, "try again")
		_, err := Retry(base, task.ID)
		if err != nil {
			t.Fatalf("retry %d should succeed, got error: %v", i+1, err)
		}
	}

	// One more retry should fail
	Review(base, task.ID)
	Reject(base, task.ID, "final rejection")
	_, err := Retry(base, task.ID)
	if err != ErrMaxRetriesExceeded {
		t.Fatalf("expected ErrMaxRetriesExceeded, got: %v", err)
	}

	// Task should stay in rejected
	entry, _ := Get(base, task.ID)
	if entry.State != domain.StateRejected {
		t.Fatalf("expected rejected after max retries, got %s", entry.State)
	}
}

func TestUnclaim(t *testing.T) {
	base := tmpBase(t)
	Init(base)

	task, _ := Add(base, "Task X", "")
	Claim(base, task.ID, "worker1")

	unclaimed, err := Unclaim(base, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if unclaimed.Assignee != "" {
		t.Fatalf("expected assignee cleared, got %q", unclaimed.Assignee)
	}
	entry, _ := Get(base, task.ID)
	if entry.State != domain.StatePending {
		t.Fatalf("expected pending after unclaim, got %s", entry.State)
	}
}

func TestReadyRespectsDependencies(t *testing.T) {
	base := tmpBase(t)
	Init(base)

	taskA, _ := Add(base, "Task A", "")
	taskB, _ := Add(base, "Task B", "")

	// Manually add depends_on to taskB
	entry, _ := Get(base, taskB.ID)
	entry.Task.DependsOn = []string{taskA.ID}
	writeTask(base, domain.StatePending, &entry.Task)

	// Only A should be ready (B depends on A)
	ready, err := Ready(base)
	if err != nil {
		t.Fatal(err)
	}
	if len(ready) != 1 {
		t.Fatalf("expected 1 ready task, got %d", len(ready))
	}
	if ready[0].ID != taskA.ID {
		t.Fatalf("expected task A to be ready, got %s", ready[0].ID)
	}

	// Complete A, now B should be ready
	Claim(base, taskA.ID, "w1")
	Review(base, taskA.ID)
	Approve(base, taskA.ID)

	ready, err = Ready(base)
	if err != nil {
		t.Fatal(err)
	}
	if len(ready) != 1 {
		t.Fatalf("expected 1 ready task after completing A, got %d", len(ready))
	}
	if ready[0].ID != taskB.ID {
		t.Fatalf("expected task B to be ready, got %s", ready[0].ID)
	}
}

func TestReadyMatchesByIDAndSummary(t *testing.T) {
	base := tmpBase(t)
	Init(base)

	taskA, _ := Add(base, "Build API", "")
	// Depend by summary instead of ID
	taskB, _ := Add(base, "Test API", "")
	entry, _ := Get(base, taskB.ID)
	entry.Task.DependsOn = []string{"Build API"}
	writeTask(base, domain.StatePending, &entry.Task)

	// B blocked
	ready, _ := Ready(base)
	if len(ready) != 1 || ready[0].ID != taskA.ID {
		t.Fatalf("expected only A ready, got %v", ready)
	}

	// Complete A
	Claim(base, taskA.ID, "w1")
	Review(base, taskA.ID)
	Approve(base, taskA.ID)

	// B unblocked via summary match
	ready, _ = Ready(base)
	if len(ready) != 1 || ready[0].ID != taskB.ID {
		t.Fatalf("expected B ready after summary match, got %v", ready)
	}
}

func TestSeedSkipsDuplicates(t *testing.T) {
	base := tmpBase(t)

	seeds := []TaskSeed{
		{Summary: "Task One", Description: "First"},
		{Summary: "Task Two", Description: "Second"},
	}

	n, err := Seed(base, seeds)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("expected 2 seeded, got %d", n)
	}

	// Seed again — should skip both
	n, err = Seed(base, seeds)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("expected 0 seeded on re-seed, got %d", n)
	}

	entries, _ := ListAll(base)
	if len(entries) != 2 {
		t.Fatalf("expected 2 total tasks, got %d", len(entries))
	}
}

func TestSeedResolvesDependencies(t *testing.T) {
	base := tmpBase(t)

	seeds := []TaskSeed{
		{Summary: "Foundation"},
		{Summary: "Feature", DependsOn: []string{"Foundation"}},
	}

	_, err := Seed(base, seeds)
	if err != nil {
		t.Fatal(err)
	}

	entries, _ := List(base, domain.StatePending)
	var feature *Task
	var foundation *Task
	for i := range entries {
		if entries[i].Task.Summary == "Feature" {
			feature = &entries[i].Task
		}
		if entries[i].Task.Summary == "Foundation" {
			foundation = &entries[i].Task
		}
	}
	if feature == nil || foundation == nil {
		t.Fatal("expected both tasks to exist")
	}
	if len(feature.DependsOn) != 1 || feature.DependsOn[0] != foundation.ID {
		t.Fatalf("expected depends_on resolved to foundation ID %s, got %v", foundation.ID, feature.DependsOn)
	}
}
