package worktree

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const Prefix = ".w-"

func DirName(project, name string) string {
	return Prefix + project + "-" + name
}

func Path(base, project, name string) string {
	return filepath.Join(base, DirName(project, name))
}

// BranchName returns the git branch name for a worker worktree.
func BranchName(project, name string) string {
	return "jackin/" + project + "/" + name
}

func git(args []string, cwd string) (string, error) {
	cmd := exec.Command("git", args...)
	if cwd != "" {
		cmd.Dir = cwd
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(string(out)))
	}
	return strings.TrimRight(string(out), "\n"), nil
}

// Create adds a new git worktree for a worker. Returns the worktree path.
func Create(base, project, name string) (string, error) {
	wt := Path(base, project, name)
	branch := BranchName(project, name)

	// Remove stale directory if it exists but isn't tracked as a worktree
	if info, err := os.Stat(wt); err == nil && info.IsDir() {
		// Check if git knows about this worktree
		entries, _ := List(base)
		tracked := false
		for _, e := range entries {
			if e.Path == wt {
				tracked = true
				break
			}
		}
		if !tracked {
			if err := os.RemoveAll(wt); err != nil {
				return "", fmt.Errorf("failed to remove stale directory %s: %w", wt, err)
			}
		}
	}

	_, err := git([]string{"worktree", "add", wt, "-b", branch}, base)
	if err != nil {
		// Branch already exists from a previous run - try without -b
		if strings.Contains(err.Error(), "already exists") {
			_, err = git([]string{"worktree", "add", wt, branch}, base)
		}
		if err != nil {
			return "", err
		}
	}
	return wt, nil
}

// Remove force-removes a worktree by path.
func Remove(path, base string) error {
	_, err := git([]string{"worktree", "remove", path, "--force"}, base)
	return err
}

// Prune removes stale worktree references (where the directory no longer exists).
func Prune(base string) error {
	_, err := git([]string{"worktree", "prune"}, base)
	return err
}

type Info struct {
	Path     string
	Branch   string
	Bare     bool
	Prunable bool // true if the worktree directory is missing/broken
}

// List returns all git worktrees.
func List(base string) ([]Info, error) {
	out, err := git([]string{"worktree", "list", "--porcelain"}, base)
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}

	var entries []Info
	var current Info
	hasPath := false

	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, "worktree "):
			current.Path = strings.TrimPrefix(line, "worktree ")
			hasPath = true
		case strings.HasPrefix(line, "branch "):
			current.Branch = strings.TrimPrefix(line, "branch ")
		case line == "bare":
			current.Bare = true
		case line == "prunable":
			current.Prunable = true
		case line == "":
			if hasPath {
				entries = append(entries, current)
			}
			current = Info{}
			hasPath = false
		}
	}
	if hasPath {
		entries = append(entries, current)
	}
	return entries, nil
}

// Refresh resets a worktree to track the given branch. It recreates the worker
// branch from the local target branch (typically main after orchestrator merge).
// Cleans up uncommitted changes and untracked files.
func Refresh(path, project, worker, branch string) error {
	wb := BranchName(project, worker)
	// Reset worker branch to the local target branch (not origin/ - the merge is local)
	if _, err := git([]string{"checkout", "-B", wb, branch}, path); err != nil {
		return fmt.Errorf("recreate branch %s from %s: %w", wb, branch, err)
	}
	// Discard any uncommitted changes
	if _, err := git([]string{"reset", "--hard", branch}, path); err != nil {
		return fmt.Errorf("reset --hard %s: %w", branch, err)
	}
	// Remove untracked files and directories
	if _, err := git([]string{"clean", "-fd"}, path); err != nil {
		return fmt.Errorf("clean -fd: %w", err)
	}
	return nil
}

// DirtyCount returns the number of uncommitted changes in a worktree.
func DirtyCount(path string) int {
	out, err := git([]string{"status", "--porcelain"}, path)
	if err != nil || strings.TrimSpace(out) == "" {
		return 0
	}
	return len(strings.Split(strings.TrimSpace(out), "\n"))
}

// Reset discards all changes, staged additions, and untracked files in a worktree.
func Reset(path string) error {
	// Unstage everything (so newly staged files become untracked)
	if _, err := git([]string{"reset", "HEAD"}, path); err != nil {
		return err
	}
	// Restore all tracked files
	if _, err := git([]string{"checkout", "."}, path); err != nil {
		return err
	}
	// Remove untracked files and directories
	_, err := git([]string{"clean", "-fd"}, path)
	return err
}

// IsJackIn checks if a worktree belongs to jackin (optionally for a specific project).
func IsJackIn(entry Info, project string) bool {
	dir := filepath.Base(entry.Path)
	if !strings.HasPrefix(dir, Prefix) {
		return false
	}
	if project != "" {
		return strings.HasPrefix(dir, Prefix+project+"-")
	}
	return true
}

// StaleFrom filters a pre-fetched worktree list, returning jackin worktrees
// for the given project that don't match any of the provided active worker names.
func StaleFrom(all []Info, project string, activeNames []string) []Info {
	active := make(map[string]bool, len(activeNames))
	for _, n := range activeNames {
		active[DirName(project, n)] = true
	}

	var stale []Info
	for _, e := range all {
		if !IsJackIn(e, project) {
			continue
		}
		if !active[filepath.Base(e.Path)] {
			stale = append(stale, e)
		}
	}
	return stale
}

// Stale returns jackin worktrees for the given project that don't match any
// of the provided active worker names.
func Stale(base, project string, activeNames []string) ([]Info, error) {
	all, err := List(base)
	if err != nil {
		return nil, err
	}
	return StaleFrom(all, project, activeNames), nil
}

// RemoveAll force-removes the given worktrees.
// Returns paths successfully removed and any errors encountered.
func RemoveAll(base string, entries []Info) (removed []string, errs []error) {
	for _, e := range entries {
		if err := Remove(e.Path, base); err != nil {
			errs = append(errs, fmt.Errorf("removing %s: %w", e.Path, err))
			continue
		}
		removed = append(removed, e.Path)
	}
	return removed, errs
}
