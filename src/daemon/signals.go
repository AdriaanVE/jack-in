package daemon

import (
	"errors"
	"os"
	"path/filepath"
	"time"
)

const (
	signalDir      = ".jack-in/signals"
	currentTaskDir = ".jack-in/current-task"
)

// Signal file path helpers.

func signalPath(base, workerName string) string {
	return filepath.Join(base, signalDir, workerName+".done")
}

func heartbeatPath(base, workerName string) string {
	return filepath.Join(base, signalDir, workerName+".heartbeat")
}

func needsInputPath(base, workerName string) string {
	return filepath.Join(base, signalDir, workerName+".needs-input")
}

func exitedPath(base, workerName string) string {
	return filepath.Join(base, signalDir, workerName+".exited")
}

func cancelPath(base, workerName string) string {
	return filepath.Join(base, signalDir, workerName+".cancel")
}

// CurrentTaskFilePath returns the path to a worker's current-task file.
func CurrentTaskFilePath(base, workerName string) string {
	return filepath.Join(base, currentTaskDir, workerName)
}

// Generic signal file operations.

func hasSignalFile(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func clearSignalFile(path string) error {
	err := os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func writeSignalFile(path string) error {
	return os.WriteFile(path, nil, 0o644)
}

// Typed signal accessors.

func HasSignal(base, worker string) bool        { return hasSignalFile(signalPath(base, worker)) }
func ClearSignal(base, worker string) error     { return clearSignalFile(signalPath(base, worker)) }
func HasNeedsInput(base, worker string) bool    { return hasSignalFile(needsInputPath(base, worker)) }
func ClearNeedsInput(base, worker string) error { return clearSignalFile(needsInputPath(base, worker)) }
func HasExited(base, worker string) bool        { return hasSignalFile(exitedPath(base, worker)) }
func ClearExited(base, worker string) error     { return clearSignalFile(exitedPath(base, worker)) }
func ClearHeartbeat(base, worker string) error  { return clearSignalFile(heartbeatPath(base, worker)) }
func HasCancel(base, worker string) bool        { return hasSignalFile(cancelPath(base, worker)) }
func ClearCancel(base, worker string) error     { return clearSignalFile(cancelPath(base, worker)) }

// ReadCancelTaskID reads the task ID from a cancel signal file.
// Returns empty string if file doesn't exist or can't be read.
func ReadCancelTaskID(base, worker string) string {
	data, err := os.ReadFile(cancelPath(base, worker))
	if err != nil {
		return ""
	}
	return string(data)
}

// WriteCancelSignal writes a cancel signal with the task ID.
func WriteCancelSignal(base, worker, taskID string) error {
	return os.WriteFile(cancelPath(base, worker), []byte(taskID), 0o644)
}

// HeartbeatAge returns the time since the heartbeat file was modified,
// or -1 if the file doesn't exist or can't be stat'd.
func HeartbeatAge(base, worker string) time.Duration {
	info, err := os.Stat(heartbeatPath(base, worker))
	if err != nil {
		return -1
	}
	return time.Since(info.ModTime())
}

// ClearAllWorkerSignals removes all signal files for a worker.
func ClearAllWorkerSignals(base, worker string) {
	_ = ClearSignal(base, worker)
	_ = ClearNeedsInput(base, worker)
	_ = ClearExited(base, worker)
	_ = ClearHeartbeat(base, worker)
	_ = ClearCancel(base, worker)
}

// WriteIdleSignal creates a .done signal to mark a worker as idle.
func WriteIdleSignal(base, worker string) error {
	return writeSignalFile(signalPath(base, worker))
}

// InitSignalDirs creates the signal and current-task directories.
func InitSignalDirs(base string) error {
	if err := os.MkdirAll(filepath.Join(base, signalDir), 0o755); err != nil {
		return err
	}
	return os.MkdirAll(filepath.Join(base, currentTaskDir), 0o755)
}

// Current-task file helpers.

func WriteCurrentTask(base, worker, taskID string) error {
	return os.WriteFile(CurrentTaskFilePath(base, worker), []byte(taskID), 0o644)
}

func ClearCurrentTask(base, worker string) error {
	err := os.Remove(CurrentTaskFilePath(base, worker))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
