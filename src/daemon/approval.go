package daemon

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/AdriaanVE/jack-in/src/domain"
)

const approvalModeFile = ".jack-in/approval-mode"

// ReadApprovalMode reads the runtime approval mode from the signal file.
// Returns empty string if the file doesn't exist or contains an invalid mode.
func ReadApprovalMode(base string) domain.ApprovalMode {
	data, err := os.ReadFile(filepath.Join(base, approvalModeFile))
	if err != nil {
		return ""
	}
	mode := strings.TrimSpace(string(data))
	if domain.IsApprovalMode(mode) {
		return domain.ApprovalMode(mode)
	}
	return ""
}

// WriteApprovalMode writes the runtime approval mode signal file.
func WriteApprovalMode(base string, mode domain.ApprovalMode) error {
	return os.WriteFile(filepath.Join(base, approvalModeFile), []byte(string(mode)+"\n"), 0o644)
}
