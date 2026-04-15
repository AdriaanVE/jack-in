package taskqueue

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"time"

	"github.com/AdriaanVE/jack-in/src/domain"
)

// safeTaskID matches valid task IDs: alphanumeric, hyphens, underscores, dots.
// Prevents path traversal via crafted task IDs passed through CLI args.
var safeTaskID = regexp.MustCompile(`^[a-zA-Z0-9_.-]+$`)

// ValidateTaskID checks that a task ID is safe for use in file paths.
func ValidateTaskID(id string) error {
	if id == "" {
		return fmt.Errorf("empty task ID")
	}
	if !safeTaskID.MatchString(id) {
		return fmt.Errorf("invalid task ID %q: must be alphanumeric, hyphens, underscores, dots", id)
	}
	return nil
}

const tasksDir = ".jack-in/tasks"

var idCounter atomic.Int64

// Task represents a unit of work in the queue.
type Task struct {
	ID             string    `json:"id"`
	Summary        string    `json:"summary"`
	Description    string    `json:"description"`
	Files          []string  `json:"files,omitempty"`
	Acceptance     []string  `json:"acceptance,omitempty"`
	DependsOn      []string  `json:"depends_on,omitempty"`
	Assignee       string    `json:"assignee,omitempty"`
	Feedback       string    `json:"feedback,omitempty"`
	CreatedBy      string    `json:"created_by,omitempty"`
	Retries        int       `json:"retries"`
	TransitionedAt time.Time `json:"transitioned_at,omitempty"`
}

// TaskEntry pairs a task with its current state.
type TaskEntry struct {
	Task  Task
	State domain.TaskState
}

// GenerateID creates a unique task ID.
func GenerateID() string {
	return fmt.Sprintf("task-%d-%d", time.Now().UnixMilli(), idCounter.Add(1)-1)
}

func stateDir(base string, state domain.TaskState) string {
	return filepath.Join(base, tasksDir, string(state))
}

func taskPath(base string, state domain.TaskState, id string) string {
	return filepath.Join(base, tasksDir, string(state), id+".json")
}

// TaskFilePath returns the file path for a task in a given state (exported for CLI use).
func TaskFilePath(base string, state domain.TaskState, id string) string {
	return taskPath(base, state, id)
}

// Init creates the task queue directory structure.
func Init(base string) error {
	for _, s := range domain.TaskStates {
		if err := os.MkdirAll(stateDir(base, s), 0o755); err != nil {
			return fmt.Errorf("creating task dir %s: %w", s, err)
		}
	}
	return nil
}

// Add creates a new task in the pending state.
func Add(base string, summary, description string) (*Task, error) {
	if description == "" {
		description = summary
	}
	task := Task{
		ID:             GenerateID(),
		Summary:        summary,
		Description:    description,
		TransitionedAt: time.Now(),
	}
	if err := writeTask(base, domain.StatePending, &task); err != nil {
		return nil, err
	}
	return &task, nil
}

// Get finds a task by ID, searching all states.
func Get(base string, taskID string) (*TaskEntry, error) {
	if err := ValidateTaskID(taskID); err != nil {
		return nil, err
	}
	for _, s := range domain.TaskStates {
		task, err := readTask(taskPath(base, s, taskID))
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		return &TaskEntry{Task: *task, State: s}, nil
	}
	return nil, nil
}

// List returns tasks in a given state.
func List(base string, state domain.TaskState) ([]TaskEntry, error) {
	return listStates(base, []domain.TaskState{state})
}

// ListAll returns tasks across all states.
func ListAll(base string) ([]TaskEntry, error) {
	return listStates(base, domain.TaskStates)
}

func listStates(base string, states []domain.TaskState) ([]TaskEntry, error) {
	var entries []TaskEntry
	for _, s := range states {
		dir := stateDir(base, s)
		dirEntries, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		for _, de := range dirEntries {
			if de.IsDir() || !strings.HasSuffix(de.Name(), ".json") {
				continue
			}
			task, err := readTask(filepath.Join(dir, de.Name()))
			if err != nil {
				return nil, err
			}
			entries = append(entries, TaskEntry{Task: *task, State: s})
		}
	}
	return entries, nil
}

// Counts returns the number of tasks in each state without parsing JSON.
func Counts(base string) (domain.TaskCounts, error) {
	var c domain.TaskCounts
	for _, s := range domain.TaskStates {
		dir := stateDir(base, s)
		dirEntries, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return c, err
		}
		n := 0
		for _, de := range dirEntries {
			if !de.IsDir() && strings.HasSuffix(de.Name(), ".json") {
				n++
			}
		}
		switch s {
		case domain.StatePending:
			c.Pending = n
		case domain.StateCurrent:
			c.Current = n
		case domain.StateReview:
			c.Review = n
		case domain.StateComplete:
			c.Complete = n
		case domain.StateRejected:
			c.Rejected = n
		}
	}
	return c, nil
}

// Validate checks for duplicate task files across states and returns a list of issues.
// Duplicates can occur if a transition write succeeds but the source removal fails.
func Validate(base string) ([]string, error) {
	var issues []string
	taskStates := make(map[string][]domain.TaskState)

	for _, s := range domain.TaskStates {
		dir := stateDir(base, s)
		dirEntries, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		for _, de := range dirEntries {
			if de.IsDir() || !strings.HasSuffix(de.Name(), ".json") {
				continue
			}
			taskID := strings.TrimSuffix(de.Name(), ".json")
			taskStates[taskID] = append(taskStates[taskID], s)
		}
	}

	for taskID, states := range taskStates {
		if len(states) > 1 {
			stateNames := make([]string, len(states))
			for i, s := range states {
				stateNames[i] = string(s)
			}
			issues = append(issues, fmt.Sprintf("task %s exists in multiple states: %v", taskID, stateNames))
		}
	}
	return issues, nil
}

// FixDuplicates removes stale duplicate task files, keeping the most recent state.
// Returns the number of duplicates fixed.
func FixDuplicates(base string) (int, error) {
	taskStates := make(map[string][]domain.TaskState)

	for _, s := range domain.TaskStates {
		dir := stateDir(base, s)
		dirEntries, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return 0, err
		}
		for _, de := range dirEntries {
			if de.IsDir() || !strings.HasSuffix(de.Name(), ".json") {
				continue
			}
			taskID := strings.TrimSuffix(de.Name(), ".json")
			taskStates[taskID] = append(taskStates[taskID], s)
		}
	}

	fixed := 0
	// State priority: later in the lifecycle wins (rejected > complete > review > current > pending)
	statePriority := map[domain.TaskState]int{
		domain.StatePending:  0,
		domain.StateCurrent:  1,
		domain.StateReview:   2,
		domain.StateComplete: 3,
		domain.StateRejected: 4,
	}

	for taskID, states := range taskStates {
		if len(states) <= 1 {
			continue
		}
		// Find highest priority state (most recent in lifecycle)
		var keepState domain.TaskState
		maxPriority := -1
		for _, s := range states {
			if p := statePriority[s]; p > maxPriority {
				maxPriority = p
				keepState = s
			}
		}
		// Remove all other copies
		for _, s := range states {
			if s != keepState {
				path := taskPath(base, s, taskID)
				if err := os.Remove(path); err != nil {
					return fixed, fmt.Errorf("removing duplicate %s from %s: %w", taskID, s, err)
				}
				fixed++
			}
		}
	}
	return fixed, nil
}

// Ready returns pending tasks whose dependencies are all complete.
func Ready(base string) ([]Task, error) {
	pending, err := List(base, domain.StatePending)
	if err != nil {
		return nil, err
	}
	completed, err := List(base, domain.StateComplete)
	if err != nil {
		return nil, err
	}

	completeIDs := make(map[string]bool, len(completed))
	completeSummaries := make(map[string]bool, len(completed))
	for _, e := range completed {
		completeIDs[e.Task.ID] = true
		completeSummaries[e.Task.Summary] = true
	}

	var ready []Task
	for _, e := range pending {
		allDeps := true
		for _, dep := range e.Task.DependsOn {
			if !completeIDs[dep] && !completeSummaries[dep] {
				allDeps = false
				break
			}
		}
		if allDeps {
			ready = append(ready, e.Task)
		}
	}
	return ready, nil
}

// TaskSeed is the subset of fields used when seeding tasks from config.
type TaskSeed struct {
	Summary     string
	Description string
	Files       []string
	Acceptance  []string
	DependsOn   []string
}

// Seed creates tasks from config seeds, skipping duplicates by summary.
// Returns the number of tasks seeded.
func Seed(base string, seeds []TaskSeed) (int, error) {
	if err := Init(base); err != nil {
		return 0, err
	}

	existing, err := ListAll(base)
	if err != nil {
		return 0, err
	}

	existingSummaries := make(map[string]bool, len(existing))
	summaryToID := make(map[string]string, len(existing))
	for _, e := range existing {
		existingSummaries[e.Task.Summary] = true
		summaryToID[e.Task.Summary] = e.Task.ID
	}

	seeded := 0
	for _, s := range seeds {
		if existingSummaries[s.Summary] {
			continue
		}

		id := GenerateID()
		desc := s.Description
		if desc == "" {
			desc = s.Summary
		}

		// Resolve depends_on summary strings to task IDs
		var resolvedDeps []string
		for _, dep := range s.DependsOn {
			if resolved, ok := summaryToID[dep]; ok {
				resolvedDeps = append(resolvedDeps, resolved)
			} else {
				resolvedDeps = append(resolvedDeps, dep)
			}
		}

		task := Task{
			ID:          id,
			Summary:     s.Summary,
			Description: desc,
			Files:       s.Files,
			Acceptance:  s.Acceptance,
			DependsOn:   resolvedDeps,
		}
		if err := writeTask(base, domain.StatePending, &task); err != nil {
			return seeded, err
		}
		summaryToID[s.Summary] = id
		existingSummaries[s.Summary] = true
		seeded++
	}
	return seeded, nil
}

func readTask(path string) (*Task, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var t Task
	if err := json.Unmarshal(data, &t); err != nil {
		return nil, fmt.Errorf("parsing task %s: %w", path, err)
	}
	return &t, nil
}

func writeTask(base string, state domain.TaskState, task *Task) error {
	data, err := json.MarshalIndent(task, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(taskPath(base, state, task.ID), data, 0o644)
}
