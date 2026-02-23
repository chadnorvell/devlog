package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func resolveRepoRoot(dir string) (string, error) {
	cmd := exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("not a git repository: %s", dir)
	}
	return strings.TrimSpace(string(out)), nil
}

// takeSnapshot captures the current state of a repo using the shadow index
// technique. It returns the diff string and whether anything was written.
// If prevDiff matches the current diff, the snapshot is skipped (dedup).
// logFile is the resolved path where the snapshot will be appended.
func takeSnapshot(repoPath, projectName, logFile, prevDiff string) (diff string, err error) {
	shadowIndex := filepath.Join(repoPath, ".git", "devlog_shadow_index")

	// Step 1: git add -A with shadow index
	addCmd := exec.Command("git", "-C", repoPath, "add", "-A")
	addCmd.Env = append(os.Environ(), "GIT_INDEX_FILE="+shadowIndex)
	if out, err := addCmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("git add: %s: %w", strings.TrimSpace(string(out)), err)
	}

	// Step 2: git diff --no-color HEAD with shadow index
	diffCmd := exec.Command("git", "-C", repoPath, "diff", "--no-color", "HEAD")
	diffCmd.Env = append(os.Environ(), "GIT_INDEX_FILE="+shadowIndex)
	out, err := diffCmd.Output()
	if err != nil {
		return "", fmt.Errorf("git diff: %w", err)
	}

	diff = string(out)

	// Empty diff: nothing to write
	if strings.TrimSpace(diff) == "" {
		return "", nil
	}

	// Dedup: skip if identical to previous
	if diff == prevDiff {
		return diff, nil
	}

	// Write snapshot to raw file
	if err := os.MkdirAll(filepath.Dir(logFile), 0o755); err != nil {
		return "", fmt.Errorf("creating raw dir: %w", err)
	}

	f, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return "", fmt.Errorf("opening log file: %w", err)
	}
	defer f.Close()

	now := time.Now()
	header := fmt.Sprintf("=== SNAPSHOT %02d:%02d ===\n", now.Hour(), now.Minute())
	if _, err := f.WriteString(header + diff + "\n"); err != nil {
		return "", fmt.Errorf("writing snapshot: %w", err)
	}

	return diff, nil
}
