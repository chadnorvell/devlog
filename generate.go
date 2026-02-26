package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

var filterHeadingRe = regexp.MustCompile(`^### At \d{2}:\d{2}(\s+#(\S+))?`)

func filterNotesForProject(content, project string) string {
	lines := strings.Split(content, "\n")
	var result []string
	var inMatch bool
	tag := "#" + project

	for _, line := range lines {
		if strings.HasPrefix(line, "### At ") {
			inMatch = filterHeadingRe.MatchString(line) && strings.Contains(line, tag)
		}
		if inMatch {
			result = append(result, line)
		}
	}
	return strings.TrimRight(strings.Join(result, "\n"), "\n")
}

func filterUnaffiliatedNotes(content string) string {
	lines := strings.Split(content, "\n")
	var result []string
	var inMatch bool

	for _, line := range lines {
		if strings.HasPrefix(line, "### At ") {
			m := filterHeadingRe.FindStringSubmatch(line)
			inMatch = m != nil && m[2] == ""
		}
		if inMatch {
			result = append(result, line)
		}
	}
	return strings.TrimRight(strings.Join(result, "\n"), "\n")
}

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

- notes.md: Manually logged notes and snippets with timestamps. These can be
  developer notes expressing intent, observations, and decisions. They can
  also be snippets captured from code, docs, the web, or terminal sessions.

- git-` + project + `.log: Time-stamped snapshots of uncommitted code changes,
  taken every 5 minutes. These show the evolution of the code over the day,
  including approaches that were tried and abandoned.

- term-` + project + `*.log: Terminal session recordings captured with tools like
  ` + "`script`" + `. These show the developer's terminal activity: commands run, test
  output, debugging sessions, REPL interactions, etc. May contain ANSI escape
  codes which should be ignored.

- claude-code-sessions.txt: Preprocessed transcripts of Claude Code sessions
  for the day, showing the developer's interactions with an AI coding
  assistant. Contains user prompts, assistant responses, and tool use
  summaries. This reveals what the developer was trying to accomplish, what
  approaches were discussed, and what changes were made through the AI
  assistant.

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

func generateProjectSummary(cfg Config, state State, project, date string) (string, error) {
	files := make(map[string]string)

	// Check for git log
	gitPath := resolveGitPath(cfg, date, project)
	if data, err := os.ReadFile(gitPath); err == nil {
		files[filepath.Base(gitPath)] = string(data)
	}

	// Check for notes
	notesPath := resolveNotesPath(cfg, date)
	if data, err := os.ReadFile(notesPath); err == nil {
		var filtered string
		if project == "general" {
			filtered = filterUnaffiliatedNotes(string(data))
		} else {
			filtered = filterNotesForProject(string(data), project)
		}
		if filtered != "" {
			files["notes.md"] = filtered
		}
	}

	// Check for terminal logs
	termPattern := resolveTermGlob(cfg, date, project)
	if matches, err := filepath.Glob(termPattern); err == nil {
		for _, m := range matches {
			if data, err := os.ReadFile(m); err == nil {
				files[filepath.Base(m)] = string(data)
			}
		}
	}

	// Check for Claude Code sessions
	claudeDir := resolveClaudeCodeDir(cfg)
	if claudeDir != "" {
		for _, w := range state.Watched {
			if w.Name == project {
				projDir := filepath.Join(claudeDir, repoPathToClaudeDir(w.Path))
				if transcript, err := preprocessClaudeCodeSessions(projDir, date, time.Now().Location()); err == nil && transcript != "" {
					files["claude-code-sessions.txt"] = transcript
				}
				break
			}
		}
	}

	if len(files) == 0 {
		return "", nil
	}

	prompt := assemblePrompt(project, date, files)

	args := strings.Fields(cfg.GenCmd)
	if len(args) == 0 {
		return "", fmt.Errorf("gen_cmd is empty")
	}

	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stdin = strings.NewReader(prompt)
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("%s failed: %s", args[0], string(exitErr.Stderr))
		}
		return "", fmt.Errorf("running %s: %w", args[0], err)
	}

	return strings.TrimSpace(string(out)), nil
}

func discoverAllProjects(cfg Config, state State, date string) []string {
	projects := discoverProjects(cfg, date)
	seen := make(map[string]bool)
	for _, p := range projects {
		seen[p] = true
	}

	claudeDir := resolveClaudeCodeDir(cfg)
	if claudeDir != "" {
		loc := time.Now().Location()
		for _, w := range state.Watched {
			if seen[w.Name] {
				continue
			}
			projDir := filepath.Join(claudeDir, repoPathToClaudeDir(w.Path))
			if info, err := os.Stat(projDir); err == nil && info.IsDir() {
				if hasEntriesOnDate(projDir, date, loc) {
					projects = append(projects, w.Name)
					seen[w.Name] = true
				}
			}
		}
		sort.Strings(projects)
	}

	return projects
}

func runGen(cfg Config, state State, date string) error {
	logDir := resolveLogDir(cfg)

	// Discover projects from raw data and Claude Code sessions
	projects := discoverAllProjects(cfg, state, date)
	if len(projects) == 0 {
		fmt.Fprintf(os.Stderr, "No raw data for %s\n", date)
		return nil
	}

	// Staleness check
	summaryPath := filepath.Join(logDir, date+".md")
	if summaryInfo, err := os.Stat(summaryPath); err == nil {
		summaryMtime := summaryInfo.ModTime()
		maxRawMtime := collectRawFileMtime(cfg, state, date)
		if !maxRawMtime.IsZero() && summaryMtime.After(maxRawMtime) {
			fmt.Println("Summary is up to date, no new data since last generation")
			return nil
		}
		// Remove stale summary before regenerating
		os.Remove(summaryPath)
	}

	// Check summarizer is available
	args := strings.Fields(cfg.GenCmd)
	if len(args) == 0 {
		return fmt.Errorf("gen_cmd is empty")
	}
	if _, err := exec.LookPath(args[0]); err != nil {
		return fmt.Errorf("summarizer command %q not found on $PATH", args[0])
	}

	// Generate summary for each project
	type projectSummary struct {
		name    string
		summary string
	}
	var summaries []projectSummary

	for _, proj := range projects {
		summary, err := generateProjectSummary(cfg, state, proj, date)
		if err != nil {
			return fmt.Errorf("generating summary for %s: %w", proj, err)
		}
		if summary != "" {
			summaries = append(summaries, projectSummary{name: proj, summary: summary})
		}
	}

	// Check for unaffiliated notes → "general" pseudo-project
	notesPath := resolveNotesPath(cfg, date)
	if data, err := os.ReadFile(notesPath); err == nil {
		if unaffiliated := filterUnaffiliatedNotes(string(data)); unaffiliated != "" {
			summary, err := generateProjectSummary(cfg, state, "general", date)
			if err != nil {
				return fmt.Errorf("generating summary for general: %w", err)
			}
			if summary != "" {
				summaries = append(summaries, projectSummary{name: "general", summary: summary})
			}
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

func runGenPrompt(cfg Config, state State, date string) error {
	projects := discoverAllProjects(cfg, state, date)

	// Check for unaffiliated notes → "general" pseudo-project
	notesPath := resolveNotesPath(cfg, date)
	hasGeneral := false
	var notesData []byte
	if data, err := os.ReadFile(notesPath); err == nil {
		notesData = data
		if filterUnaffiliatedNotes(string(data)) != "" {
			hasGeneral = true
		}
	}

	if len(projects) == 0 && !hasGeneral {
		fmt.Fprintf(os.Stderr, "No raw data for %s\n", date)
		return nil
	}

	allProjects := make([]string, len(projects))
	copy(allProjects, projects)
	if hasGeneral {
		allProjects = append(allProjects, "general")
	}

	multi := len(allProjects) > 1

	for i, proj := range allProjects {
		files := make(map[string]string)

		if proj != "general" {
			gitPath := resolveGitPath(cfg, date, proj)
			if data, err := os.ReadFile(gitPath); err == nil {
				files[filepath.Base(gitPath)] = string(data)
			}
		}

		if notesData != nil {
			var filtered string
			if proj == "general" {
				filtered = filterUnaffiliatedNotes(string(notesData))
			} else {
				filtered = filterNotesForProject(string(notesData), proj)
			}
			if filtered != "" {
				files["notes.md"] = filtered
			}
		}

		if proj != "general" {
			termPattern := resolveTermGlob(cfg, date, proj)
			if matches, err := filepath.Glob(termPattern); err == nil {
				for _, m := range matches {
					if data, err := os.ReadFile(m); err == nil {
						files[filepath.Base(m)] = string(data)
					}
				}
			}

			// Check for Claude Code sessions
			claudeDir := resolveClaudeCodeDir(cfg)
			if claudeDir != "" {
				for _, w := range state.Watched {
					if w.Name == proj {
						projDir := filepath.Join(claudeDir, repoPathToClaudeDir(w.Path))
						if transcript, err := preprocessClaudeCodeSessions(projDir, date, time.Now().Location()); err == nil && transcript != "" {
							files["claude-code-sessions.txt"] = transcript
						}
						break
					}
				}
			}
		}

		if len(files) == 0 {
			continue
		}

		if multi {
			if i > 0 {
				fmt.Println()
			}
			fmt.Printf("=== %s ===\n", proj)
		}

		fmt.Print(assemblePrompt(proj, date, files))
	}

	return nil
}

func collectRawFileMtime(cfg Config, state State, date string) time.Time {
	rawDir := resolveRawDir(cfg)
	var maxMtime time.Time

	gitTmpl := cfg.GitPath
	if gitTmpl == "" {
		gitTmpl = "<raw_dir>/<date>/git-<project>.log"
	}
	for _, path := range globForTemplate(gitTmpl, rawDir, date) {
		if info, err := os.Stat(path); err == nil {
			if info.ModTime().After(maxMtime) {
				maxMtime = info.ModTime()
			}
		}
	}

	notesPath := resolveNotesPath(cfg, date)
	if info, err := os.Stat(notesPath); err == nil {
		if info.ModTime().After(maxMtime) {
			maxMtime = info.ModTime()
		}
	}

	termTmpl := cfg.TermPath
	if termTmpl == "" {
		termTmpl = "<raw_dir>/<date>/term-<project>*.log"
	}
	for _, path := range globForTemplate(termTmpl, rawDir, date) {
		if info, err := os.Stat(path); err == nil {
			if info.ModTime().After(maxMtime) {
				maxMtime = info.ModTime()
			}
		}
	}

	// Check Claude Code JSONL mtimes
	claudeDir := resolveClaudeCodeDir(cfg)
	if claudeDir != "" {
		for _, w := range state.Watched {
			projDir := filepath.Join(claudeDir, repoPathToClaudeDir(w.Path))
			matches, _ := filepath.Glob(filepath.Join(projDir, "*.jsonl"))
			for _, m := range matches {
				if info, err := os.Stat(m); err == nil {
					if info.ModTime().After(maxMtime) {
						maxMtime = info.ModTime()
					}
				}
			}
		}
	}

	return maxMtime
}

