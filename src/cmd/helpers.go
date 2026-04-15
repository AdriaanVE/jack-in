package cmd

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/AdriaanVE/jack-in/src/agent"
	"github.com/AdriaanVE/jack-in/src/config"
	"github.com/AdriaanVE/jack-in/src/daemon"
	"github.com/AdriaanVE/jack-in/src/domain"
	"github.com/AdriaanVE/jack-in/src/tmux"
	"github.com/AdriaanVE/jack-in/src/worktree"
)

func cwd() string {
	dir, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	// Auto-detect project root if we're inside a worktree (.w-<project>-<worker>)
	return projectRoot(dir)
}

// isGitRepo checks if the given path is inside a git repository.
func isGitRepo(path string) bool {
	cmd := exec.Command("git", "rev-parse", "--git-dir")
	cmd.Dir = path
	return cmd.Run() == nil
}

// requireGitRepo returns an error if path is not inside a git repository.
func requireGitRepo(path string) error {
	if !isGitRepo(path) {
		return fmt.Errorf("not a git repository\njackin requires git for worktree management. Run 'git init' first.")
	}
	return nil
}

// requireCleanGitState returns an error if git has conflicts or is mid-merge/rebase.
func requireCleanGitState(path string) error {
	// Check for merge in progress
	cmd := exec.Command("git", "rev-parse", "--git-path", "MERGE_HEAD")
	cmd.Dir = path
	if out, err := cmd.Output(); err == nil {
		mergePath := strings.TrimSpace(string(out))
		if _, err := os.Stat(mergePath); err == nil {
			return fmt.Errorf("merge in progress\nResolve the merge before running jackin init.")
		}
	}

	// Check for rebase in progress
	for _, rebaseDir := range []string{"rebase-merge", "rebase-apply"} {
		cmd := exec.Command("git", "rev-parse", "--git-path", rebaseDir)
		cmd.Dir = path
		if out, err := cmd.Output(); err == nil {
			rebasePath := strings.TrimSpace(string(out))
			if _, err := os.Stat(rebasePath); err == nil {
				return fmt.Errorf("rebase in progress\nComplete or abort the rebase before running jackin init.")
			}
		}
	}

	// Check for unmerged files (conflicts)
	cmd = exec.Command("git", "diff", "--name-only", "--diff-filter=U")
	cmd.Dir = path
	if out, err := cmd.Output(); err == nil && len(strings.TrimSpace(string(out))) > 0 {
		return fmt.Errorf("unresolved conflicts detected\nResolve all conflicts before running jackin init.")
	}

	return nil
}

// projectRoot returns the project root, detecting if we're inside a worktree.
// Worktrees are named .w-<project>-<worker> and live inside the project root.
func projectRoot(dir string) string {
	// Check if any path component looks like a worktree directory
	parts := strings.Split(dir, string(filepath.Separator))
	for i, part := range parts {
		if strings.HasPrefix(part, worktree.Prefix) && strings.Count(part, "-") >= 2 {
			// Found a worktree dir - project root is the parent
			root := string(filepath.Separator) + filepath.Join(parts[:i]...)
			// Verify it has jack-in.yaml
			if findConfigPath(root) != "" {
				return root
			}
		}
	}
	return dir
}

func promptUser(message string) string {
	fmt.Print(message)
	scanner := bufio.NewScanner(os.Stdin)
	if scanner.Scan() {
		return strings.TrimSpace(strings.ToLower(scanner.Text()))
	}
	return ""
}

// promptUserWithTimeout shows a countdown and returns defaultVal if no input within seconds.
func promptUserWithTimeout(message, defaultVal string, seconds int) string {
	inputCh := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(os.Stdin)
		if scanner.Scan() {
			inputCh <- strings.TrimSpace(strings.ToLower(scanner.Text()))
		} else {
			inputCh <- ""
		}
	}()

	for i := seconds; i > 0; i-- {
		fmt.Printf("\r%s (defaulting to '%s' in %ds) ", message, defaultVal, i)
		select {
		case input := <-inputCh:
			fmt.Println()
			return input
		case <-time.After(1 * time.Second):
		}
	}

	fmt.Printf("\r%s defaulting to '%s'.%s\n", message, defaultVal, strings.Repeat(" ", 10))
	return defaultVal
}

// attachSession attaches to a tmux session (or session:window target).
// Inside tmux it switches/selects; outside it spawns tmux attach.
func attachSession(target string) error {
	if os.Getenv("TMUX") != "" {
		if session, window, ok := strings.Cut(target, ":"); ok {
			return tmux.SelectWindow(session, window)
		}
		return tmux.SwitchClient(target)
	}
	cmd := exec.Command("tmux", "attach", "-t", target)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// resolveApproval returns the effective approval mode and its source.
func resolveApproval(base string, cfg *config.Config) (domain.ApprovalMode, string) {
	mode := daemon.ReadApprovalMode(base)
	if mode != "" {
		return mode, "runtime"
	}
	return cfg.Orchestrator.Approval, "config"
}

// findConfigPath returns the path to jack-in.yaml/yml, or empty string if not found.
func findConfigPath(base string) string {
	for _, name := range []string{"jack-in.yaml", "jack-in.yml"} {
		path := filepath.Join(base, name)
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return ""
}

func loadConfig(base string) (*config.Config, error) {
	path := findConfigPath(base)
	if path == "" {
		return nil, fmt.Errorf("no jack-in.yaml or jack-in.yml found in %s\nRun `jackin init` to get started.", base)
	}
	return config.Load(path)
}

func filterJackInWorktrees(entries []worktree.Info, project string) []worktree.Info {
	var filtered []worktree.Info
	for _, e := range entries {
		if worktree.IsJackIn(e, project) {
			filtered = append(filtered, e)
		}
	}
	return filtered
}

// workerShellCmd builds the full shell command to spawn a worker agent in its worktree.
func workerShellCmd(wCfg *config.WorkerConfig, wtPath string, startup *config.StartupInstructions) string {
	prompt := buildWorkerPrompt(wCfg.Prompt, startup)
	spawnCmd := agent.SpawnCommand(wCfg.Agent, prompt, "", "")
	return fmt.Sprintf("cd %s && %s", agent.ShellEscape(wtPath), spawnCmd)
}

// pruneOrphans prints orphaned worktrees, prompts the user, and removes them.
// When autoRemoveClean is true, clean orphans are removed without prompting.
func pruneOrphans(base string, orphans []worktree.Info, autoRemoveClean bool) {
	fmt.Printf("Orphaned worktrees (no matching worker in config):\n")
	anyDirty := printWorktreeStatuses(orphans)

	remove := autoRemoveClean && !anyDirty
	if !remove {
		if anyDirty {
			remove = promptUser("Remove orphaned worktrees with uncommitted changes? [y/N] ") == "y"
		} else {
			remove = promptUser(fmt.Sprintf("Remove %d orphaned worktree%s? [Y/n] ", len(orphans), plural(len(orphans)))) != "n"
		}
	}

	if !remove {
		fmt.Println("Kept.")
		return
	}

	removed, errs := worktree.RemoveAll(base, orphans)
	for _, e := range errs {
		fmt.Fprintf(os.Stderr, "  %v\n", e)
	}
	if len(removed) > 0 {
		fmt.Printf("Pruned %d orphaned worktree%s.\n", len(removed), plural(len(removed)))
	}
}

func printWorktreeStatuses(entries []worktree.Info) bool {
	anyDirty := false
	for _, e := range entries {
		n := worktree.DirtyCount(e.Path)
		label := "clean"
		if n > 0 {
			label = fmt.Sprintf("dirty - %d uncommitted change(s)", n)
			anyDirty = true
		}
		fmt.Printf("  %s (%s)\n", e.Path, label)
	}
	return anyDirty
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
