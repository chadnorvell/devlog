package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRepoPathToClaudeDir(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"/home/chad/dev/ctrl", "-home-chad-dev-ctrl"},
		{"/home/user/work/api", "-home-user-work-api"},
		{"/tmp/test", "-tmp-test"},
	}
	for _, tt := range tests {
		got := repoPathToClaudeDir(tt.input)
		if got != tt.want {
			t.Errorf("repoPathToClaudeDir(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestPreprocessClaudeCodeSessions(t *testing.T) {
	tmp := t.TempDir()
	loc := time.UTC

	date := "2024-06-15"
	ts1 := "2024-06-15T10:00:00.000Z"
	ts2 := "2024-06-15T10:01:00.000Z"
	ts3 := "2024-06-15T10:02:00.000Z"

	// Write a session file
	lines := []string{
		jsonLine(t, map[string]interface{}{
			"type": "user", "timestamp": ts1, "sessionId": "s1",
			"message": map[string]interface{}{
				"role": "user", "content": "Help me fix the bug",
			},
		}),
		jsonLine(t, map[string]interface{}{
			"type": "assistant", "timestamp": ts2, "sessionId": "s1",
			"message": map[string]interface{}{
				"role": "assistant", "content": []map[string]interface{}{
					{"type": "thinking", "thinking": "Let me think about this..."},
					{"type": "text", "text": "I'll look at the code."},
					{"type": "tool_use", "name": "Read", "input": map[string]string{"file_path": "main.go"}},
				},
			},
		}),
		jsonLine(t, map[string]interface{}{
			"type": "user", "timestamp": ts3, "sessionId": "s1",
			"message": map[string]interface{}{
				"role": "user", "content": []map[string]interface{}{
					{"type": "tool_result", "content": "file contents here"},
				},
			},
		}),
	}

	os.WriteFile(filepath.Join(tmp, "session1.jsonl"), []byte(strings.Join(lines, "\n")+"\n"), 0o644)

	result, err := preprocessClaudeCodeSessions(tmp, date, loc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(result, "=== SESSION started 10:00 ===") {
		t.Error("should contain session header with start time")
	}
	if !strings.Contains(result, "> Help me fix the bug") {
		t.Error("should contain user text")
	}
	if !strings.Contains(result, "I'll look at the code.") {
		t.Error("should contain assistant text")
	}
	if !strings.Contains(result, `[Tool: Read file_path="main.go"]`) {
		t.Error("should contain tool use summary")
	}
	if strings.Contains(result, "Let me think about this") {
		t.Error("should NOT contain thinking block content")
	}
	// Tool result user entries (content is array) should be skipped
	if strings.Contains(result, "file contents here") {
		t.Error("should NOT contain tool result content")
	}
}

func TestPreprocessClaudeCodeSessionsMultiple(t *testing.T) {
	tmp := t.TempDir()
	loc := time.UTC

	date := "2024-06-15"

	// Session 2 starts later
	session2 := []string{
		jsonLine(t, map[string]interface{}{
			"type": "user", "timestamp": "2024-06-15T14:00:00.000Z", "sessionId": "s2",
			"message": map[string]interface{}{
				"role": "user", "content": "afternoon session",
			},
		}),
	}

	// Session 1 starts earlier
	session1 := []string{
		jsonLine(t, map[string]interface{}{
			"type": "user", "timestamp": "2024-06-15T09:00:00.000Z", "sessionId": "s1",
			"message": map[string]interface{}{
				"role": "user", "content": "morning session",
			},
		}),
	}

	os.WriteFile(filepath.Join(tmp, "sess2.jsonl"), []byte(strings.Join(session2, "\n")+"\n"), 0o644)
	os.WriteFile(filepath.Join(tmp, "sess1.jsonl"), []byte(strings.Join(session1, "\n")+"\n"), 0o644)

	result, err := preprocessClaudeCodeSessions(tmp, date, loc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Morning session should come first
	morningIdx := strings.Index(result, "morning session")
	afternoonIdx := strings.Index(result, "afternoon session")
	if morningIdx < 0 || afternoonIdx < 0 {
		t.Fatal("should contain both sessions")
	}
	if morningIdx > afternoonIdx {
		t.Error("sessions should be sorted by start time (morning before afternoon)")
	}
}

func TestPreprocessClaudeCodeSessionsNoMatch(t *testing.T) {
	tmp := t.TempDir()
	loc := time.UTC

	lines := []string{
		jsonLine(t, map[string]interface{}{
			"type": "user", "timestamp": "2024-06-14T10:00:00.000Z", "sessionId": "s1",
			"message": map[string]interface{}{
				"role": "user", "content": "wrong date",
			},
		}),
	}

	os.WriteFile(filepath.Join(tmp, "session.jsonl"), []byte(strings.Join(lines, "\n")+"\n"), 0o644)

	result, err := preprocessClaudeCodeSessions(tmp, "2024-06-15", loc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "" {
		t.Errorf("expected empty result for wrong date, got %q", result)
	}
}

func TestPreprocessClaudeCodeSessionsSkipsSubagents(t *testing.T) {
	tmp := t.TempDir()
	loc := time.UTC

	date := "2024-06-15"

	// Create a subagents subdirectory with a JSONL file
	subDir := filepath.Join(tmp, "abc-uuid", "subagents")
	os.MkdirAll(subDir, 0o755)
	lines := []string{
		jsonLine(t, map[string]interface{}{
			"type": "user", "timestamp": "2024-06-15T10:00:00.000Z", "sessionId": "sub1",
			"message": map[string]interface{}{
				"role": "user", "content": "subagent content",
			},
		}),
	}
	os.WriteFile(filepath.Join(subDir, "sub.jsonl"), []byte(strings.Join(lines, "\n")+"\n"), 0o644)

	result, err := preprocessClaudeCodeSessions(tmp, date, loc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(result, "subagent content") {
		t.Error("should NOT include content from subdirectories")
	}
	if result != "" {
		t.Errorf("expected empty result (no top-level JSONL), got %q", result)
	}
}

func TestSummarizeToolInput(t *testing.T) {
	tests := []struct {
		name  string
		input map[string]interface{}
		want  string
	}{
		{
			name:  "Read",
			input: map[string]interface{}{"file_path": "main.go"},
			want:  `[Tool: Read file_path="main.go"]`,
		},
		{
			name:  "Edit",
			input: map[string]interface{}{"file_path": "config.go", "old_string": "x", "new_string": "y"},
			want:  `[Tool: Edit file_path="config.go"]`,
		},
		{
			name:  "Write",
			input: map[string]interface{}{"file_path": "new.go", "content": "package main"},
			want:  `[Tool: Write file_path="new.go"]`,
		},
		{
			name:  "Bash",
			input: map[string]interface{}{"command": "go test ./..."},
			want:  `[Tool: Bash command="go test ./..."]`,
		},
		{
			name:  "Grep",
			input: map[string]interface{}{"pattern": "func main"},
			want:  `[Tool: Grep pattern="func main"]`,
		},
		{
			name:  "Glob",
			input: map[string]interface{}{"pattern": "**/*.go"},
			want:  `[Tool: Glob pattern="**/*.go"]`,
		},
		{
			name:  "WebSearch",
			input: map[string]interface{}{"query": "golang json parsing"},
			want:  `[Tool: WebSearch query="golang json parsing"]`,
		},
		{
			name:  "WebFetch",
			input: map[string]interface{}{"url": "https://example.com"},
			want:  `[Tool: WebFetch url="https://example.com"]`,
		},
		{
			name:  "Task",
			input: map[string]interface{}{"prompt": "find the bug"},
			want:  `[Tool: Task prompt="find the bug"]`,
		},
		{
			name:  "UnknownTool",
			input: map[string]interface{}{"foo": "bar"},
			want:  `[Tool: UnknownTool]`,
		},
	}

	for _, tt := range tests {
		inputJSON, _ := json.Marshal(tt.input)
		got := summarizeToolInput(tt.name, json.RawMessage(inputJSON))
		if got != tt.want {
			t.Errorf("summarizeToolInput(%q, ...) = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestHasEntriesOnDate(t *testing.T) {
	tmp := t.TempDir()
	loc := time.UTC

	lines := []string{
		jsonLine(t, map[string]interface{}{
			"type": "user", "timestamp": "2024-06-15T10:00:00.000Z",
			"message": map[string]interface{}{"role": "user", "content": "hello"},
		}),
	}
	os.WriteFile(filepath.Join(tmp, "session.jsonl"), []byte(strings.Join(lines, "\n")+"\n"), 0o644)

	if !hasEntriesOnDate(tmp, "2024-06-15", loc) {
		t.Error("should find entries on matching date")
	}
	if hasEntriesOnDate(tmp, "2024-06-16", loc) {
		t.Error("should NOT find entries on different date")
	}
}

func TestParseSessionDateFilteringUTC(t *testing.T) {
	tmp := t.TempDir()

	// Entry at 2024-06-15 23:30 UTC → in UTC+2 this is 2024-06-16 01:30
	lines := []string{
		jsonLine(t, map[string]interface{}{
			"type": "user", "timestamp": "2024-06-15T23:30:00.000Z", "sessionId": "s1",
			"message": map[string]interface{}{
				"role": "user", "content": "late night entry",
			},
		}),
	}
	path := filepath.Join(tmp, "session.jsonl")
	os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644)

	// In UTC, this is June 15
	transcript, _, err := parseSessionForDate(path, "2024-06-15", time.UTC)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(transcript, "late night entry") {
		t.Error("entry should match June 15 in UTC")
	}

	// In UTC+2, this is June 16
	loc := time.FixedZone("UTC+2", 2*60*60)
	transcript, _, err = parseSessionForDate(path, "2024-06-16", loc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(transcript, "late night entry") {
		t.Error("entry should match June 16 in UTC+2")
	}

	// In UTC+2, should NOT match June 15
	transcript, _, err = parseSessionForDate(path, "2024-06-15", loc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if transcript != "" {
		t.Error("entry should NOT match June 15 in UTC+2")
	}
}

func jsonLine(t *testing.T, v interface{}) string {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
