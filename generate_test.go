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
		"comp-git-myproject.md": "Compressed git summary\n",
		"notes.md":              "### At 10:20 #myproject\nStarted work\n",
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
	if !strings.Contains(prompt, "--- comp-git-myproject.md ---") {
		t.Error("prompt should contain compressed git section")
	}
	if !strings.Contains(prompt, "--- notes.md ---") {
		t.Error("prompt should contain notes section")
	}
	if !strings.Contains(prompt, "Compressed git summary") {
		t.Error("prompt should contain file contents")
	}

	// Check updated descriptions
	if !strings.Contains(prompt, "comp-git-myproject.md: AI-compressed summary of time-stamped snapshots") {
		t.Error("prompt should contain compressed git description")
	}
	if !strings.Contains(prompt, "Below is the data collected") {
		t.Error("prompt should use updated preamble")
	}
}

func TestAssemblePromptGitOnly(t *testing.T) {
	files := map[string]string{
		"comp-git-myproject.md": "Compressed git summary\n",
	}

	prompt := assemblePrompt("myproject", "2024-01-15", files)

	if !strings.Contains(prompt, "--- comp-git-myproject.md ---") {
		t.Error("prompt should contain compressed git section")
	}
	if strings.Contains(prompt, "--- notes.md ---") {
		t.Error("prompt should NOT contain notes section when notes don't exist")
	}
}

func TestAssemblePromptNotesOnly(t *testing.T) {
	files := map[string]string{
		"notes.md": "### At 10:20 #myproject\nsome notes\n",
	}

	prompt := assemblePrompt("myproject", "2024-01-15", files)

	if strings.Contains(prompt, "--- git-myproject.log ---") {
		t.Error("prompt should NOT contain git log section when git log doesn't exist")
	}
	if !strings.Contains(prompt, "--- notes.md ---") {
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
	os.WriteFile(filepath.Join(dateDir, "notes.md"),
		[]byte("### At 10:20 #myproject\nStarted work\n"), 0o644)

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
	// Without comp files, falls back to raw git data
	if !strings.Contains(s, "diff content") {
		t.Error("output should contain git log data (raw fallback)")
	}
	if !strings.Contains(s, "Started work") {
		t.Error("output should contain notes data")
	}
}

func TestRunGenPromptWithCompFiles(t *testing.T) {
	tmp := t.TempDir()
	rawDir := filepath.Join(tmp, "raw")
	t.Setenv("DEVLOG_RAW_DIR", rawDir)
	t.Setenv("DEVLOG_LOG_DIR", filepath.Join(tmp, "log"))

	date := "2024-01-15"
	dateDir := filepath.Join(rawDir, date)
	os.MkdirAll(dateDir, 0o755)

	// Create both raw and comp files
	os.WriteFile(filepath.Join(dateDir, "git-myproject.log"),
		[]byte("=== SNAPSHOT 10:00 ===\nraw diff content\n"), 0o644)
	os.WriteFile(filepath.Join(dateDir, "comp-git-myproject.md"),
		[]byte("Compressed git summary"), 0o644)
	os.WriteFile(filepath.Join(dateDir, "notes.md"),
		[]byte("### At 10:20 #myproject\nStarted work\n"), 0o644)

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

	// Should use comp file, not raw
	if !strings.Contains(s, "Compressed git summary") {
		t.Error("output should contain compressed git data")
	}
	if strings.Contains(s, "raw diff content") {
		t.Error("output should NOT contain raw git data when comp file exists")
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

	// Create mock summarizer and compressor scripts
	mockBin := filepath.Join(tmp, "bin")
	os.MkdirAll(mockBin, 0o755)
	mockSummarizer := filepath.Join(mockBin, "mysummarizer")
	os.WriteFile(mockSummarizer, []byte("#!/bin/sh\necho 'This is a test summary.'\n"), 0o755)
	mockCompressor := filepath.Join(mockBin, "mycompressor")
	os.WriteFile(mockCompressor, []byte("#!/bin/sh\necho 'Compressed data.'\n"), 0o755)
	t.Setenv("PATH", mockBin+":"+os.Getenv("PATH"))

	date := "2024-01-15"
	dateDir := filepath.Join(rawDir, date)
	os.MkdirAll(dateDir, 0o755)
	os.WriteFile(filepath.Join(dateDir, "git-myproject.log"),
		[]byte("=== SNAPSHOT 10:00 ===\ndiff content\n\n"), 0o644)

	cfg := Config{
		GenCmd:  "mysummarizer",
		CompCmd: "mycompressor",
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
		"comp-git-myproject.md":  "Compressed git summary\n",
		"comp-term-myproject.md": "Compressed term summary with go test\n",
	}

	prompt := assemblePrompt("myproject", "2024-01-15", files)

	if !strings.Contains(prompt, "--- comp-term-myproject.md ---") {
		t.Error("prompt should contain compressed terminal section")
	}
	if !strings.Contains(prompt, "go test") {
		t.Error("prompt should contain compressed terminal contents")
	}
	if !strings.Contains(prompt, "comp-term-myproject.md: AI-compressed summary of terminal session") {
		t.Error("prompt should describe compressed terminal data source")
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
		"comp-git-myproject.md":    "Compressed git summary\n",
		"comp-claude-myproject.md": "Compressed Claude summary about fixing tests\n",
	}

	prompt := assemblePrompt("myproject", "2024-06-15", files)

	if !strings.Contains(prompt, "--- comp-claude-myproject.md ---") {
		t.Error("prompt should contain compressed Claude Code section")
	}
	if !strings.Contains(prompt, "fixing tests") {
		t.Error("prompt should contain compressed Claude Code content")
	}
	if !strings.Contains(prompt, "comp-claude-myproject.md: AI-compressed summary of Claude Code session") {
		t.Error("prompt should contain compressed Claude Code data source description")
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

func TestFilterNotesForProject(t *testing.T) {
	content := "### At 09:00 #alpha\nalpha note 1\n\n" +
		"### At 10:00 #beta\nbeta note\n\n" +
		"### At 11:00 #alpha\nalpha note 2\n\n" +
		"### At 12:00\nunaffiliated note\n\n"

	got := filterNotesForProject(content, "alpha")
	if !strings.Contains(got, "alpha note 1") {
		t.Error("should contain first alpha note")
	}
	if !strings.Contains(got, "alpha note 2") {
		t.Error("should contain second alpha note")
	}
	if strings.Contains(got, "beta note") {
		t.Error("should not contain beta note")
	}
	if strings.Contains(got, "unaffiliated note") {
		t.Error("should not contain unaffiliated note")
	}
}

func TestFilterUnaffiliatedNotes(t *testing.T) {
	content := "### At 09:00 #alpha\nalpha note\n\n" +
		"### At 10:00\ngeneral note 1\n\n" +
		"### At 11:00 #beta\nbeta note\n\n" +
		"### At 12:00\ngeneral note 2\n\n"

	got := filterUnaffiliatedNotes(content)
	if !strings.Contains(got, "general note 1") {
		t.Error("should contain first unaffiliated note")
	}
	if !strings.Contains(got, "general note 2") {
		t.Error("should contain second unaffiliated note")
	}
	if strings.Contains(got, "alpha note") {
		t.Error("should not contain alpha note")
	}
	if strings.Contains(got, "beta note") {
		t.Error("should not contain beta note")
	}
}

func TestRunGenPromptGeneral(t *testing.T) {
	tmp := t.TempDir()
	rawDir := filepath.Join(tmp, "raw")
	t.Setenv("DEVLOG_RAW_DIR", rawDir)
	t.Setenv("DEVLOG_LOG_DIR", filepath.Join(tmp, "log"))

	date := "2024-01-15"
	dateDir := filepath.Join(rawDir, date)
	os.MkdirAll(dateDir, 0o755)
	os.WriteFile(filepath.Join(dateDir, "notes.md"),
		[]byte("### At 10:00\nA general note\n\n"), 0o644)

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

	if !strings.Contains(s, `"general"`) {
		t.Error("output should contain general pseudo-project")
	}
	if !strings.Contains(s, "A general note") {
		t.Error("output should contain the unaffiliated note")
	}
}

func TestAssembleCompPrompt(t *testing.T) {
	files := map[string]string{
		"git-myproject.log": "=== SNAPSHOT 10:15 ===\ndiff content\n",
	}

	for _, tc := range []struct {
		dataType string
		wantDesc string
	}{
		{"git", "Time-stamped snapshots of uncommitted code changes"},
		{"term", "Terminal session recordings captured with tools like"},
		{"claude", "Preprocessed transcripts of Claude Code sessions"},
	} {
		t.Run(tc.dataType, func(t *testing.T) {
			prompt := assembleCompPrompt(tc.dataType, files)

			if !strings.Contains(prompt, tc.wantDesc) {
				t.Errorf("prompt should contain %q description", tc.dataType)
			}
			if !strings.Contains(prompt, "--- git-myproject.log ---") {
				t.Error("prompt should contain file section")
			}
			if !strings.Contains(prompt, "high fidelity\ncompression") {
				t.Error("prompt should contain compression task")
			}
			if !strings.Contains(prompt, "Correlate summarized events by timestamp") {
				t.Error("prompt should contain timestamp guideline")
			}
		})
	}
}

func TestCompressData(t *testing.T) {
	tmp := t.TempDir()
	rawDir := filepath.Join(tmp, "raw")
	t.Setenv("DEVLOG_RAW_DIR", rawDir)

	date := "2024-01-15"
	dateDir := filepath.Join(rawDir, date)
	os.MkdirAll(dateDir, 0o755)

	// Create mock compressor
	mockBin := filepath.Join(tmp, "bin")
	os.MkdirAll(mockBin, 0o755)
	mockComp := filepath.Join(mockBin, "mockcomp")
	os.WriteFile(mockComp, []byte("#!/bin/sh\necho 'Compressed output.'\n"), 0o755)
	t.Setenv("PATH", mockBin+":"+os.Getenv("PATH"))

	// Create source file
	srcPath := filepath.Join(dateDir, "git-proj.log")
	os.WriteFile(srcPath, []byte("diff data"), 0o644)

	cfg := Config{CompCmd: "mockcomp"}
	files := map[string]string{"git-proj.log": "diff data"}

	result, err := compressData(cfg, "git", "proj", date, files, []string{srcPath})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "Compressed output." {
		t.Errorf("expected %q, got %q", "Compressed output.", result)
	}

	// Verify comp file was written
	compPath := filepath.Join(dateDir, "comp-git-proj.md")
	data, err := os.ReadFile(compPath)
	if err != nil {
		t.Fatalf("comp file should exist: %v", err)
	}
	if string(data) != "Compressed output." {
		t.Errorf("comp file content: got %q, want %q", string(data), "Compressed output.")
	}
}

func TestCompressDataCaching(t *testing.T) {
	tmp := t.TempDir()
	rawDir := filepath.Join(tmp, "raw")
	t.Setenv("DEVLOG_RAW_DIR", rawDir)

	date := "2024-01-15"
	dateDir := filepath.Join(rawDir, date)
	os.MkdirAll(dateDir, 0o755)

	// Create source file with old timestamp
	srcPath := filepath.Join(dateDir, "git-proj.log")
	os.WriteFile(srcPath, []byte("diff data"), 0o644)
	past := time.Now().Add(-1 * time.Hour)
	os.Chtimes(srcPath, past, past)

	// Create comp file with newer timestamp
	compPath := filepath.Join(dateDir, "comp-git-proj.md")
	os.WriteFile(compPath, []byte("Cached compressed data"), 0o644)

	// Use a nonexistent command — if caching works, it won't be invoked
	cfg := Config{CompCmd: "nonexistent-command-that-should-not-run"}
	files := map[string]string{"git-proj.log": "diff data"}

	result, err := compressData(cfg, "git", "proj", date, files, []string{srcPath})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "Cached compressed data" {
		t.Errorf("expected cached data, got %q", result)
	}
}

func TestCompressDataNoFiles(t *testing.T) {
	cfg := Config{CompCmd: "anything"}
	result, err := compressData(cfg, "git", "proj", "2024-01-15", map[string]string{}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "" {
		t.Errorf("expected empty string, got %q", result)
	}
}

