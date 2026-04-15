package daemon

import (
	"testing"

	"github.com/AdriaanVE/jack-in/src/domain"
	"github.com/AdriaanVE/jack-in/src/taskqueue"
)

func TestSendEvent_NilChannel(t *testing.T) {
	// Should not panic
	sendEvent(nil, "test")
}

func TestSendEvent_FullChannel(t *testing.T) {
	ch := make(chan any, 1)
	ch <- "first"
	// Channel is full; should not block
	sendEvent(ch, "second")
	got := <-ch
	if got != "first" {
		t.Errorf("expected first message, got %v", got)
	}
}

func TestSendEvent_Delivers(t *testing.T) {
	ch := make(chan any, 1)
	sendEvent(ch, "hello")
	got := <-ch
	if got != "hello" {
		t.Errorf("expected hello, got %v", got)
	}
}

func TestBuildStateMsg(t *testing.T) {
	base := t.TempDir()
	taskqueue.Init(base)

	workers := map[string]*domain.WorkerState{
		"bob": {
			Name:  "bob",
			Agent: domain.AgentClaude,
		},
		"alice": {
			Name:        "alice",
			Agent:       domain.AgentCodex,
			CurrentTask: "task-1",
		},
	}

	counts := domain.TaskCounts{Pending: 1, Current: 1}
	msg := buildStateMsg(base, "jackin-test", domain.ApprovalAuto, workers, nil, counts, nil)

	if msg.Session != "jackin-test" {
		t.Errorf("session = %q", msg.Session)
	}
	if msg.Approval != domain.ApprovalAuto {
		t.Errorf("approval = %q", msg.Approval)
	}
	if msg.Orchestrator != nil {
		t.Error("expected nil orchestrator")
	}
	// Workers should be sorted by name
	if len(msg.Workers) != 2 {
		t.Fatalf("expected 2 workers, got %d", len(msg.Workers))
	}
	if msg.Workers[0].Name != "alice" {
		t.Errorf("first worker = %q, want alice (sorted)", msg.Workers[0].Name)
	}
	if msg.Workers[1].Name != "bob" {
		t.Errorf("second worker = %q, want bob (sorted)", msg.Workers[1].Name)
	}
}

func TestBuildStateMsg_WithOrchestrator(t *testing.T) {
	base := t.TempDir()
	taskqueue.Init(base)

	workers := map[string]*domain.WorkerState{}
	orch := &OrchestratorState{Agent: domain.AgentClaude}
	counts := domain.TaskCounts{}
	msg := buildStateMsg(base, "s", domain.ApprovalManual, workers, orch, counts, nil)

	if msg.Orchestrator == nil {
		t.Fatal("expected orchestrator snapshot")
	}
	if msg.Orchestrator.Name != "orchestrator" {
		t.Errorf("orchestrator name = %q", msg.Orchestrator.Name)
	}
}
