package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStateRoundTrip(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_STATE_HOME", tmp)

	original := State{
		Watched: []WatchEntry{
			{Path: "/home/user/dev/foo", Name: "foo"},
			{Path: "/home/user/dev/bar", Name: "my-bar"},
		},
	}

	if err := saveState(original); err != nil {
		t.Fatalf("saveState: %v", err)
	}

	loaded, err := loadState()
	if err != nil {
		t.Fatalf("loadState: %v", err)
	}

	if len(loaded.Watched) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(loaded.Watched))
	}
	if loaded.Watched[0].Path != "/home/user/dev/foo" || loaded.Watched[0].Name != "foo" {
		t.Errorf("entry 0 mismatch: %+v", loaded.Watched[0])
	}
	if loaded.Watched[1].Path != "/home/user/dev/bar" || loaded.Watched[1].Name != "my-bar" {
		t.Errorf("entry 1 mismatch: %+v", loaded.Watched[1])
	}
}

func TestStateMissingFile(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_STATE_HOME", tmp)

	s, err := loadState()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(s.Watched) != 0 {
		t.Errorf("expected empty watched list, got %d", len(s.Watched))
	}
}

func TestStateAtomicWrite(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_STATE_HOME", tmp)

	s := State{Watched: []WatchEntry{{Path: "/a", Name: "a"}}}
	if err := saveState(s); err != nil {
		t.Fatalf("saveState: %v", err)
	}

	// Verify the file exists at expected path
	path := filepath.Join(tmp, "devlog", "state.json")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("state file not found: %v", err)
	}

	// Verify no temp files left behind
	entries, _ := os.ReadDir(filepath.Dir(path))
	for _, e := range entries {
		if e.Name() != "state.json" {
			t.Errorf("unexpected file left behind: %s", e.Name())
		}
	}
}

func TestProjectNameForRepo(t *testing.T) {
	state := State{
		Watched: []WatchEntry{
			{Path: "/home/user/dev/foo", Name: "custom-name"},
		},
	}

	// Repo in state uses state name
	got := projectNameForRepo("/home/user/dev/foo", state, "")
	if got != "custom-name" {
		t.Errorf("expected custom-name, got %q", got)
	}

	// Override takes precedence
	got = projectNameForRepo("/home/user/dev/foo", state, "override")
	if got != "override" {
		t.Errorf("expected override, got %q", got)
	}

	// Unknown repo falls back to basename
	got = projectNameForRepo("/home/user/dev/bar", state, "")
	if got != "bar" {
		t.Errorf("expected bar, got %q", got)
	}
}
