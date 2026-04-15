package worktree

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestMain(m *testing.M) {
	// Clear git env vars that pre-commit hooks set, so our tests
	// don't accidentally operate on the parent repo.
	os.Unsetenv("GIT_DIR")
	os.Unsetenv("GIT_WORK_TREE")
	os.Unsetenv("GIT_INDEX_FILE")
	os.Exit(m.Run())
}

func TestDirName(t *testing.T) {
	got := DirName("myproj", "alice")
	want := ".w-myproj-alice"
	if got != want {
		t.Errorf("DirName = %q, want %q", got, want)
	}
}

func TestBranchName(t *testing.T) {
	got := BranchName("myproj", "alice")
	want := "jackin/myproj/alice"
	if got != want {
		t.Errorf("BranchName = %q, want %q", got, want)
	}
}

func TestIsJackIn(t *testing.T) {
	tests := []struct {
		name    string
		entry   Info
		project string
		want    bool
	}{
		{
			name:    "matching project worker",
			entry:   Info{Path: "/repo/.w-myproj-alice"},
			project: "myproj",
			want:    true,
		},
		{
			name:    "different project",
			entry:   Info{Path: "/repo/.w-other-alice"},
			project: "myproj",
			want:    false,
		},
		{
			name:    "empty project matches any jackin worktree",
			entry:   Info{Path: "/repo/.w-anything-bob"},
			project: "",
			want:    true,
		},
		{
			name:    "non-jackin worktree",
			entry:   Info{Path: "/repo/regular-dir"},
			project: "",
			want:    false,
		},
		{
			name:    "main worktree",
			entry:   Info{Path: "/repo"},
			project: "myproj",
			want:    false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsJackIn(tt.entry, tt.project)
			if got != tt.want {
				t.Errorf("IsJackIn(%q, %q) = %v, want %v", tt.entry.Path, tt.project, got, tt.want)
			}
		})
	}
}

func TestStaleFrom(t *testing.T) {
	all := []Info{
		{Path: "/repo"},                      // main worktree
		{Path: "/repo/.w-proj-alice"},        // active worker
		{Path: "/repo/.w-proj-bob"},          // active worker
		{Path: "/repo/.w-proj-removed"},      // orphan: worker removed from config
		{Path: "/repo/.w-proj-also-removed"}, // orphan: worker removed from config
		{Path: "/repo/.w-otherproj-charlie"}, // different project
	}

	tests := []struct {
		name        string
		project     string
		activeNames []string
		wantPaths   []string
	}{
		{
			name:        "filters out active workers",
			project:     "proj",
			activeNames: []string{"alice", "bob"},
			wantPaths:   []string{"/repo/.w-proj-removed", "/repo/.w-proj-also-removed"},
		},
		{
			name:        "all workers active returns empty",
			project:     "proj",
			activeNames: []string{"alice", "bob", "removed", "also-removed"},
			wantPaths:   nil,
		},
		{
			name:        "no active workers returns all project worktrees",
			project:     "proj",
			activeNames: nil,
			wantPaths:   []string{"/repo/.w-proj-alice", "/repo/.w-proj-bob", "/repo/.w-proj-removed", "/repo/.w-proj-also-removed"},
		},
		{
			name:        "different project not included",
			project:     "proj",
			activeNames: []string{"alice", "bob"},
			wantPaths:   []string{"/repo/.w-proj-removed", "/repo/.w-proj-also-removed"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := StaleFrom(all, tt.project, tt.activeNames)
			if len(got) != len(tt.wantPaths) {
				t.Fatalf("StaleFrom returned %d entries, want %d", len(got), len(tt.wantPaths))
			}
			for i, entry := range got {
				if entry.Path != tt.wantPaths[i] {
					t.Errorf("StaleFrom[%d].Path = %q, want %q", i, entry.Path, tt.wantPaths[i])
				}
			}
		})
	}
}

// initBareRemote creates a bare "origin" repo with one commit on "main",
// and returns the path to a standalone repo that uses it as remote.
// Uses git init + remote add (not clone) to avoid inheriting parent repo context.
func initBareRemote(t *testing.T) string {
	t.Helper()
	bare := filepath.Join(t.TempDir(), "origin.git")
	run(t, "", "git", "init", "--bare", bare)

	repo := filepath.Join(t.TempDir(), "repo")
	os.MkdirAll(repo, 0o755)
	run(t, repo, "git", "init")
	run(t, repo, "git", "remote", "add", "origin", bare)
	run(t, repo, "git", "config", "user.email", "test@test.com")
	run(t, repo, "git", "config", "user.name", "test")

	// seed commit so main exists
	os.WriteFile(filepath.Join(repo, "README.md"), []byte("init"), 0o644)
	run(t, repo, "git", "add", "README.md")
	run(t, repo, "git", "commit", "-m", "init")
	run(t, repo, "git", "push", "-u", "origin", "main")
	return repo
}

func run(t *testing.T, dir string, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	// Use clean env (inherits current but strips git vars via TestMain)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v failed: %v\n%s", name, args, err, out)
	}
}

func TestPath(t *testing.T) {
	got := Path("/repo", "proj", "alice")
	want := "/repo/.w-proj-alice"
	if got != want {
		t.Errorf("Path = %q, want %q", got, want)
	}
}

func TestCreateAndList(t *testing.T) {
	clone := initBareRemote(t)

	wt, err := Create(clone, "proj", "alice")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Verify worktree directory exists
	if _, err := os.Stat(wt); err != nil {
		t.Fatalf("worktree dir should exist: %v", err)
	}

	// Verify it shows up in List
	entries, err := List(clone)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	found := false
	for _, e := range entries {
		if filepath.Base(e.Path) == filepath.Base(wt) {
			found = true
			if e.Branch != "refs/heads/"+BranchName("proj", "alice") {
				t.Errorf("branch = %q, want refs/heads/%s", e.Branch, BranchName("proj", "alice"))
			}
		}
	}
	if !found {
		t.Errorf("created worktree not found in List (wt=%s, entries=%v)", wt, entries)
	}
}

func TestCreateExistingBranch(t *testing.T) {
	clone := initBareRemote(t)

	// Create then remove worktree, leaving the branch behind
	wt, err := Create(clone, "proj", "bob")
	if err != nil {
		t.Fatal(err)
	}
	if err := Remove(wt, clone); err != nil {
		t.Fatal(err)
	}

	// Second create should reuse the existing branch
	wt2, err := Create(clone, "proj", "bob")
	if err != nil {
		t.Fatalf("Create with existing branch: %v", err)
	}
	if _, err := os.Stat(wt2); err != nil {
		t.Fatalf("worktree dir should exist: %v", err)
	}
}

func TestDirtyCount(t *testing.T) {
	clone := initBareRemote(t)
	wt, _ := Create(clone, "proj", "alice")

	// Clean worktree
	if n := DirtyCount(wt); n != 0 {
		t.Errorf("expected 0 dirty files, got %d", n)
	}

	// Add an untracked file
	os.WriteFile(filepath.Join(wt, "new.txt"), []byte("new"), 0o644)
	if n := DirtyCount(wt); n != 1 {
		t.Errorf("expected 1 dirty file, got %d", n)
	}

	// Nonexistent path returns 0
	if n := DirtyCount("/nonexistent"); n != 0 {
		t.Errorf("expected 0 for bad path, got %d", n)
	}
}

func TestReset(t *testing.T) {
	clone := initBareRemote(t)
	wt, _ := Create(clone, "proj", "alice")

	// Dirty the worktree
	os.WriteFile(filepath.Join(wt, "untracked.txt"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(wt, "README.md"), []byte("modified"), 0o644)

	if n := DirtyCount(wt); n == 0 {
		t.Fatal("expected dirty worktree before reset")
	}

	if err := Reset(wt); err != nil {
		t.Fatalf("Reset: %v", err)
	}

	if n := DirtyCount(wt); n != 0 {
		t.Errorf("expected 0 dirty files after reset, got %d", n)
	}
}

func TestRemove(t *testing.T) {
	clone := initBareRemote(t)
	wt, _ := Create(clone, "proj", "alice")

	if err := Remove(wt, clone); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	if _, err := os.Stat(wt); err == nil {
		t.Error("worktree directory should be gone after Remove")
	}
}

func TestRemoveAll(t *testing.T) {
	clone := initBareRemote(t)
	wt1, _ := Create(clone, "proj", "a")
	wt2, _ := Create(clone, "proj", "b")

	entries := []Info{{Path: wt1}, {Path: wt2}}
	removed, errs := RemoveAll(clone, entries)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(removed) != 2 {
		t.Errorf("expected 2 removed, got %d", len(removed))
	}
}

func TestRefresh(t *testing.T) {
	clone := initBareRemote(t)

	// Create a worktree like jackin does
	wt, err := Create(clone, "proj", "alice")
	if err != nil {
		t.Fatal(err)
	}

	// Make a commit in the worktree (simulates worker doing work)
	run(t, wt, "git", "config", "user.email", "test@test.com")
	run(t, wt, "git", "config", "user.name", "test")
	os.WriteFile(filepath.Join(wt, "work.txt"), []byte("done"), 0o644)
	run(t, wt, "git", "add", "work.txt")
	run(t, wt, "git", "commit", "-m", "worker commit")

	// Push a new commit to origin/main from the main clone (simulates merge)
	os.WriteFile(filepath.Join(clone, "merged.txt"), []byte("merged"), 0o644)
	run(t, clone, "git", "add", "merged.txt")
	run(t, clone, "git", "commit", "-m", "merged work")
	run(t, clone, "git", "push", "origin", "main")

	// Refresh the worktree
	if err := Refresh(wt, "proj", "alice", "main"); err != nil {
		t.Fatalf("Refresh failed: %v", err)
	}

	// Verify: worktree should be on the worker branch
	branch, err := git([]string{"rev-parse", "--abbrev-ref", "HEAD"}, wt)
	if err != nil {
		t.Fatal(err)
	}
	if branch != BranchName("proj", "alice") {
		t.Errorf("branch = %q, want %q", branch, BranchName("proj", "alice"))
	}

	// Verify: merged.txt should exist (pulled from origin/main)
	if _, err := os.Stat(filepath.Join(wt, "merged.txt")); err != nil {
		t.Error("expected merged.txt to exist after refresh")
	}

	// Verify: work.txt should NOT exist (branch was recreated from origin/main)
	if _, err := os.Stat(filepath.Join(wt, "work.txt")); err == nil {
		t.Error("expected work.txt to be gone after refresh")
	}
}
