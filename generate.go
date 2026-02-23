package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func assemblePrompt(project, date string, files map[string]string) string {
	var b strings.Builder

	fmt.Fprintf(&b, "You are summarizing a day of software engineering work on the project\n"+
		"%q for the date %s.\n\n"+
		"Below is the raw data collected during the day.\n", project, date)

	// Sort filenames for deterministic output
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		fmt.Fprintf(&b, "\n--- %s ---\n%s\n", name, files[name])
	}

	b.WriteString(`
Description of data sources:

- git-` + project + `.log: Time-stamped snapshots of uncommitted code changes,
  taken every 5 minutes. These show the evolution of the code over the day,
  including approaches that were tried and abandoned.

- notes-` + project + `.log: Manually logged notes with timestamps, expressing
  intent, observations, and decisions.

Not all sources may be present. Work with whatever is available.

Task: Write a concise summary of the day's work on this project. The summary
should allow someone to read it and immediately resume working on the project,
even after a long absence.

Guidelines:
- Describe what was being worked on and why.
- Explain the approaches tried, including dead ends and pivots. Explain what
  went wrong and what eventually worked.
- Summarize key code changes by functional impact, not just file names.
- Identify unfinished work, open questions, and likely next steps.
- Do NOT include timestamps in the summary.
- Do NOT use headings. Write flowing prose, with bullet points where
  appropriate for lists of items.
- Write in first person.

Output only the summary text, nothing else.
`)

	return b.String()
}

func generateProjectSummary(project, date, rawDir string) (string, error) {
	dateDir := filepath.Join(rawDir, date)

	files := make(map[string]string)

	// Check for git log
	gitFile := "git-" + project + ".log"
	if data, err := os.ReadFile(filepath.Join(dateDir, gitFile)); err == nil {
		files[gitFile] = string(data)
	}

	// Check for notes log
	notesFile := "notes-" + project + ".log"
	if data, err := os.ReadFile(filepath.Join(dateDir, notesFile)); err == nil {
		files[notesFile] = string(data)
	}

	if len(files) == 0 {
		return "", nil
	}

	prompt := assemblePrompt(project, date, files)

	cmd := exec.Command("claude", "-p")
	cmd.Stdin = strings.NewReader(prompt)
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("claude failed: %s", string(exitErr.Stderr))
		}
		return "", fmt.Errorf("running claude: %w", err)
	}

	return strings.TrimSpace(string(out)), nil
}

func runGen(cfg Config, date string) error {
	rawDir := resolveRawDir(cfg)
	logDir := resolveLogDir(cfg)
	dateDir := filepath.Join(rawDir, date)

	// Check raw data exists
	entries, err := os.ReadDir(dateDir)
	if err != nil || len(entries) == 0 {
		fmt.Fprintf(os.Stderr, "No raw data for %s\n", date)
		return nil
	}

	// Staleness check
	summaryPath := filepath.Join(logDir, date+".md")
	if summaryInfo, err := os.Stat(summaryPath); err == nil {
		summaryMtime := summaryInfo.ModTime()
		var maxRawMtime time.Time
		for _, e := range entries {
			info, err := e.Info()
			if err != nil {
				continue
			}
			if info.ModTime().After(maxRawMtime) {
				maxRawMtime = info.ModTime()
			}
		}
		if summaryMtime.After(maxRawMtime) {
			fmt.Println("Summary is up to date, no new data since last generation")
			return nil
		}
		// Remove stale summary before regenerating
		os.Remove(summaryPath)
	}

	// Check claude is available
	if _, err := exec.LookPath("claude"); err != nil {
		return fmt.Errorf("claude (Claude Code CLI) is required for summary generation but was not found on $PATH")
	}

	// Extract project names from files
	projects := extractProjects(entries)
	if len(projects) == 0 {
		fmt.Fprintf(os.Stderr, "No raw data for %s\n", date)
		return nil
	}

	// Generate summary for each project
	type projectSummary struct {
		name    string
		summary string
	}
	var summaries []projectSummary

	for _, proj := range projects {
		summary, err := generateProjectSummary(proj, date, rawDir)
		if err != nil {
			return fmt.Errorf("generating summary for %s: %w", proj, err)
		}
		if summary != "" {
			summaries = append(summaries, projectSummary{name: proj, summary: summary})
		}
	}

	if len(summaries) == 0 {
		fmt.Fprintf(os.Stderr, "No raw data for %s\n", date)
		return nil
	}

	// Assemble output
	var out strings.Builder
	fmt.Fprintf(&out, "# %s\n", date)
	for _, s := range summaries {
		fmt.Fprintf(&out, "\n## %s\n\n%s\n", s.name, s.summary)
	}

	// Write output atomically
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return fmt.Errorf("creating log dir: %w", err)
	}
	if err := os.WriteFile(summaryPath, []byte(out.String()), 0o644); err != nil {
		return fmt.Errorf("writing summary: %w", err)
	}

	fmt.Printf("Summary written to %s\n", summaryPath)
	return nil
}

func extractProjects(entries []os.DirEntry) []string {
	seen := make(map[string]bool)
	for _, e := range entries {
		name := e.Name()
		var project string
		if strings.HasPrefix(name, "git-") && strings.HasSuffix(name, ".log") {
			project = strings.TrimSuffix(strings.TrimPrefix(name, "git-"), ".log")
		} else if strings.HasPrefix(name, "notes-") && strings.HasSuffix(name, ".log") {
			project = strings.TrimSuffix(strings.TrimPrefix(name, "notes-"), ".log")
		}
		if project != "" {
			seen[project] = true
		}
	}

	projects := make([]string, 0, len(seen))
	for p := range seen {
		projects = append(projects, p)
	}
	sort.Strings(projects)
	return projects
}
