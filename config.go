package main

import (
	"bufio"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"

	"github.com/BurntSushi/toml"
)

type Config struct {
	LogDir           string `toml:"log_dir"`
	RawDir           string `toml:"raw_dir"`
	SnapshotInterval int    `toml:"snapshot_interval"`
	Editor           string `toml:"editor"`
	GenCmd           string `toml:"gen_cmd"`
	GitPath          string `toml:"git_path"`
	NotesPath        string `toml:"notes_path"`
	TermPath         string `toml:"term_path"`
	ClaudeCodeDir    *string `toml:"claude_code_dir"`
}

func configFilePath() string {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "devlog", "config.toml")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "devlog", "config.toml")
}

func loadConfig() (Config, error) {
	cfg := Config{
		SnapshotInterval: 300,
		GenCmd:           "claude -p",
	}

	path := configFilePath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, fmt.Errorf("reading config: %w", err)
	}

	if err := toml.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("parsing config: %w", err)
	}

	if cfg.SnapshotInterval <= 0 {
		cfg.SnapshotInterval = 300
	}

	return cfg, nil
}

func xdgDataHome() string {
	if dir := os.Getenv("XDG_DATA_HOME"); dir != "" {
		return dir
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share")
}

func resolveLogDir(cfg Config) string {
	if dir := os.Getenv("DEVLOG_LOG_DIR"); dir != "" {
		return dir
	}
	if cfg.LogDir != "" {
		return cfg.LogDir
	}
	return filepath.Join(xdgDataHome(), "devlog", "log")
}

func resolveRawDir(cfg Config) string {
	if dir := os.Getenv("DEVLOG_RAW_DIR"); dir != "" {
		return dir
	}
	if cfg.RawDir != "" {
		return cfg.RawDir
	}
	return filepath.Join(xdgDataHome(), "devlog", "raw")
}

func resolveStatePath() string {
	if dir := os.Getenv("XDG_STATE_HOME"); dir != "" {
		return filepath.Join(dir, "devlog", "state.json")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "state", "devlog", "state.json")
}

func socketPath() string {
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir != "" {
		return filepath.Join(dir, "devlog.sock")
	}
	u, _ := user.Current()
	uid := "1000"
	if u != nil {
		uid = u.Uid
	}
	return "/tmp/devlog-" + uid + ".sock"
}

func pidFilePath() string {
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir != "" {
		return filepath.Join(dir, "devlog.pid")
	}
	u, _ := user.Current()
	uid := "1000"
	if u != nil {
		uid = u.Uid
	}
	return "/tmp/devlog-" + uid + ".pid"
}

func resolveEditor(cfg Config) string {
	if editor := os.Getenv("EDITOR"); editor != "" {
		return editor
	}
	if cfg.Editor != "" {
		return cfg.Editor
	}
	return "vi"
}

func readPidFile() (int, error) {
	data, err := os.ReadFile(pidFilePath())
	if err != nil {
		return 0, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, fmt.Errorf("invalid PID file: %w", err)
	}
	return pid, nil
}

func isProcessRunning(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// On Unix, FindProcess always succeeds. Send signal 0 to check.
	err = proc.Signal(syscall.Signal(0))
	return err == nil
}

func resolvePathTemplate(tmpl, rawDir, date, project string) string {
	r := strings.NewReplacer("<raw_dir>", rawDir, "<date>", date, "<project>", project)
	return r.Replace(tmpl)
}

func resolveGitPath(cfg Config, date, project string) string {
	tmpl := cfg.GitPath
	if tmpl == "" {
		tmpl = "<raw_dir>/<date>/git-<project>.log"
	}
	return resolvePathTemplate(tmpl, resolveRawDir(cfg), date, project)
}

func resolveNotesPath(cfg Config, date string) string {
	tmpl := cfg.NotesPath
	if tmpl == "" {
		tmpl = "<raw_dir>/<date>/notes.md"
	}
	return resolvePathTemplate(tmpl, resolveRawDir(cfg), date, "")
}

func resolveTermGlob(cfg Config, date, project string) string {
	tmpl := cfg.TermPath
	if tmpl == "" {
		tmpl = "<raw_dir>/<date>/term-<project>*.log"
	}
	return resolvePathTemplate(tmpl, resolveRawDir(cfg), date, project)
}

func discoverProjects(cfg Config, date string) []string {
	seen := make(map[string]bool)
	rawDir := resolveRawDir(cfg)

	gitTmpl := cfg.GitPath
	if gitTmpl == "" {
		gitTmpl = "<raw_dir>/<date>/git-<project>.log"
	}
	for _, path := range globForTemplate(gitTmpl, rawDir, date) {
		if p := extractProjectFromPath(path, gitTmpl, rawDir, date); p != "" {
			seen[p] = true
		}
	}

	for _, p := range discoverProjectsFromNotes(cfg, date) {
		seen[p] = true
	}

	projects := make([]string, 0, len(seen))
	for p := range seen {
		projects = append(projects, p)
	}
	sort.Strings(projects)
	return projects
}

var notesHeadingRe = regexp.MustCompile(`^### At \d{2}:\d{2}\s+#(\S+)`)

func discoverProjectsFromNotes(cfg Config, date string) []string {
	path := resolveNotesPath(cfg, date)
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	seen := make(map[string]bool)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		if m := notesHeadingRe.FindStringSubmatch(scanner.Text()); m != nil {
			seen[m[1]] = true
		}
	}

	projects := make([]string, 0, len(seen))
	for p := range seen {
		projects = append(projects, p)
	}
	sort.Strings(projects)
	return projects
}

func globForTemplate(tmpl, rawDir, date string) []string {
	pattern := resolvePathTemplate(tmpl, rawDir, date, "*")
	matches, _ := filepath.Glob(pattern)
	return matches
}

func extractProjectFromPath(path, tmpl, rawDir, date string) string {
	resolved := resolvePathTemplate(tmpl, rawDir, date, "<project>")
	parts := strings.SplitN(resolved, "<project>", 2)
	if len(parts) != 2 {
		return ""
	}
	prefix, suffix := parts[0], parts[1]
	if !strings.HasPrefix(path, prefix) || !strings.HasSuffix(path, suffix) {
		return ""
	}
	return path[len(prefix) : len(path)-len(suffix)]
}

func resolveClaudeCodeDir(cfg Config) string {
	if cfg.ClaudeCodeDir != nil {
		dir := *cfg.ClaudeCodeDir
		if dir == "" {
			return ""
		}
		if strings.HasPrefix(dir, "~/") {
			home, _ := os.UserHomeDir()
			return filepath.Join(home, dir[2:])
		}
		return dir
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude", "projects")
}

func repoPathToClaudeDir(repoPath string) string {
	return strings.ReplaceAll(repoPath, "/", "-")
}
