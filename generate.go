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
		"Below is the data collected during the day.\n", project, date)

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

- comp-git-` + project + `.md: AI-compressed summary of time-stamped snapshots of
  uncommitted code changes, taken every 5 minutes. Describes the evolution of
  the code over the day, including approaches that were tried and abandoned.

- comp-term-` + project + `.md: AI-compressed summary of terminal session
  recordings. Describes the developer's terminal activity: commands run, test
  output, debugging sessions, REPL interactions, etc.

- comp-claude-` + project + `.md: AI-compressed summary of Claude Code session
  transcripts for the day. Describes the developer's interactions with an AI
  coding assistant, what the developer was trying to accomplish, what
  approaches were discussed, and what changes were made.

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

func assembleCompPrompt(dataType string, files map[string]string) string {
	var b strings.Builder

	b.WriteString("You are summarizing data automatically logged during a software engineering\nsession.\n\nDescription of the data:\n\n")

	switch dataType {
	case "git":
		b.WriteString("- Time-stamped snapshots of uncommitted code changes, taken every 5 minutes.\n" +
			"  These show the evolution of the code over the day, including approaches that\n" +
			"  were tried and abandoned.\n")
	case "term":
		b.WriteString("- Terminal session recordings captured with tools like `script`. These show the\n" +
			"  developer's terminal activity: commands run, test output, debugging sessions,\n" +
			"  REPL interactions, etc. May contain ANSI escape codes which should be\n" +
			"  ignored.\n")
	case "claude":
		b.WriteString("- Preprocessed transcripts of Claude Code sessions for the day, showing the\n" +
			"  developer's interactions with an AI coding assistant. Contains user prompts,\n" +
			"  assistant responses, and tool use summaries. This reveals what the developer\n" +
			"  was trying to accomplish, what approaches were discussed, and what changes\n" +
			"  were made through the AI assistant.\n")
	}

	b.WriteString("\nBelow is the raw data collected during the day.\n")

	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		fmt.Fprintf(&b, "\n--- %s ---\n%s\n", name, files[name])
	}

	b.WriteString(`
Task: Write a concise summary of the work done in the logs, such that someone
could read the summary and have a complete understanding without reading the
raw data at all. In other words, the summary should be a high fidelity
compression of the raw data.

Guidelines:
- Describe what was being worked on and why.
- Explain the approaches tried, including dead ends and pivots. Explain what
  went wrong and what eventually worked.
- Correlate summarized events by timestamp or timestamp range.

Output only the summary text, nothing else.
`)

	return b.String()
}

func compressData(cfg Config, dataType, project, date string, files map[string]string, sourcePaths []string) (string, error) {
	if len(files) == 0 {
		return "", nil
	}

	rawDir := resolveRawDir(cfg)
	outPath := filepath.Join(rawDir, date, "comp-"+dataType+"-"+project+".md")

	// Staleness check: if output exists and is newer than all sources, use cache
	if outInfo, err := os.Stat(outPath); err == nil {
		outMtime := outInfo.ModTime()
		fresh := true
		for _, sp := range sourcePaths {
			if info, err := os.Stat(sp); err == nil {
				if info.ModTime().After(outMtime) {
					fresh = false
					break
				}
			}
		}
		if fresh {
			data, err := os.ReadFile(outPath)
			if err != nil {
				return "", err
			}
			return strings.TrimSpace(string(data)), nil
		}
	}

	prompt := assembleCompPrompt(dataType, files)

	args := strings.Fields(cfg.CompCmd)
	if len(args) == 0 {
		return "", fmt.Errorf("comp_cmd is empty")
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

	result := strings.TrimSpace(string(out))

	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return "", fmt.Errorf("creating comp dir: %w", err)
	}
	if err := os.WriteFile(outPath, []byte(result), 0o644); err != nil {
		return "", fmt.Errorf("writing comp file: %w", err)
	}

	return result, nil
}

func generateProjectSummary(cfg Config, state State, project, date string) (string, error) {
	files := make(map[string]string)

	// Collect and compress git data
	gitPath := resolveGitPath(cfg, date, project)
	if data, err := os.ReadFile(gitPath); err == nil {
		gitFiles := map[string]string{filepath.Base(gitPath): string(data)}
		compressed, err := compressData(cfg, "git", project, date, gitFiles, []string{gitPath})
		if err != nil {
			return "", fmt.Errorf("compressing git data: %w", err)
		}
		if compressed != "" {
			files["comp-git-"+project+".md"] = compressed
		}
	}

	// Check for notes (no compression)
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

	// Collect and compress terminal logs
	termPattern := resolveTermGlob(cfg, date, project)
	if matches, err := filepath.Glob(termPattern); err == nil && len(matches) > 0 {
		termFiles := make(map[string]string)
		var termSourcePaths []string
		for _, m := range matches {
			if data, err := os.ReadFile(m); err == nil {
				termFiles[filepath.Base(m)] = string(data)
				termSourcePaths = append(termSourcePaths, m)
			}
		}
		compressed, err := compressData(cfg, "term", project, date, termFiles, termSourcePaths)
		if err != nil {
			return "", fmt.Errorf("compressing term data: %w", err)
		}
		if compressed != "" {
			files["comp-term-"+project+".md"] = compressed
		}
	}

	// Collect and compress Claude Code sessions
	claudeDir := resolveClaudeCodeDir(cfg)
	if claudeDir != "" {
		for _, w := range state.Watched {
			if w.Name == project {
				projDir := filepath.Join(claudeDir, repoPathToClaudeDir(w.Path))
				if transcript, err := preprocessClaudeCodeSessions(projDir, date, time.Now().Location()); err == nil && transcript != "" {
					// Find JSONL source files for staleness check
					jsonlMatches, _ := filepath.Glob(filepath.Join(projDir, "*.jsonl"))
					claudeFiles := map[string]string{"claude-code-sessions.txt": transcript}
					compressed, err := compressData(cfg, "claude", project, date, claudeFiles, jsonlMatches)
					if err != nil {
						return "", fmt.Errorf("compressing claude data: %w", err)
					}
					if compressed != "" {
						files["comp-claude-"+project+".md"] = compressed
					}
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

	// Check compressor is available
	compArgs := strings.Fields(cfg.CompCmd)
	if len(compArgs) == 0 {
		return fmt.Errorf("comp_cmd is empty")
	}
	if _, err := exec.LookPath(compArgs[0]); err != nil {
		return fmt.Errorf("compressor command %q not found on $PATH", compArgs[0])
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

	rawDir := resolveRawDir(cfg)

	for i, proj := range allProjects {
		files := make(map[string]string)

		if proj != "general" {
			// Prefer compressed git data; fall back to raw
			compGitPath := filepath.Join(rawDir, date, "comp-git-"+proj+".md")
			if data, err := os.ReadFile(compGitPath); err == nil {
				files["comp-git-"+proj+".md"] = string(data)
			} else {
				gitPath := resolveGitPath(cfg, date, proj)
				if data, err := os.ReadFile(gitPath); err == nil {
					files[filepath.Base(gitPath)] = string(data)
				}
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
			// Prefer compressed term data; fall back to raw
			compTermPath := filepath.Join(rawDir, date, "comp-term-"+proj+".md")
			if data, err := os.ReadFile(compTermPath); err == nil {
				files["comp-term-"+proj+".md"] = string(data)
			} else {
				termPattern := resolveTermGlob(cfg, date, proj)
				if matches, err := filepath.Glob(termPattern); err == nil {
					for _, m := range matches {
						if data, err := os.ReadFile(m); err == nil {
							files[filepath.Base(m)] = string(data)
						}
					}
				}
			}

			// Prefer compressed Claude data; fall back to raw
			compClaudePath := filepath.Join(rawDir, date, "comp-claude-"+proj+".md")
			if data, err := os.ReadFile(compClaudePath); err == nil {
				files["comp-claude-"+proj+".md"] = string(data)
			} else {
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

