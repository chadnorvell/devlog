package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type WatchEntry struct {
	Path string `json:"path"`
	Name string `json:"name"`
}

type State struct {
	Watched []WatchEntry `json:"watched"`
}

func loadState() (State, error) {
	path := resolveStatePath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return State{}, nil
		}
		return State{}, fmt.Errorf("reading state: %w", err)
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return State{}, fmt.Errorf("parsing state: %w", err)
	}
	return s, nil
}

func saveState(s State) error {
	path := resolveStatePath()
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating state dir: %w", err)
	}

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling state: %w", err)
	}
	data = append(data, '\n')

	// Atomic write: write to temp file in same dir, then rename.
	tmp, err := os.CreateTemp(dir, "state-*.json.tmp")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	tmpName := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("writing temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("closing temp file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("renaming temp file: %w", err)
	}
	return nil
}

func projectNameForRepo(repoPath string, state State, nameOverride string) string {
	if nameOverride != "" {
		return nameOverride
	}
	// Check state for an existing name mapping.
	for _, w := range state.Watched {
		if w.Path == repoPath {
			return w.Name
		}
	}
	// Fall back to basename of repo path.
	return filepath.Base(repoPath)
}
