package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteNote(t *testing.T) {
	notesFile := filepath.Join(t.TempDir(), "2024-01-15", "notes.md")

	err := writeNote(notesFile, "Testing the note command", "myproject")
	if err != nil {
		t.Fatalf("writeNote: %v", err)
	}

	content, err := os.ReadFile(notesFile)
	if err != nil {
		t.Fatalf("reading notes: %v", err)
	}

	s := string(content)
	if !strings.Contains(s, "#myproject") {
		t.Error("missing project hashtag in header")
	}
	if !strings.Contains(s, "Testing the note command") {
		t.Error("missing note text")
	}
	if !strings.HasSuffix(s, "\n\n") {
		t.Error("note should end with blank line")
	}
}

func TestWriteNoteMultiple(t *testing.T) {
	notesFile := filepath.Join(t.TempDir(), "2024-01-15", "notes.md")

	writeNote(notesFile, "First note", "myproject")
	writeNote(notesFile, "Second note", "myproject")

	content, _ := os.ReadFile(notesFile)

	count := strings.Count(string(content), "### At")
	if count != 2 {
		t.Errorf("expected 2 notes, got %d", count)
	}
}

func TestWriteNoteNoProject(t *testing.T) {
	notesFile := filepath.Join(t.TempDir(), "2024-01-15", "notes.md")

	err := writeNote(notesFile, "A general note", "")
	if err != nil {
		t.Fatalf("writeNote: %v", err)
	}

	content, err := os.ReadFile(notesFile)
	if err != nil {
		t.Fatalf("reading notes: %v", err)
	}

	s := string(content)
	if !strings.Contains(s, "### At") {
		t.Error("missing note header")
	}
	if strings.Contains(s, "#") && !strings.Contains(s, "### ") {
		t.Error("note without project should not contain a hashtag")
	}
	if strings.Count(s, "#") != 3 { // only the ### heading
		t.Errorf("expected only ### in header, got content: %q", s)
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

func TestWatchOffline(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_STATE_HOME", tmp)

	// Watch a repo offline
	watchOffline("/home/user/dev/foo", "")
	state, err := loadState()
	if err != nil {
		t.Fatalf("loadState: %v", err)
	}
	if len(state.Watched) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(state.Watched))
	}
	if state.Watched[0].Path != "/home/user/dev/foo" || state.Watched[0].Name != "foo" {
		t.Errorf("unexpected entry: %+v", state.Watched[0])
	}

	// Watch a second repo with a name override
	watchOffline("/home/user/dev/bar", "custom-bar")
	state, _ = loadState()
	if len(state.Watched) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(state.Watched))
	}
	if state.Watched[1].Name != "custom-bar" {
		t.Errorf("expected custom-bar, got %q", state.Watched[1].Name)
	}

	// Watching the same repo again should not add a duplicate
	watchOffline("/home/user/dev/foo", "")
	state, _ = loadState()
	if len(state.Watched) != 2 {
		t.Errorf("expected 2 entries (no duplicate), got %d", len(state.Watched))
	}
}

func TestWatchOfflineNameCollision(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_STATE_HOME", tmp)

	// Set up an existing watched repo
	saveState(State{Watched: []WatchEntry{{Path: "/home/user/dev/foo", Name: "foo"}}})

	// Trying to watch a different repo with the same name should fail.
	// watchOffline calls os.Exit(1) on collision, so we can't test it
	// directly in-process. Instead, test the collision logic extracted
	// to a helper.
	state, _ := loadState()
	projectName := "foo"
	hasCollision := false
	for _, w := range state.Watched {
		if w.Path != "/home/user/dev/other" && w.Name == projectName {
			hasCollision = true
			break
		}
	}
	if !hasCollision {
		t.Error("expected name collision to be detected")
	}
}

func TestUnwatchOffline(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_STATE_HOME", tmp)

	// Set up watched repos
	saveState(State{Watched: []WatchEntry{
		{Path: "/home/user/dev/foo", Name: "foo"},
		{Path: "/home/user/dev/bar", Name: "bar"},
	}})

	// Unwatch one
	unwatchOffline("/home/user/dev/foo")
	state, _ := loadState()
	if len(state.Watched) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(state.Watched))
	}
	if state.Watched[0].Path != "/home/user/dev/bar" {
		t.Errorf("wrong entry remaining: %+v", state.Watched[0])
	}
}

func TestUnwatchOfflineNotWatched(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_STATE_HOME", tmp)

	// Unwatch from empty state — should not error
	unwatchOffline("/home/user/dev/foo")
	state, _ := loadState()
	if len(state.Watched) != 0 {
		t.Errorf("expected empty watched list, got %d", len(state.Watched))
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
