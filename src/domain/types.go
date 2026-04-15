package domain

// WorkerRole defines what a worker does in the swarm.
type WorkerRole string

const (
	RoleExecutor WorkerRole = "executor"
	RoleReviewer WorkerRole = "reviewer"
	RolePlanner  WorkerRole = "planner"
)

// AgentType identifies a supported CLI agent.
type AgentType string

const (
	AgentClaude   AgentType = "claude"
	AgentCodex    AgentType = "codex"
	AgentOpencode AgentType = "opencode"
	AgentGemini   AgentType = "gemini"
)

var AgentNames = []AgentType{AgentClaude, AgentCodex, AgentOpencode, AgentGemini}

func IsAgentType(s string) bool {
	for _, a := range AgentNames {
		if string(a) == s {
			return true
		}
	}
	return false
}

// ApprovalMode controls how permission prompts are handled.
type ApprovalMode string

const (
	ApprovalManual ApprovalMode = "manual"
	ApprovalAuto   ApprovalMode = "auto"
	ApprovalYolo   ApprovalMode = "yolo"
)

func IsApprovalMode(s string) bool {
	return s == "manual" || s == "auto" || s == "yolo"
}

func IsWorkerRole(s string) bool {
	return s == string(RoleExecutor) || s == string(RoleReviewer) || s == string(RolePlanner)
}

// TaskState represents where a task is in the queue lifecycle.
type TaskState string

const (
	StatePending  TaskState = "pending"
	StateCurrent  TaskState = "current"
	StateReview   TaskState = "review"
	StateComplete TaskState = "complete"
	StateRejected TaskState = "rejected"
)

var TaskStates = []TaskState{StatePending, StateCurrent, StateReview, StateComplete, StateRejected}

// TaskCounts holds the number of tasks in each state.
type TaskCounts struct {
	Pending  int
	Current  int
	Review   int
	Complete int
	Rejected int
}

// WorkerState is the runtime state of a single worker as tracked by the daemon.
type WorkerState struct {
	Name             string
	Agent            AgentType
	CurrentTask      string
	AssignedAt       int64  // unix ms, 0 = idle
	ReviewTaskID     string // task ID blocking this worker while in review
	LastPaneSnapshot string
	LastSnapshotAt   int64
	LLMEvalCount     int
	EscalatedToUser  bool
}

func (w *WorkerState) IsIdle() bool {
	return w.CurrentTask == ""
}

// State label constants returned by StateLabel().
const (
	LabelIdle       = "idle"
	LabelWorking    = "working"
	LabelBlocked    = "blocked"
	LabelStuck      = "stuck"
	LabelRestarting = "restarting"
)

func (w *WorkerState) StateLabel() string {
	if w.EscalatedToUser {
		return LabelStuck
	}
	if w.CurrentTask != "" {
		return LabelWorking
	}
	if w.ReviewTaskID != "" {
		return LabelBlocked
	}
	return LabelIdle
}

// WorkerSnapshot is a point-in-time view of a single worker,
// used for event passing between daemon and UI.
type WorkerSnapshot struct {
	Name               string
	Agent              AgentType
	State              string // LabelIdle, LabelWorking, LabelStuck
	CurrentTask        string
	CurrentTaskSummary string
	Tokens             TokenUsage // session token usage (Claude only)
}

// StateEvent is a full snapshot of daemon state, sent to the TUI after each tick.
type StateEvent struct {
	Workers      []WorkerSnapshot
	Orchestrator *WorkerSnapshot // nil if no orchestrator
	Tasks        TaskCounts
	Tokens       TokenUsage // cumulative LLM API usage
	Approval     ApprovalMode
	Session      string
}

// LogEvent carries a single formatted log line for the TUI.
type LogEvent struct {
	Line string
}

// TokenUsage tracks LLM API token consumption.
type TokenUsage struct {
	InputTokens  int64   `json:"input_tokens"`
	OutputTokens int64   `json:"output_tokens"`
	CostUSD      float64 `json:"cost_usd"` // estimated cost based on API pricing
}
