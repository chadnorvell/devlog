package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteNote(t *testing.T) {
	dateDir := filepath.Join(t.TempDir(), "2024-01-15")

	err := writeNote(dateDir, "myproject", "Testing the note command")
	if err != nil {
		t.Fatalf("writeNote: %v", err)
	}

	logFile := filepath.Join(dateDir, "notes-myproject.log")
	content, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("reading notes: %v", err)
	}

	s := string(content)
	if !strings.Contains(s, "=== NOTE") {
		t.Error("missing NOTE header")
	}
	if !strings.Contains(s, "Testing the note command") {
		t.Error("missing note text")
	}
	if !strings.HasSuffix(s, "\n\n") {
		t.Error("note should end with blank line")
	}
}

func TestWriteNoteMultiple(t *testing.T) {
	dateDir := filepath.Join(t.TempDir(), "2024-01-15")

	writeNote(dateDir, "myproject", "First note")
	writeNote(dateDir, "myproject", "Second note")

	logFile := filepath.Join(dateDir, "notes-myproject.log")
	content, _ := os.ReadFile(logFile)

	count := strings.Count(string(content), "=== NOTE")
	if count != 2 {
		t.Errorf("expected 2 notes, got %d", count)
	}
}

func TestProjectNameFromState(t *testing.T) {
	state := State{
		Watched: []WatchEntry{
			{Path: "/home/user/dev/foo", Name: "custom-foo"},
		},
	}

	// When repo is in state, use state name
	name := projectNameForRepo("/home/user/dev/foo", state, "")
	if name != "custom-foo" {
		t.Errorf("expected custom-foo, got %q", name)
	}

	// When repo is not in state, use basename
	name = projectNameForRepo("/home/user/dev/bar", state, "")
	if name != "bar" {
		t.Errorf("expected bar, got %q", name)
	}
}

func TestIsValidDate(t *testing.T) {
	tests := []struct {
		input string
		valid bool
	}{
		{"2024-01-15", true},
		{"2024-12-31", true},
		{"2024-1-15", false},
		{"01-15-2024", false},
		{"not-a-date", false},
		{"", false},
	}

	for _, tt := range tests {
		got := isValidDate(tt.input)
		if got != tt.valid {
			t.Errorf("isValidDate(%q) = %v, want %v", tt.input, got, tt.valid)
		}
	}
}
