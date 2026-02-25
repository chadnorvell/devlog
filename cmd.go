package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func cmdNote() {
	fs := flag.NewFlagSet("note", flag.ExitOnError)
	msg := fs.String("m", "", "note message")
	proj := fs.String("p", "", "project name")
	fs.Parse(os.Args[1:])

	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	var projectName string
	if *proj != "" {
		projectName = *proj
	} else {
		cwd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		repoRoot, err := resolveRepoRoot(cwd)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error: not in a git repository")
			os.Exit(1)
		}

		state, _ := loadState()
		projectName = projectNameForRepo(repoRoot, state, "")
	}

	today := time.Now().Format("2006-01-02")
	notesFile := resolveNotesPath(cfg, today, projectName)

	var noteText string
	if *msg != "" {
		noteText = *msg
	} else {
		noteText, err = editNote(cfg, projectName)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		if noteText == "" {
			fmt.Println("Note cancelled (empty message)")
			return
		}
	}

	if err := writeNote(notesFile, noteText); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Logged note for %s.\n", projectName)
}

func editNote(cfg Config, projectName string) (string, error) {
	editor := resolveEditor(cfg)

	tmp, err := os.CreateTemp("", "devlog-note-*.md")
	if err != nil {
		return "", fmt.Errorf("creating temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	template := fmt.Sprintf("# Project: %s\n# Enter your note below. Lines starting with # are ignored.\n", projectName)
	tmp.WriteString(template)
	tmp.Close()

	cmd := exec.Command(editor, tmpPath)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("editor: %w", err)
	}

	data, err := os.ReadFile(tmpPath)
	if err != nil {
		return "", fmt.Errorf("reading note: %w", err)
	}

	// Strip comment lines and trim
	var lines []string
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "#") {
			lines = append(lines, line)
		}
	}
	return strings.TrimSpace(strings.Join(lines, "\n")), nil
}

func writeNote(notesFile, text string) error {
	if err := os.MkdirAll(filepath.Dir(notesFile), 0o755); err != nil {
		return fmt.Errorf("creating raw dir: %w", err)
	}

	f, err := os.OpenFile(notesFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("opening notes file: %w", err)
	}
	defer f.Close()

	now := time.Now()
	header := fmt.Sprintf("### At %02d:%02d\n", now.Hour(), now.Minute())
	if _, err := f.WriteString(header + text + "\n\n"); err != nil {
		return fmt.Errorf("writing note: %w", err)
	}
	return nil
}

func cmdGen() {
	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	date := time.Now().Format("2006-01-02")
	if len(os.Args) >= 3 && os.Args[1] == "gen" {
		date = os.Args[2]
		if !isValidDate(date) {
			fmt.Fprintln(os.Stderr, "Error: invalid date format, expected YYYY-MM-DD")
			os.Exit(1)
		}
	}

	if err := runGen(cfg, date); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func isValidDate(s string) bool {
	_, err := time.Parse("2006-01-02", s)
	return err == nil
}

func cmdWatch() {
	fs := flag.NewFlagSet("watch", flag.ExitOnError)
	name := fs.String("name", "", "override project name")
	fs.Parse(os.Args[2:])

	var repoPath string
	if fs.NArg() > 0 {
		repoPath = fs.Arg(0)
	} else {
		cwd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		repoPath = cwd
	}

	repoRoot, err := resolveRepoRoot(repoPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error: not in a git repository")
		os.Exit(1)
	}

	args, _ := json.Marshal(WatchArgs{Path: repoRoot, Name: *name})
	resp, err := ipcSend(IPCRequest{Command: "watch", Args: json.RawMessage(args)})
	if err != nil {
		if isServerNotRunning(err) {
			watchOffline(repoRoot, *name)
			return
		}
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if !resp.OK {
		fmt.Fprintf(os.Stderr, "Error: %s\n", resp.Error)
		os.Exit(1)
	}

	printWatchedList(resp.Data)
}

func watchOffline(repoRoot, nameOverride string) {
	state, err := loadState()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	projectName := nameOverride
	if projectName == "" {
		projectName = filepath.Base(repoRoot)
	}

	// Check if already watched
	for _, w := range state.Watched {
		if w.Path == repoRoot {
			fmt.Printf("Already watching %s (%s)\n", w.Name, w.Path)
			printWatchedState(state)
			fmt.Println("(server is not running; snapshot collection will begin when it starts)")
			return
		}
	}

	// Check for name collision
	for _, w := range state.Watched {
		if w.Name == projectName {
			fmt.Fprintf(os.Stderr, "Error: name conflict: %q is already used by %s\n", projectName, w.Path)
			os.Exit(1)
		}
	}

	state.Watched = append(state.Watched, WatchEntry{Path: repoRoot, Name: projectName})
	if err := saveState(state); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	printWatchedState(state)
	fmt.Println("(server is not running; snapshot collection will begin when it starts)")
}

func cmdUnwatch() {
	fs := flag.NewFlagSet("unwatch", flag.ExitOnError)
	fs.Parse(os.Args[2:])

	var repoPath string
	if fs.NArg() > 0 {
		repoPath = fs.Arg(0)
	} else {
		cwd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		repoPath = cwd
	}

	repoRoot, err := resolveRepoRoot(repoPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error: not in a git repository")
		os.Exit(1)
	}

	args, _ := json.Marshal(UnwatchArgs{Path: repoRoot})
	resp, err := ipcSend(IPCRequest{Command: "unwatch", Args: json.RawMessage(args)})
	if err != nil {
		if isServerNotRunning(err) {
			unwatchOffline(repoRoot)
			return
		}
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if !resp.OK {
		fmt.Fprintf(os.Stderr, "Error: %s\n", resp.Error)
		os.Exit(1)
	}

	printWatchedList(resp.Data)
}

func unwatchOffline(repoRoot string) {
	state, err := loadState()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	found := false
	var newWatched []WatchEntry
	for _, w := range state.Watched {
		if w.Path == repoRoot {
			found = true
			continue
		}
		newWatched = append(newWatched, w)
	}

	if !found {
		fmt.Printf("Not watching %s\n", repoRoot)
		printWatchedState(state)
		return
	}

	state.Watched = newWatched
	if err := saveState(state); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	printWatchedState(state)
}

func cmdStart() {
	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	s := newServer(cfg)
	if err := s.run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func cmdStop() {
	resp, err := ipcSend(IPCRequest{Command: "stop"})
	if err != nil {
		if isServerNotRunning(err) {
			fmt.Println("devlog server is not running")
			return
		}
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if !resp.OK {
		fmt.Fprintf(os.Stderr, "Error: %s\n", resp.Error)
		os.Exit(1)
	}

	// Wait for server to exit (check PID file removal)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(pidFilePath()); os.IsNotExist(err) {
			fmt.Println("devlog server stopped.")
			return
		}
		time.Sleep(100 * time.Millisecond)
	}

	fmt.Println("devlog server stopped.")
}

func cmdStatus() {
	resp, err := ipcSend(IPCRequest{Command: "status"})
	if err != nil {
		if isServerNotRunning(err) {
			fmt.Println("devlog server is not running")
			return
		}
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if !resp.OK {
		fmt.Fprintf(os.Stderr, "Error: %s\n", resp.Error)
		os.Exit(1)
	}

	var status StatusData
	if err := json.Unmarshal(resp.Data, &status); err != nil {
		fmt.Fprintf(os.Stderr, "Error: parsing status: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("devlog server running (PID %d)\n", status.PID)
	if len(status.Watched) == 0 {
		fmt.Println("No repos being watched")
	} else {
		fmt.Println("Watched repos:")
		for _, w := range status.Watched {
			fmt.Printf("  %s (%s)\n", w.Name, w.Path)
		}
	}
}

func printWatchedList(data json.RawMessage) {
	var wd WatchResponseData
	if err := json.Unmarshal(data, &wd); err != nil {
		return
	}

	if len(wd.Watched) == 0 {
		fmt.Println("No repos being watched")
	} else {
		fmt.Println("Watched repos:")
		for _, w := range wd.Watched {
			fmt.Printf("  %s (%s)\n", w.Name, w.Path)
		}
	}
}

func printWatchedState(state State) {
	if len(state.Watched) == 0 {
		fmt.Println("No repos being watched")
	} else {
		fmt.Println("Watched repos:")
		for _, w := range state.Watched {
			fmt.Printf("  %s (%s)\n", w.Name, w.Path)
		}
	}
}
