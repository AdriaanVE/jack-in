package daemon

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/AdriaanVE/jack-in/hooks"
	"github.com/AdriaanVE/jack-in/src/agent"
	"github.com/AdriaanVE/jack-in/src/domain"
)

// Hook scripts to install in .jack-in/.
var hookScripts = []string{
	"stop-hook.sh",
	"permission-eval.sh",
	"yolo-approve.sh",
	"notification-hook.sh",
	"heartbeat-hook.sh",
	"prompt-hook.sh",
	"session-end-hook.sh",
	"notify-hook.sh",
}

var notificationMatchers = []string{
	"idle_prompt",
	"permission_prompt",
	"elicitation_dialog",
}

var basePermissions = []string{"Bash(jackin *)"}

// OrchestratorPermissions are extra permissions for the orchestrator agent.
var OrchestratorPermissions = []string{
	"Bash(jackin *)",
	"Bash(tmux *)",
	"Bash(git diff *)",
	"Bash(git log *)",
}

// InstallHooks writes embedded hook scripts into .jack-in/.
func InstallHooks(base string) error {
	jackinDir := filepath.Join(base, ".jack-in")
	if err := os.MkdirAll(jackinDir, 0o755); err != nil {
		return fmt.Errorf("creating .jack-in dir: %w", err)
	}
	for _, name := range hookScripts {
		data, err := hooks.Scripts.ReadFile(name)
		if err != nil {
			return fmt.Errorf("reading embedded hook %s: %w", name, err)
		}
		dst := filepath.Join(jackinDir, name)
		if err := os.WriteFile(dst, data, 0o755); err != nil {
			return fmt.Errorf("writing hook %s: %w", name, err)
		}
	}
	return nil
}

// ClaudeSettings represents the settings.local.json structure for Claude Code.
type ClaudeSettings struct {
	Permissions struct {
		Allow []string `json:"allow"`
	} `json:"permissions"`
	Hooks map[string][]hookEntry `json:"hooks"`
}

type hookEntry struct {
	Matcher string     `json:"matcher"`
	Hooks   []hookSpec `json:"hooks"`
}

type hookSpec struct {
	Type    string `json:"type"`
	Command string `json:"command"`
	Timeout int    `json:"timeout,omitempty"`
}

// BuildClaudeSettings creates the jackin-specific Claude settings structure.
func BuildClaudeSettings(base, workerName string, approval domain.ApprovalMode, extraPermissions []string) *ClaudeSettings {
	jackinDir := filepath.Join(base, ".jack-in")
	stopHook := filepath.Join(jackinDir, "stop-hook.sh")
	sigDir := filepath.Join(base, signalDir)
	esc := agent.ShellEscape

	cs := &ClaudeSettings{}
	cs.Hooks = make(map[string][]hookEntry)

	// Stop hook
	cs.Hooks["Stop"] = []hookEntry{{
		Matcher: "*",
		Hooks: []hookSpec{{
			Type:    "command",
			Command: fmt.Sprintf("%s %s %s", esc(stopHook), esc(sigDir), esc(workerName)),
		}},
	}}

	hookCmd := func(script string) string {
		return fmt.Sprintf("%s %s %s", esc(filepath.Join(jackinDir, script)), esc(sigDir), esc(workerName))
	}
	wildcardHook := func(script string) []hookEntry {
		return []hookEntry{{
			Matcher: "*",
			Hooks:   []hookSpec{{Type: "command", Command: hookCmd(script)}},
		}}
	}

	// Notification hooks
	var notifEntries []hookEntry
	for _, matcher := range notificationMatchers {
		notifEntries = append(notifEntries, hookEntry{
			Matcher: matcher,
			Hooks:   []hookSpec{{Type: "command", Command: hookCmd("notification-hook.sh")}},
		})
	}
	cs.Hooks["Notification"] = notifEntries

	cs.Hooks["PreToolUse"] = wildcardHook("heartbeat-hook.sh")
	cs.Hooks["PostToolUse"] = wildcardHook("heartbeat-hook.sh")
	cs.Hooks["UserPromptSubmit"] = wildcardHook("prompt-hook.sh")
	cs.Hooks["SessionEnd"] = wildcardHook("session-end-hook.sh")

	// Approval-mode-specific hooks
	switch approval {
	case domain.ApprovalAuto:
		permEval := filepath.Join(jackinDir, "permission-eval.sh")
		cs.Hooks["PermissionRequest"] = []hookEntry{{
			Matcher: "*",
			Hooks:   []hookSpec{{Type: "command", Command: esc(permEval), Timeout: 20}},
		}}
	case domain.ApprovalYolo:
		yoloHook := filepath.Join(jackinDir, "yolo-approve.sh")
		cs.Hooks["PermissionRequest"] = []hookEntry{{
			Matcher: "*",
			Hooks:   []hookSpec{{Type: "command", Command: esc(yoloHook)}},
		}}
	}
	// manual: no PermissionRequest hook

	// Permissions
	allow := make([]string, len(basePermissions))
	copy(allow, basePermissions)
	allow = append(allow, extraPermissions...)
	cs.Permissions.Allow = allow

	return cs
}

// WriteClaudeSettings writes settings.local.json for a worktree (overwrites).
func WriteClaudeSettings(worktreePath, base, workerName string, approval domain.ApprovalMode, extraPermissions []string) error {
	settingsDir := filepath.Join(worktreePath, ".claude")
	if err := os.MkdirAll(settingsDir, 0o755); err != nil {
		return err
	}
	cs := BuildClaudeSettings(base, workerName, approval, extraPermissions)
	return atomicWriteJSON(filepath.Join(settingsDir, "settings.local.json"), cs)
}

// MergeClaudeSettings merges jackin settings into an existing settings.local.json.
func MergeClaudeSettings(projectRoot, base, workerName string, approval domain.ApprovalMode, extraPermissions []string) error {
	settingsDir := filepath.Join(projectRoot, ".claude")
	if err := os.MkdirAll(settingsDir, 0o755); err != nil {
		return err
	}
	settingsPath := filepath.Join(settingsDir, "settings.local.json")

	// Read existing
	var existing map[string]any
	data, err := os.ReadFile(settingsPath)
	if err == nil {
		_ = json.Unmarshal(data, &existing)
	}
	if existing == nil {
		existing = make(map[string]any)
	}

	jackin := BuildClaudeSettings(base, workerName, approval, extraPermissions)

	// Merge permissions.allow (deduplicate)
	existingAllow := extractStringSlice(existing, "permissions", "allow")
	merged := dedupStrings(append(existingAllow, jackin.Permissions.Allow...))
	if existing["permissions"] == nil {
		existing["permissions"] = map[string]any{}
	}
	existing["permissions"].(map[string]any)["allow"] = merged

	// Merge hooks
	if existing["hooks"] == nil {
		existing["hooks"] = map[string]any{}
	}
	existingHooks := existing["hooks"].(map[string]any)

	// Convert typed hooks to generic form for merging with untyped JSON
	jackinHooks := hooksToGeneric(jackin.Hooks)

	for event, entries := range jackinHooks {
		if _, ok := existingHooks[event]; !ok {
			existingHooks[event] = entries
		} else {
			// Filter out existing jackin hooks, then append new ones
			existingEntries, ok := existingHooks[event].([]any)
			if !ok {
				existingHooks[event] = entries
				continue
			}
			var kept []any
			for _, e := range existingEntries {
				if !isJackInHook(e) {
					kept = append(kept, e)
				}
			}
			existingHooks[event] = append(kept, entries...)
		}
	}

	// Remove stale jackin hooks from events not in new settings
	for event := range existingHooks {
		if _, ok := jackinHooks[event]; ok {
			continue
		}
		entries, ok := existingHooks[event].([]any)
		if !ok {
			continue
		}
		var kept []any
		for _, e := range entries {
			if !isJackInHook(e) {
				kept = append(kept, e)
			}
		}
		if len(kept) == 0 {
			delete(existingHooks, event)
		} else {
			existingHooks[event] = kept
		}
	}

	return atomicWriteJSON(settingsPath, existing)
}

func atomicWriteJSON(path string, data any) error {
	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func extractStringSlice(m map[string]any, keys ...string) []string {
	var current any = m
	for _, k := range keys {
		obj, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current = obj[k]
	}
	arr, ok := current.([]any)
	if !ok {
		return nil
	}
	var result []string
	for _, v := range arr {
		if s, ok := v.(string); ok {
			result = append(result, s)
		}
	}
	return result
}

func dedupStrings(ss []string) []string {
	seen := make(map[string]bool, len(ss))
	var result []string
	for _, s := range ss {
		if !seen[s] {
			seen[s] = true
			result = append(result, s)
		}
	}
	return result
}

func isJackInHook(entry any) bool {
	m, ok := entry.(map[string]any)
	if !ok {
		return false
	}
	hooks, ok := m["hooks"].([]any)
	if !ok {
		return false
	}
	for _, h := range hooks {
		hm, ok := h.(map[string]any)
		if !ok {
			continue
		}
		if cmd, ok := hm["command"].(string); ok {
			if strings.Contains(cmd, ".jack-in/") {
				return true
			}
		}
	}
	return false
}

// hooksToGeneric converts typed hookEntry maps to map[string][]any without JSON round-trip.
func hooksToGeneric(typed map[string][]hookEntry) map[string][]any {
	result := make(map[string][]any, len(typed))
	for event, entries := range typed {
		generic := make([]any, len(entries))
		for i, e := range entries {
			h := make([]any, len(e.Hooks))
			for j, spec := range e.Hooks {
				hm := map[string]any{"type": spec.Type, "command": spec.Command}
				if spec.Timeout > 0 {
					hm["timeout"] = spec.Timeout
				}
				h[j] = hm
			}
			generic[i] = map[string]any{"matcher": e.Matcher, "hooks": h}
		}
		result[event] = generic
	}
	return result
}
