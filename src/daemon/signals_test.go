package daemon

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestInitSignalDirs(t *testing.T) {
	base := t.TempDir()
	if err := InitSignalDirs(base); err != nil {
		t.Fatalf("InitSignalDirs: %v", err)
	}
	for _, dir := range []string{signalDir, currentTaskDir} {
		info, err := os.Stat(filepath.Join(base, dir))
		if err != nil {
			t.Errorf("directory %s not created: %v", dir, err)
		} else if !info.IsDir() {
			t.Errorf("%s is not a directory", dir)
		}
	}
}

func TestSignalCRUD(t *testing.T) {
	base := t.TempDir()
	if err := InitSignalDirs(base); err != nil {
		t.Fatal(err)
	}

	worker := "w1"

	// Initially no signal
	if HasSignal(base, worker) {
		t.Error("expected no signal initially")
	}

	// Write idle signal
	if err := WriteIdleSignal(base, worker); err != nil {
		t.Fatalf("WriteIdleSignal: %v", err)
	}
	if !HasSignal(base, worker) {
		t.Error("expected signal after write")
	}

	// Clear signal
	if err := ClearSignal(base, worker); err != nil {
		t.Fatalf("ClearSignal: %v", err)
	}
	if HasSignal(base, worker) {
		t.Error("expected no signal after clear")
	}

	// Clear non-existent signal is not an error
	if err := ClearSignal(base, worker); err != nil {
		t.Errorf("ClearSignal on missing file: %v", err)
	}
}

func TestNeedsInputSignal(t *testing.T) {
	base := t.TempDir()
	if err := InitSignalDirs(base); err != nil {
		t.Fatal(err)
	}

	worker := "w1"
	if HasNeedsInput(base, worker) {
		t.Error("expected no needs-input initially")
	}

	// Create the signal file manually
	if err := os.WriteFile(needsInputPath(base, worker), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if !HasNeedsInput(base, worker) {
		t.Error("expected needs-input after write")
	}
	if err := ClearNeedsInput(base, worker); err != nil {
		t.Fatal(err)
	}
	if HasNeedsInput(base, worker) {
		t.Error("expected no needs-input after clear")
	}
}

func TestExitedSignal(t *testing.T) {
	base := t.TempDir()
	if err := InitSignalDirs(base); err != nil {
		t.Fatal(err)
	}

	worker := "w1"
	if HasExited(base, worker) {
		t.Error("expected no exited initially")
	}
	if err := os.WriteFile(exitedPath(base, worker), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if !HasExited(base, worker) {
		t.Error("expected exited after write")
	}
	if err := ClearExited(base, worker); err != nil {
		t.Fatal(err)
	}
}

func TestHeartbeatAge(t *testing.T) {
	base := t.TempDir()
	if err := InitSignalDirs(base); err != nil {
		t.Fatal(err)
	}

	worker := "w1"

	// No heartbeat file
	if age := HeartbeatAge(base, worker); age != -1 {
		t.Errorf("expected -1 for missing heartbeat, got %v", age)
	}

	// Create heartbeat
	if err := os.WriteFile(heartbeatPath(base, worker), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	age := HeartbeatAge(base, worker)
	if age < 0 || age > 2*time.Second {
		t.Errorf("heartbeat age %v out of expected range", age)
	}
}

func TestClearAllWorkerSignals(t *testing.T) {
	base := t.TempDir()
	if err := InitSignalDirs(base); err != nil {
		t.Fatal(err)
	}

	worker := "w1"
	// Create all signal files
	for _, path := range []string{
		signalPath(base, worker),
		needsInputPath(base, worker),
		exitedPath(base, worker),
		heartbeatPath(base, worker),
	} {
		if err := os.WriteFile(path, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	ClearAllWorkerSignals(base, worker)

	if HasSignal(base, worker) || HasNeedsInput(base, worker) || HasExited(base, worker) {
		t.Error("expected all signals cleared")
	}
	if HeartbeatAge(base, worker) != -1 {
		t.Error("expected heartbeat cleared")
	}
}

func TestCurrentTask(t *testing.T) {
	base := t.TempDir()
	if err := InitSignalDirs(base); err != nil {
		t.Fatal(err)
	}

	worker := "w1"
	taskID := "task-123"

	if err := WriteCurrentTask(base, worker, taskID); err != nil {
		t.Fatalf("WriteCurrentTask: %v", err)
	}

	data, err := os.ReadFile(CurrentTaskFilePath(base, worker))
	if err != nil {
		t.Fatalf("reading current task: %v", err)
	}
	if string(data) != taskID {
		t.Errorf("current task = %q, want %q", data, taskID)
	}

	if err := ClearCurrentTask(base, worker); err != nil {
		t.Fatalf("ClearCurrentTask: %v", err)
	}
	if _, err := os.Stat(CurrentTaskFilePath(base, worker)); !os.IsNotExist(err) {
		t.Error("expected current task file removed")
	}

	// Clear non-existent is not an error
	if err := ClearCurrentTask(base, worker); err != nil {
		t.Errorf("ClearCurrentTask on missing: %v", err)
	}
}

func TestCancelSignal(t *testing.T) {
	base := t.TempDir()
	if err := InitSignalDirs(base); err != nil {
		t.Fatal(err)
	}

	worker := "w1"
	taskID := "task-456"

	// Initially no cancel signal
	if HasCancel(base, worker) {
		t.Error("expected no cancel signal initially")
	}
	if got := ReadCancelTaskID(base, worker); got != "" {
		t.Errorf("ReadCancelTaskID = %q, want empty", got)
	}

	// Write cancel signal with task ID
	if err := WriteCancelSignal(base, worker, taskID); err != nil {
		t.Fatalf("WriteCancelSignal: %v", err)
	}
	if !HasCancel(base, worker) {
		t.Error("expected cancel signal after write")
	}
	if got := ReadCancelTaskID(base, worker); got != taskID {
		t.Errorf("ReadCancelTaskID = %q, want %q", got, taskID)
	}

	// Clear cancel signal
	if err := ClearCancel(base, worker); err != nil {
		t.Fatalf("ClearCancel: %v", err)
	}
	if HasCancel(base, worker) {
		t.Error("expected no cancel signal after clear")
	}

	// ClearAllWorkerSignals should also clear cancel
	if err := WriteCancelSignal(base, worker, taskID); err != nil {
		t.Fatal(err)
	}
	ClearAllWorkerSignals(base, worker)
	if HasCancel(base, worker) {
		t.Error("expected cancel signal cleared by ClearAllWorkerSignals")
	}
}

func TestParseCancelPayload(t *testing.T) {
	tests := []struct {
		payload    string
		wantTaskID string
		wantAction string
	}{
		{"task-123:pending", "task-123", "pending"},
		{"task-123:drop", "task-123", "drop"},
		{"task-123", "task-123", "pending"},           // default action
		{"task-123:invalid", "task-123", "pending"},   // invalid action defaults
		{"task-123-456:drop", "task-123-456", "drop"}, // task ID with hyphens
		{"", "", "pending"},                           // empty payload
	}

	for _, tt := range tests {
		t.Run(tt.payload, func(t *testing.T) {
			taskID, action := parseCancelPayload(tt.payload)
			if taskID != tt.wantTaskID {
				t.Errorf("taskID = %q, want %q", taskID, tt.wantTaskID)
			}
			if action != tt.wantAction {
				t.Errorf("action = %q, want %q", action, tt.wantAction)
			}
		})
	}
}
