package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func initTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	cmds := [][]string{
		{"git", "init", dir},
		{"git", "-C", dir, "config", "user.email", "test@test.com"},
		{"git", "-C", dir, "config", "user.name", "Test"},
	}
	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("init cmd %v: %s: %v", args, out, err)
		}
	}

	// Create initial commit so HEAD exists
	readmePath := filepath.Join(dir, "README.md")
	os.WriteFile(readmePath, []byte("# test\n"), 0o644)
	exec.Command("git", "-C", dir, "add", "-A").Run()
	exec.Command("git", "-C", dir, "commit", "-m", "initial").Run()

	return dir
}

func TestResolveRepoRoot(t *testing.T) {
	repo := initTestRepo(t)

	// From repo root
	got, err := resolveRepoRoot(repo)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != repo {
		t.Errorf("got %q, want %q", got, repo)
	}

	// From subdirectory
	sub := filepath.Join(repo, "subdir")
	os.MkdirAll(sub, 0o755)
	got, err = resolveRepoRoot(sub)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != repo {
		t.Errorf("got %q, want %q", got, repo)
	}

	// From non-repo
	notRepo := t.TempDir()
	_, err = resolveRepoRoot(notRepo)
	if err == nil {
		t.Error("expected error for non-repo dir")
	}
}

func TestSnapshotNewDiff(t *testing.T) {
	repo := initTestRepo(t)
	logFile := filepath.Join(t.TempDir(), "raw", "2024-01-15", "git-test-project.log")

	// Make a change
	os.WriteFile(filepath.Join(repo, "main.go"), []byte("package main\n"), 0o644)

	diff, err := takeSnapshot(repo, "test-project", logFile, "")
	if err != nil {
		t.Fatalf("takeSnapshot: %v", err)
	}
	if diff == "" {
		t.Fatal("expected non-empty diff")
	}

	// Verify log file
	content, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("reading log: %v", err)
	}

	s := string(content)
	if !strings.Contains(s, "=== SNAPSHOT") {
		t.Error("missing SNAPSHOT header")
	}
	if !strings.Contains(s, "main.go") {
		t.Error("diff should mention main.go")
	}
}

func TestSnapshotDedup(t *testing.T) {
	repo := initTestRepo(t)
	logFile := filepath.Join(t.TempDir(), "raw", "2024-01-15", "git-test-project.log")

	// Make a change
	os.WriteFile(filepath.Join(repo, "main.go"), []byte("package main\n"), 0o644)

	// First snapshot
	diff1, err := takeSnapshot(repo, "test-project", logFile, "")
	if err != nil {
		t.Fatalf("first snapshot: %v", err)
	}

	// Second snapshot with same prevDiff — should dedup
	diff2, err := takeSnapshot(repo, "test-project", logFile, diff1)
	if err != nil {
		t.Fatalf("second snapshot: %v", err)
	}
	if diff2 != diff1 {
		t.Error("expected same diff returned")
	}

	// Log file should only have one snapshot
	content, _ := os.ReadFile(logFile)
	count := strings.Count(string(content), "=== SNAPSHOT")
	if count != 1 {
		t.Errorf("expected 1 snapshot in log, got %d", count)
	}
}

func TestSnapshotEmptyDiff(t *testing.T) {
	repo := initTestRepo(t)
	logFile := filepath.Join(t.TempDir(), "raw", "2024-01-15", "git-test-project.log")

	// No changes — diff should be empty
	diff, err := takeSnapshot(repo, "test-project", logFile, "")
	if err != nil {
		t.Fatalf("takeSnapshot: %v", err)
	}
	if diff != "" {
		t.Errorf("expected empty diff, got %q", diff)
	}

	// Log file should not exist
	if _, err := os.Stat(logFile); !os.IsNotExist(err) {
		t.Error("log file should not exist for empty diff")
	}
}

func TestSnapshotUntrackedFiles(t *testing.T) {
	repo := initTestRepo(t)
	logFile := filepath.Join(t.TempDir(), "raw", "2024-01-15", "git-test-project.log")

	// Create a new untracked file
	os.WriteFile(filepath.Join(repo, "newfile.txt"), []byte("hello\n"), 0o644)

	diff, err := takeSnapshot(repo, "test-project", logFile, "")
	if err != nil {
		t.Fatalf("takeSnapshot: %v", err)
	}
	if !strings.Contains(diff, "newfile.txt") {
		t.Error("untracked file should appear in diff")
	}
}

func TestSnapshotDoesNotDisturbRealIndex(t *testing.T) {
	repo := initTestRepo(t)
	logFile := filepath.Join(t.TempDir(), "raw", "2024-01-15", "git-test-project.log")

	// Stage a file in the real index
	os.WriteFile(filepath.Join(repo, "staged.go"), []byte("package main\n"), 0o644)
	exec.Command("git", "-C", repo, "add", "staged.go").Run()

	// Create another file but don't stage it
	os.WriteFile(filepath.Join(repo, "unstaged.go"), []byte("package main\n"), 0o644)

	// Take snapshot
	_, err := takeSnapshot(repo, "test-project", logFile, "")
	if err != nil {
		t.Fatalf("takeSnapshot: %v", err)
	}

	// Verify real index still has only staged.go staged
	cmd := exec.Command("git", "-C", repo, "diff", "--cached", "--name-only")
	out, _ := cmd.Output()
	staged := strings.TrimSpace(string(out))
	if staged != "staged.go" {
		t.Errorf("real index disturbed: staged files = %q", staged)
	}

	// Verify unstaged.go is still untracked
	cmd = exec.Command("git", "-C", repo, "ls-files", "--others", "--exclude-standard")
	out, _ = cmd.Output()
	untracked := strings.TrimSpace(string(out))
	if untracked != "unstaged.go" {
		t.Errorf("real index disturbed: untracked = %q", untracked)
	}
}

func TestSnapshotFormat(t *testing.T) {
	repo := initTestRepo(t)
	logFile := filepath.Join(t.TempDir(), "raw", "2024-01-15", "git-myproject.log")

	os.WriteFile(filepath.Join(repo, "file.txt"), []byte("content\n"), 0o644)

	takeSnapshot(repo, "myproject", logFile, "")

	content, _ := os.ReadFile(logFile)
	lines := strings.Split(string(content), "\n")

	// First line should match === SNAPSHOT HH:MM ===
	if len(lines) < 2 {
		t.Fatal("log file too short")
	}
	if !strings.HasPrefix(lines[0], "=== SNAPSHOT ") || !strings.HasSuffix(lines[0], " ===") {
		t.Errorf("header format wrong: %q", lines[0])
	}
	// Last non-empty content should be followed by a blank line
	s := string(content)
	if !strings.HasSuffix(s, "\n\n") {
		t.Error("snapshot should end with blank line")
	}
}
