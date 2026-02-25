package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAssemblePrompt(t *testing.T) {
	files := map[string]string{
		"git-myproject.log":  "=== SNAPSHOT 10:15 ===\ndiff content\n",
		"notes-myproject.md": "### At 10:20\nStarted work\n",
	}

	prompt := assemblePrompt("myproject", "2024-01-15", files)

	// Check project name
	if !strings.Contains(prompt, `"myproject"`) {
		t.Error("prompt should contain project name")
	}

	// Check date
	if !strings.Contains(prompt, "2024-01-15") {
		t.Error("prompt should contain date")
	}

	// Check file sections
	if !strings.Contains(prompt, "--- git-myproject.log ---") {
		t.Error("prompt should contain git log section")
	}
	if !strings.Contains(prompt, "--- notes-myproject.md ---") {
		t.Error("prompt should contain notes section")
	}
	if !strings.Contains(prompt, "diff content") {
		t.Error("prompt should contain file contents")
	}
}

func TestAssemblePromptGitOnly(t *testing.T) {
	files := map[string]string{
		"git-myproject.log": "=== SNAPSHOT 10:15 ===\ndiff content\n",
	}

	prompt := assemblePrompt("myproject", "2024-01-15", files)

	if !strings.Contains(prompt, "--- git-myproject.log ---") {
		t.Error("prompt should contain git log section")
	}
	if strings.Contains(prompt, "--- notes-myproject.md ---") {
		t.Error("prompt should NOT contain notes section when notes don't exist")
	}
}

func TestAssemblePromptNotesOnly(t *testing.T) {
	files := map[string]string{
		"notes-myproject.md": "### At 10:20\nsome notes\n",
	}

	prompt := assemblePrompt("myproject", "2024-01-15", files)

	if strings.Contains(prompt, "--- git-myproject.log ---") {
		t.Error("prompt should NOT contain git log section when git log doesn't exist")
	}
	if !strings.Contains(prompt, "--- notes-myproject.md ---") {
		t.Error("prompt should contain notes section")
	}
}

func TestRunGenPrompt(t *testing.T) {
	tmp := t.TempDir()
	rawDir := filepath.Join(tmp, "raw")
	t.Setenv("DEVLOG_RAW_DIR", rawDir)
	t.Setenv("DEVLOG_LOG_DIR", filepath.Join(tmp, "log"))

	date := "2024-01-15"
	dateDir := filepath.Join(rawDir, date)
	os.MkdirAll(dateDir, 0o755)
	os.WriteFile(filepath.Join(dateDir, "git-myproject.log"),
		[]byte("=== SNAPSHOT 10:00 ===\ndiff content\n"), 0o644)
	os.WriteFile(filepath.Join(dateDir, "notes-myproject.md"),
		[]byte("### At 10:20\nStarted work\n"), 0o644)

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	cfg := Config{}
	err := runGenPrompt(cfg, date)

	w.Close()
	os.Stdout = oldStdout

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out, _ := io.ReadAll(r)
	s := string(out)

	if !strings.Contains(s, `"myproject"`) {
		t.Error("output should contain project name")
	}
	if !strings.Contains(s, "2024-01-15") {
		t.Error("output should contain date")
	}
	if !strings.Contains(s, "diff content") {
		t.Error("output should contain git log data")
	}
	if !strings.Contains(s, "Started work") {
		t.Error("output should contain notes data")
	}
}

func TestRunGenPromptNoData(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("DEVLOG_RAW_DIR", filepath.Join(tmp, "raw"))
	t.Setenv("DEVLOG_LOG_DIR", filepath.Join(tmp, "log"))

	cfg := Config{}
	err := runGenPrompt(cfg, "2024-01-15")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunGenPromptMultipleProjects(t *testing.T) {
	tmp := t.TempDir()
	rawDir := filepath.Join(tmp, "raw")
	t.Setenv("DEVLOG_RAW_DIR", rawDir)
	t.Setenv("DEVLOG_LOG_DIR", filepath.Join(tmp, "log"))

	date := "2024-01-15"
	dateDir := filepath.Join(rawDir, date)
	os.MkdirAll(dateDir, 0o755)
	os.WriteFile(filepath.Join(dateDir, "git-alpha.log"),
		[]byte("=== SNAPSHOT 10:00 ===\nalpha diff\n"), 0o644)
	os.WriteFile(filepath.Join(dateDir, "git-beta.log"),
		[]byte("=== SNAPSHOT 11:00 ===\nbeta diff\n"), 0o644)

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	cfg := Config{}
	err := runGenPrompt(cfg, date)

	w.Close()
	os.Stdout = oldStdout

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out, _ := io.ReadAll(r)
	s := string(out)

	if !strings.Contains(s, "=== alpha ===") {
		t.Error("output should contain alpha project header")
	}
	if !strings.Contains(s, "=== beta ===") {
		t.Error("output should contain beta project header")
	}
	if !strings.Contains(s, "alpha diff") {
		t.Error("output should contain alpha data")
	}
	if !strings.Contains(s, "beta diff") {
		t.Error("output should contain beta data")
	}
}

func TestRunGenNoRawData(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("DEVLOG_RAW_DIR", filepath.Join(tmp, "raw"))
	t.Setenv("DEVLOG_LOG_DIR", filepath.Join(tmp, "log"))

	cfg := Config{}
	err := runGen(cfg, "2024-01-15")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunGenStalenessCheck(t *testing.T) {
	tmp := t.TempDir()
	rawDir := filepath.Join(tmp, "raw")
	logDir := filepath.Join(tmp, "log")
	t.Setenv("DEVLOG_RAW_DIR", rawDir)
	t.Setenv("DEVLOG_LOG_DIR", logDir)

	date := "2024-01-15"
	dateDir := filepath.Join(rawDir, date)
	os.MkdirAll(dateDir, 0o755)
	os.MkdirAll(logDir, 0o755)

	// Create raw data with old timestamp (template-resolved default path)
	rawFile := filepath.Join(dateDir, "git-test.log")
	os.WriteFile(rawFile, []byte("=== SNAPSHOT 10:00 ===\ndiff\n"), 0o644)
	past := time.Now().Add(-1 * time.Hour)
	os.Chtimes(rawFile, past, past)

	// Create summary with newer timestamp
	summaryPath := filepath.Join(logDir, date+".md")
	os.WriteFile(summaryPath, []byte("# existing summary\n"), 0o644)

	cfg := Config{}
	err := runGen(cfg, date)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Summary should still exist (not regenerated)
	content, _ := os.ReadFile(summaryPath)
	if !strings.Contains(string(content), "existing summary") {
		t.Error("summary should not have been regenerated")
	}
}

func TestRunGenWithMockSummarizer(t *testing.T) {
	tmp := t.TempDir()
	rawDir := filepath.Join(tmp, "raw")
	logDir := filepath.Join(tmp, "log")
	t.Setenv("DEVLOG_RAW_DIR", rawDir)
	t.Setenv("DEVLOG_LOG_DIR", logDir)

	// Create a mock summarizer script
	mockBin := filepath.Join(tmp, "bin")
	os.MkdirAll(mockBin, 0o755)
	mockSummarizer := filepath.Join(mockBin, "mysummarizer")
	os.WriteFile(mockSummarizer, []byte("#!/bin/sh\necho 'This is a test summary.'\n"), 0o755)
	t.Setenv("PATH", mockBin+":"+os.Getenv("PATH"))

	date := "2024-01-15"
	dateDir := filepath.Join(rawDir, date)
	os.MkdirAll(dateDir, 0o755)
	os.WriteFile(filepath.Join(dateDir, "git-myproject.log"),
		[]byte("=== SNAPSHOT 10:00 ===\ndiff content\n\n"), 0o644)

	cfg := Config{
		GenCmd: "mysummarizer",
	}
	err := runGen(cfg, date)
	if err != nil {
		t.Fatalf("runGen: %v", err)
	}

	summaryPath := filepath.Join(logDir, date+".md")
	content, err := os.ReadFile(summaryPath)
	if err != nil {
		t.Fatalf("reading summary: %v", err)
	}

	s := string(content)
	if !strings.Contains(s, "# 2024-01-15") {
		t.Error("summary should contain date heading")
	}
	if !strings.Contains(s, "## myproject") {
		t.Error("summary should contain project heading")
	}
	if !strings.Contains(s, "This is a test summary.") {
		t.Error("summary should contain summarizer output")
	}
}
