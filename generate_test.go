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
	err := runGenPrompt(cfg, State{}, date)

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
	err := runGenPrompt(cfg, State{}, "2024-01-15")
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
	err := runGenPrompt(cfg, State{}, date)

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
	err := runGen(cfg, State{}, "2024-01-15")
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
	err := runGen(cfg, State{}, date)
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
	err := runGen(cfg, State{}, date)
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

func TestAssemblePromptWithTermLog(t *testing.T) {
	files := map[string]string{
		"git-myproject.log":      "=== SNAPSHOT 10:15 ===\ndiff content\n",
		"term-myproject-1.log":   "$ go test ./...\nPASS\n",
	}

	prompt := assemblePrompt("myproject", "2024-01-15", files)

	if !strings.Contains(prompt, "--- term-myproject-1.log ---") {
		t.Error("prompt should contain terminal log section")
	}
	if !strings.Contains(prompt, "go test") {
		t.Error("prompt should contain terminal log contents")
	}
	if !strings.Contains(prompt, "term-myproject*.log") {
		t.Error("prompt should describe terminal log data source")
	}
}

func TestRunGenPromptWithTermLog(t *testing.T) {
	tmp := t.TempDir()
	rawDir := filepath.Join(tmp, "raw")
	t.Setenv("DEVLOG_RAW_DIR", rawDir)
	t.Setenv("DEVLOG_LOG_DIR", filepath.Join(tmp, "log"))

	date := "2024-01-15"
	dateDir := filepath.Join(rawDir, date)
	os.MkdirAll(dateDir, 0o755)
	os.WriteFile(filepath.Join(dateDir, "git-myproject.log"),
		[]byte("=== SNAPSHOT 10:00 ===\ndiff content\n"), 0o644)
	os.WriteFile(filepath.Join(dateDir, "term-myproject-session1.log"),
		[]byte("$ make build\nok\n"), 0o644)
	os.WriteFile(filepath.Join(dateDir, "term-myproject-session2.log"),
		[]byte("$ go test\nPASS\n"), 0o644)

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	cfg := Config{}
	err := runGenPrompt(cfg, State{}, date)

	w.Close()
	os.Stdout = oldStdout

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out, _ := io.ReadAll(r)
	s := string(out)

	if !strings.Contains(s, "make build") {
		t.Error("output should contain first terminal log data")
	}
	if !strings.Contains(s, "go test") {
		t.Error("output should contain second terminal log data")
	}
}

func TestRunGenPromptTermLogNotDiscovery(t *testing.T) {
	tmp := t.TempDir()
	rawDir := filepath.Join(tmp, "raw")
	t.Setenv("DEVLOG_RAW_DIR", rawDir)
	t.Setenv("DEVLOG_LOG_DIR", filepath.Join(tmp, "log"))

	date := "2024-01-15"
	dateDir := filepath.Join(rawDir, date)
	os.MkdirAll(dateDir, 0o755)
	// Only terminal logs, no git or notes
	os.WriteFile(filepath.Join(dateDir, "term-orphan-session1.log"),
		[]byte("$ echo hello\nhello\n"), 0o644)

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	cfg := Config{}
	err := runGenPrompt(cfg, State{}, date)

	w.Close()
	os.Stdout = oldStdout

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out, _ := io.ReadAll(r)
	s := string(out)

	if strings.Contains(s, "orphan") {
		t.Error("project with only terminal logs should not be discovered")
	}
}

func TestCollectRawFileMtimeIncludesTermLogs(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("DEVLOG_RAW_DIR", tmp)

	date := "2024-01-15"
	dateDir := filepath.Join(tmp, date)
	os.MkdirAll(dateDir, 0o755)

	// Create a git log with old mtime
	gitFile := filepath.Join(dateDir, "git-proj.log")
	os.WriteFile(gitFile, []byte("diff"), 0o644)
	past := time.Now().Add(-2 * time.Hour)
	os.Chtimes(gitFile, past, past)

	// Create a terminal log with newer mtime
	termFile := filepath.Join(dateDir, "term-proj-s1.log")
	os.WriteFile(termFile, []byte("$ cmd"), 0o644)
	recent := time.Now().Add(-10 * time.Minute)
	os.Chtimes(termFile, recent, recent)

	cfg := Config{}
	maxMtime := collectRawFileMtime(cfg, State{}, date)

	if maxMtime.Before(recent) {
		t.Errorf("maxMtime should reflect terminal log time, got %v, want at least %v", maxMtime, recent)
	}
}

func TestDiscoverAllProjectsIncludesClaudeCode(t *testing.T) {
	tmp := t.TempDir()
	rawDir := filepath.Join(tmp, "raw")
	claudeDir := filepath.Join(tmp, "claude")
	t.Setenv("DEVLOG_RAW_DIR", rawDir)

	date := "2024-06-15"

	// No raw data files — only Claude Code sessions
	repoPath := "/home/user/dev/myproject"
	projDir := filepath.Join(claudeDir, "-home-user-dev-myproject")
	os.MkdirAll(projDir, 0o755)

	lines := jsonLine(t, map[string]interface{}{
		"type": "user", "timestamp": "2024-06-15T10:00:00.000Z",
		"message": map[string]interface{}{"role": "user", "content": "hello"},
	})
	os.WriteFile(filepath.Join(projDir, "session.jsonl"), []byte(lines+"\n"), 0o644)

	ccDir := claudeDir
	cfg := Config{ClaudeCodeDir: &ccDir}
	state := State{Watched: []WatchEntry{{Path: repoPath, Name: "myproject"}}}

	projects := discoverAllProjects(cfg, state, date)

	found := false
	for _, p := range projects {
		if p == "myproject" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected myproject in discovered projects, got %v", projects)
	}
}

func TestRunGenPromptWithClaudeCode(t *testing.T) {
	tmp := t.TempDir()
	rawDir := filepath.Join(tmp, "raw")
	claudeDir := filepath.Join(tmp, "claude")
	t.Setenv("DEVLOG_RAW_DIR", rawDir)
	t.Setenv("DEVLOG_LOG_DIR", filepath.Join(tmp, "log"))

	date := "2024-06-15"

	// Create raw data so project is discovered
	dateDir := filepath.Join(rawDir, date)
	os.MkdirAll(dateDir, 0o755)
	os.WriteFile(filepath.Join(dateDir, "git-myproject.log"),
		[]byte("=== SNAPSHOT 10:00 ===\ndiff content\n"), 0o644)

	// Create Claude Code session
	repoPath := "/home/user/dev/myproject"
	projDir := filepath.Join(claudeDir, "-home-user-dev-myproject")
	os.MkdirAll(projDir, 0o755)

	lines := []string{
		jsonLine(t, map[string]interface{}{
			"type": "user", "timestamp": "2024-06-15T10:30:00.000Z", "sessionId": "s1",
			"message": map[string]interface{}{"role": "user", "content": "Fix the parser bug"},
		}),
		jsonLine(t, map[string]interface{}{
			"type": "assistant", "timestamp": "2024-06-15T10:31:00.000Z", "sessionId": "s1",
			"message": map[string]interface{}{
				"role": "assistant", "content": []map[string]interface{}{
					{"type": "text", "text": "I found the issue in parser.go."},
				},
			},
		}),
	}
	os.WriteFile(filepath.Join(projDir, "session.jsonl"),
		[]byte(strings.Join(lines, "\n")+"\n"), 0o644)

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	ccDir := claudeDir
	cfg := Config{ClaudeCodeDir: &ccDir}
	state := State{Watched: []WatchEntry{{Path: repoPath, Name: "myproject"}}}
	err := runGenPrompt(cfg, state, date)

	w.Close()
	os.Stdout = oldStdout

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out, _ := io.ReadAll(r)
	s := string(out)

	if !strings.Contains(s, "--- claude-code-sessions.txt ---") {
		t.Error("output should contain Claude Code sessions section")
	}
	if !strings.Contains(s, "Fix the parser bug") {
		t.Error("output should contain user prompt from Claude Code session")
	}
	if !strings.Contains(s, "I found the issue in parser.go.") {
		t.Error("output should contain assistant response from Claude Code session")
	}
}

func TestAssemblePromptWithClaudeCode(t *testing.T) {
	files := map[string]string{
		"git-myproject.log":        "=== SNAPSHOT 10:15 ===\ndiff content\n",
		"claude-code-sessions.txt": "=== SESSION started 10:22 ===\n\n> Help me fix the test\n\nThe test fails because...\n",
	}

	prompt := assemblePrompt("myproject", "2024-06-15", files)

	if !strings.Contains(prompt, "--- claude-code-sessions.txt ---") {
		t.Error("prompt should contain Claude Code sessions section")
	}
	if !strings.Contains(prompt, "Help me fix the test") {
		t.Error("prompt should contain Claude Code session content")
	}
	if !strings.Contains(prompt, "claude-code-sessions.txt: Preprocessed transcripts") {
		t.Error("prompt should contain Claude Code data source description")
	}
}

func TestCollectRawFileMtimeIncludesClaudeCode(t *testing.T) {
	tmp := t.TempDir()
	claudeDir := filepath.Join(tmp, "claude")
	t.Setenv("DEVLOG_RAW_DIR", filepath.Join(tmp, "raw"))

	date := "2024-06-15"

	// Create Claude Code session file with recent mtime
	repoPath := "/home/user/dev/proj"
	projDir := filepath.Join(claudeDir, "-home-user-dev-proj")
	os.MkdirAll(projDir, 0o755)
	jsonlFile := filepath.Join(projDir, "session.jsonl")
	os.WriteFile(jsonlFile, []byte(`{"type":"user"}`+"\n"), 0o644)
	recent := time.Now().Add(-5 * time.Minute)
	os.Chtimes(jsonlFile, recent, recent)

	ccDir := claudeDir
	cfg := Config{ClaudeCodeDir: &ccDir}
	state := State{Watched: []WatchEntry{{Path: repoPath, Name: "proj"}}}
	maxMtime := collectRawFileMtime(cfg, state, date)

	if maxMtime.Before(recent) {
		t.Errorf("maxMtime should include Claude Code JSONL time, got %v, want at least %v", maxMtime, recent)
	}
}

