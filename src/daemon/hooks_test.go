package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/AdriaanVE/jack-in/src/domain"
)

func TestBuildClaudeSettings_Manual(t *testing.T) {
	base := "/tmp/project"
	cs := BuildClaudeSettings(base, "w1", domain.ApprovalManual, nil)

	// Should have Stop, Notification, PreToolUse, PostToolUse, UserPromptSubmit, SessionEnd
	expectedHooks := []string{"Stop", "Notification", "PreToolUse", "PostToolUse", "UserPromptSubmit", "SessionEnd"}
	for _, h := range expectedHooks {
		if _, ok := cs.Hooks[h]; !ok {
			t.Errorf("missing hook %s", h)
		}
	}

	// Manual mode should NOT have PermissionRequest
	if _, ok := cs.Hooks["PermissionRequest"]; ok {
		t.Error("manual mode should not have PermissionRequest hook")
	}

	if len(cs.Permissions.Allow) != 1 || cs.Permissions.Allow[0] != "Bash(jackin *)" {
		t.Errorf("unexpected permissions: %v", cs.Permissions.Allow)
	}
}

func TestBuildClaudeSettings_Yolo(t *testing.T) {
	cs := BuildClaudeSettings("/tmp/p", "w1", domain.ApprovalYolo, nil)
	pr, ok := cs.Hooks["PermissionRequest"]
	if !ok {
		t.Fatal("yolo mode should have PermissionRequest hook")
	}
	if len(pr) != 1 {
		t.Fatalf("expected 1 PermissionRequest entry, got %d", len(pr))
	}
}

func TestBuildClaudeSettings_Auto(t *testing.T) {
	cs := BuildClaudeSettings("/tmp/p", "w1", domain.ApprovalAuto, nil)
	pr, ok := cs.Hooks["PermissionRequest"]
	if !ok {
		t.Fatal("auto mode should have PermissionRequest hook")
	}
	if pr[0].Hooks[0].Timeout != 20 {
		t.Errorf("auto mode PermissionRequest should have timeout 20, got %d", pr[0].Hooks[0].Timeout)
	}
}

func TestBuildClaudeSettings_ExtraPermissions(t *testing.T) {
	cs := BuildClaudeSettings("/tmp/p", "w1", domain.ApprovalManual, []string{"Bash(tmux *)"})
	if len(cs.Permissions.Allow) != 2 {
		t.Errorf("expected 2 permissions, got %d", len(cs.Permissions.Allow))
	}
}

func TestBuildClaudeSettings_JSONRoundtrip(t *testing.T) {
	cs := BuildClaudeSettings("/tmp/p", "w1", domain.ApprovalManual, nil)
	data, err := json.MarshalIndent(cs, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if parsed["permissions"] == nil {
		t.Error("missing permissions in JSON")
	}
	if parsed["hooks"] == nil {
		t.Error("missing hooks in JSON")
	}
}

func TestWriteClaudeSettings(t *testing.T) {
	base := t.TempDir()
	wt := filepath.Join(base, "worktree")
	if err := os.MkdirAll(wt, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := WriteClaudeSettings(wt, base, "w1", domain.ApprovalManual, nil); err != nil {
		t.Fatalf("WriteClaudeSettings: %v", err)
	}
	path := filepath.Join(wt, ".claude", "settings.local.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading settings: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("parsing settings: %v", err)
	}
}

func TestMergeClaudeSettings(t *testing.T) {
	base := t.TempDir()

	// Write initial settings with a custom permission
	settingsDir := filepath.Join(base, ".claude")
	if err := os.MkdirAll(settingsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	initial := map[string]any{
		"permissions": map[string]any{
			"allow": []any{"Bash(custom *)"},
		},
	}
	data, _ := json.Marshal(initial)
	if err := os.WriteFile(filepath.Join(settingsDir, "settings.local.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	// Merge jackin settings
	if err := MergeClaudeSettings(base, base, "w1", domain.ApprovalManual, nil); err != nil {
		t.Fatalf("MergeClaudeSettings: %v", err)
	}

	// Read and verify
	merged, err := os.ReadFile(filepath.Join(settingsDir, "settings.local.json"))
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err := json.Unmarshal(merged, &result); err != nil {
		t.Fatal(err)
	}

	perms := extractStringSlice(result, "permissions", "allow")
	hasCustom := false
	hasJackIn := false
	for _, p := range perms {
		if p == "Bash(custom *)" {
			hasCustom = true
		}
		if p == "Bash(jackin *)" {
			hasJackIn = true
		}
	}
	if !hasCustom {
		t.Error("merge should preserve existing permissions")
	}
	if !hasJackIn {
		t.Error("merge should add jackin permissions")
	}
}

func TestDedupStrings(t *testing.T) {
	tests := []struct {
		input []string
		want  []string
	}{
		{[]string{"a", "b", "c"}, []string{"a", "b", "c"}},
		{[]string{"a", "b", "a", "c", "b"}, []string{"a", "b", "c"}},
		{[]string{}, nil},
		{[]string{"x"}, []string{"x"}},
	}
	for _, tt := range tests {
		got := dedupStrings(tt.input)
		if len(got) != len(tt.want) {
			t.Errorf("dedupStrings(%v) = %v, want %v", tt.input, got, tt.want)
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("dedupStrings(%v)[%d] = %q, want %q", tt.input, i, got[i], tt.want[i])
			}
		}
	}
}

func TestIsJackInHook(t *testing.T) {
	jackinHook := map[string]any{
		"matcher": "*",
		"hooks": []any{
			map[string]any{"type": "command", "command": "/project/.jack-in/stop-hook.sh"},
		},
	}
	if !isJackInHook(jackinHook) {
		t.Error("expected jackin hook to be detected")
	}

	userHook := map[string]any{
		"matcher": "*",
		"hooks": []any{
			map[string]any{"type": "command", "command": "/usr/local/bin/my-hook"},
		},
	}
	if isJackInHook(userHook) {
		t.Error("expected user hook to NOT be detected as jackin")
	}

	if isJackInHook("not a map") {
		t.Error("expected non-map to return false")
	}

	if isJackInHook(map[string]any{"hooks": "not an array"}) {
		t.Error("expected bad hooks field to return false")
	}
}

func TestExtractStringSlice(t *testing.T) {
	m := map[string]any{
		"permissions": map[string]any{
			"allow": []any{"Bash(jackin *)", "Bash(tmux *)"},
		},
	}
	got := extractStringSlice(m, "permissions", "allow")
	if len(got) != 2 {
		t.Fatalf("expected 2 strings, got %d", len(got))
	}

	// Missing key
	got = extractStringSlice(m, "permissions", "deny")
	if got != nil {
		t.Errorf("expected nil for missing key, got %v", got)
	}

	// Wrong type
	got = extractStringSlice(map[string]any{"x": "not a map"}, "x", "y")
	if got != nil {
		t.Errorf("expected nil for wrong type, got %v", got)
	}
}

func TestInstallHooks(t *testing.T) {
	base := t.TempDir()
	if err := InstallHooks(base); err != nil {
		t.Fatal(err)
	}
	// Verify all scripts were written
	for _, name := range hookScripts {
		path := filepath.Join(base, ".jack-in", name)
		info, err := os.Stat(path)
		if err != nil {
			t.Errorf("expected hook %s to exist: %v", name, err)
			continue
		}
		if info.Size() == 0 {
			t.Errorf("hook %s is empty", name)
		}
	}
}

func TestApprovalModeReadWrite(t *testing.T) {
	base := t.TempDir()

	// No file yet
	if mode := ReadApprovalMode(base); mode != "" {
		t.Errorf("expected empty, got %q", mode)
	}

	// Create .jack-in dir (normally done by InitSignalDirs at daemon startup)
	if err := os.MkdirAll(filepath.Join(base, ".jack-in"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := WriteApprovalMode(base, domain.ApprovalYolo); err != nil {
		t.Fatal(err)
	}
	if mode := ReadApprovalMode(base); mode != domain.ApprovalYolo {
		t.Errorf("expected yolo, got %q", mode)
	}
}
