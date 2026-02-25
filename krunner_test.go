package main

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestParseKRunnerQuery(t *testing.T) {
	tests := []struct {
		input       string
		wantProject string
		wantContent string
	}{
		{"#devlog", "devlog", ""},
		{"#devlog some note", "devlog", "some note"},
		{"#dev", "dev", ""},
		{"#dev multi word content", "dev", "multi word content"},
		{"#", "", ""},
		{"#project   spaced  ", "project", "spaced"},
		{"not a hashtag", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			project, content := parseKRunnerQuery(tt.input)
			if project != tt.wantProject {
				t.Errorf("project = %q, want %q", project, tt.wantProject)
			}
			if content != tt.wantContent {
				t.Errorf("content = %q, want %q", content, tt.wantContent)
			}
		})
	}
}

func TestMatchIDRoundTrip(t *testing.T) {
	tests := []struct {
		project string
		content string
	}{
		{"devlog", "some content"},
		{"devlog", ""},
		{"devlog", "content:with:colons"},
		{"project", "multi word content"},
	}

	for _, tt := range tests {
		t.Run(tt.project+"/"+tt.content, func(t *testing.T) {
			id := encodeMatchID(tt.project, tt.content)
			gotProject, gotContent := decodeMatchID(id)
			if gotProject != tt.project {
				t.Errorf("project = %q, want %q", gotProject, tt.project)
			}
			if gotContent != tt.content {
				t.Errorf("content = %q, want %q", gotContent, tt.content)
			}
		})
	}
}

func TestKRunnerMatch(t *testing.T) {
	s := &Server{
		watched: []WatchEntry{
			{Path: "/home/user/dev/devlog", Name: "devlog"},
			{Path: "/home/user/dev/devtools", Name: "devtools"},
			{Path: "/home/user/work/api", Name: "api"},
		},
	}
	kr := &KRunner{server: s}

	t.Run("non-hashtag query returns nothing", func(t *testing.T) {
		matches, err := kr.Match("devlog")
		if err != nil {
			t.Fatal(err)
		}
		if len(matches) != 0 {
			t.Errorf("got %d matches, want 0", len(matches))
		}
	})

	t.Run("prefix match", func(t *testing.T) {
		matches, err := kr.Match("#dev")
		if err != nil {
			t.Fatal(err)
		}
		if len(matches) != 2 {
			t.Fatalf("got %d matches, want 2", len(matches))
		}
		// Both should be PossibleMatch (prefix only, neither is exact for "dev")
		for _, m := range matches {
			if m.CategoryRelevance != 10 {
				t.Errorf("match %q: CategoryRelevance = %d, want 10", m.ID, m.CategoryRelevance)
			}
		}
	})

	t.Run("exact match", func(t *testing.T) {
		matches, err := kr.Match("#devlog")
		if err != nil {
			t.Fatal(err)
		}
		if len(matches) != 1 {
			t.Fatalf("got %d matches, want 1", len(matches))
		}
		if matches[0].CategoryRelevance != 100 {
			t.Errorf("CategoryRelevance = %d, want 100", matches[0].CategoryRelevance)
		}
	})

	t.Run("exact match with content encodes in ID", func(t *testing.T) {
		matches, err := kr.Match("#devlog my note text")
		if err != nil {
			t.Fatal(err)
		}
		if len(matches) != 1 {
			t.Fatalf("got %d matches, want 1", len(matches))
		}
		project, content := decodeMatchID(matches[0].ID)
		if project != "devlog" {
			t.Errorf("project = %q, want devlog", project)
		}
		if content != "my note text" {
			t.Errorf("content = %q, want 'my note text'", content)
		}
	})

	t.Run("no match", func(t *testing.T) {
		matches, err := kr.Match("#xyz")
		if err != nil {
			t.Fatal(err)
		}
		if len(matches) != 0 {
			t.Errorf("got %d matches, want 0", len(matches))
		}
	})
}

func TestKRunnerRunWithContent(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("DEVLOG_RAW_DIR", tmpDir)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	s := &Server{
		watched: []WatchEntry{
			{Path: "/home/user/dev/devlog", Name: "devlog"},
		},
	}
	kr := &KRunner{server: s}

	matchID := encodeMatchID("devlog", "test note from krunner")
	dbusErr := kr.Run(matchID, "")
	if dbusErr != nil {
		t.Fatalf("Run returned error: %v", dbusErr)
	}

	// Find the notes file that was written
	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		t.Fatalf("reading tmpDir: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("no date directory created")
	}

	dateDir := filepath.Join(tmpDir, entries[0].Name())
	files, err := os.ReadDir(dateDir)
	if err != nil {
		t.Fatalf("reading date dir: %v", err)
	}

	found := false
	for _, f := range files {
		if strings.HasPrefix(f.Name(), "notes-devlog") {
			data, err := os.ReadFile(filepath.Join(dateDir, f.Name()))
			if err != nil {
				t.Fatalf("reading notes file: %v", err)
			}
			if !strings.Contains(string(data), "test note from krunner") {
				t.Errorf("notes file doesn't contain expected content: %s", data)
			}
			found = true
		}
	}
	if !found {
		t.Error("no notes file found for devlog project")
	}
}

func TestKRunnerAvailableNoKdialog(t *testing.T) {
	// Set PATH to an empty directory so kdialog won't be found
	emptyDir := t.TempDir()
	t.Setenv("PATH", emptyDir)

	s := &Server{
		mu:      sync.RWMutex{},
		watched: []WatchEntry{},
	}

	cleanup := startKRunner(s)
	if cleanup != nil {
		cleanup()
		t.Error("startKRunner should return nil when kdialog is not available")
	}
}
