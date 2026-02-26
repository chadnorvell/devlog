package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfigMissing(t *testing.T) {
	// Point XDG_CONFIG_HOME at an empty dir so no config file is found.
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.SnapshotInterval != 300 {
		t.Errorf("expected default interval 300, got %d", cfg.SnapshotInterval)
	}
	if cfg.LogDir != "" {
		t.Errorf("expected empty LogDir, got %q", cfg.LogDir)
	}
}

func TestLoadConfigPartial(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	dir := filepath.Join(tmp, "devlog")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "config.toml"), []byte(`
log_dir = "/my/logs"
snapshot_interval = 60
`), 0o644)

	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.LogDir != "/my/logs" {
		t.Errorf("expected /my/logs, got %q", cfg.LogDir)
	}
	if cfg.SnapshotInterval != 60 {
		t.Errorf("expected 60, got %d", cfg.SnapshotInterval)
	}
	if cfg.RawDir != "" {
		t.Errorf("expected empty RawDir, got %q", cfg.RawDir)
	}
}

func TestResolveLogDirPrecedence(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)

	cfg := Config{}

	// Default: XDG_DATA_HOME based
	got := resolveLogDir(cfg)
	want := filepath.Join(tmp, "devlog", "log")
	if got != want {
		t.Errorf("default: got %q, want %q", got, want)
	}

	// Config overrides default
	cfg.LogDir = "/config/logs"
	got = resolveLogDir(cfg)
	if got != "/config/logs" {
		t.Errorf("config: got %q, want /config/logs", got)
	}

	// Env var overrides config
	t.Setenv("DEVLOG_LOG_DIR", "/env/logs")
	got = resolveLogDir(cfg)
	if got != "/env/logs" {
		t.Errorf("env: got %q, want /env/logs", got)
	}
}

func TestResolveRawDirPrecedence(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)

	cfg := Config{}

	// Default
	got := resolveRawDir(cfg)
	want := filepath.Join(tmp, "devlog", "raw")
	if got != want {
		t.Errorf("default: got %q, want %q", got, want)
	}

	// Config overrides
	cfg.RawDir = "/config/raw"
	got = resolveRawDir(cfg)
	if got != "/config/raw" {
		t.Errorf("config: got %q, want /config/raw", got)
	}

	// Env overrides
	t.Setenv("DEVLOG_RAW_DIR", "/env/raw")
	got = resolveRawDir(cfg)
	if got != "/env/raw" {
		t.Errorf("env: got %q, want /env/raw", got)
	}
}

func TestResolveStatePath(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_STATE_HOME", tmp)

	got := resolveStatePath()
	want := filepath.Join(tmp, "devlog", "state.json")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSocketPath(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", tmp)

	got := socketPath()
	want := filepath.Join(tmp, "devlog.sock")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSocketPathFallback(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", "")

	got := socketPath()
	// Should be /tmp/devlog-<uid>.sock
	if got == "" {
		t.Error("expected non-empty socket path")
	}
	if filepath.Dir(got) != "/tmp" {
		t.Errorf("expected /tmp dir, got %q", filepath.Dir(got))
	}
}

func TestPidFilePath(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", tmp)

	got := pidFilePath()
	want := filepath.Join(tmp, "devlog.pid")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestResolveEditor(t *testing.T) {
	t.Setenv("EDITOR", "")

	// Default
	cfg := Config{}
	got := resolveEditor(cfg)
	if got != "vi" {
		t.Errorf("default: got %q, want vi", got)
	}

	// Config
	cfg.Editor = "nano"
	got = resolveEditor(cfg)
	if got != "nano" {
		t.Errorf("config: got %q, want nano", got)
	}

	// Env overrides
	t.Setenv("EDITOR", "emacs")
	got = resolveEditor(cfg)
	if got != "emacs" {
		t.Errorf("env: got %q, want emacs", got)
	}
}

func TestResolvePathTemplate(t *testing.T) {
	got := resolvePathTemplate("<raw_dir>/<date>/git-<project>.log", "/data/raw", "2024-01-15", "myproject")
	want := "/data/raw/2024-01-15/git-myproject.log"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestResolveGitPathDefault(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("DEVLOG_RAW_DIR", tmp)

	cfg := Config{}
	got := resolveGitPath(cfg, "2024-01-15", "myproject")
	want := filepath.Join(tmp, "2024-01-15", "git-myproject.log")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestResolveGitPathCustom(t *testing.T) {
	cfg := Config{GitPath: "/custom/<date>/<project>-git.log"}
	got := resolveGitPath(cfg, "2024-01-15", "myproject")
	want := "/custom/2024-01-15/myproject-git.log"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestResolveNotesPathDefault(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("DEVLOG_RAW_DIR", tmp)

	cfg := Config{}
	got := resolveNotesPath(cfg, "2024-01-15")
	want := filepath.Join(tmp, "2024-01-15", "notes.md")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestResolveNotesPathCustom(t *testing.T) {
	cfg := Config{NotesPath: "/notes/<date>/notes.md"}
	got := resolveNotesPath(cfg, "2024-01-15")
	want := "/notes/2024-01-15/notes.md"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestDiscoverProjects(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("DEVLOG_RAW_DIR", tmp)

	// Create files in default template locations
	dateDir := filepath.Join(tmp, "2024-01-15")
	os.MkdirAll(dateDir, 0o755)
	os.WriteFile(filepath.Join(dateDir, "git-foo.log"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(dateDir, "git-bar.log"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(dateDir, "notes.md"), []byte("### At 10:00 #baz\nsome note\n\n"), 0o644)

	cfg := Config{}
	projects := discoverProjects(cfg, "2024-01-15")

	if len(projects) != 3 {
		t.Fatalf("expected 3 projects, got %d: %v", len(projects), projects)
	}
	if projects[0] != "bar" || projects[1] != "baz" || projects[2] != "foo" {
		t.Errorf("expected [bar baz foo], got %v", projects)
	}
}

func TestDiscoverProjectsCustomTemplate(t *testing.T) {
	tmp := t.TempDir()

	// Create files in custom locations
	dateDir := filepath.Join(tmp, "2024-01-15")
	os.MkdirAll(dateDir, 0o755)
	os.WriteFile(filepath.Join(dateDir, "myproject-git.log"), []byte("x"), 0o644)

	cfg := Config{GitPath: tmp + "/<date>/<project>-git.log"}
	projects := discoverProjects(cfg, "2024-01-15")

	if len(projects) != 1 || projects[0] != "myproject" {
		t.Errorf("expected [myproject], got %v", projects)
	}
}

func TestResolveTermGlobDefault(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("DEVLOG_RAW_DIR", tmp)

	cfg := Config{}
	got := resolveTermGlob(cfg, "2024-01-15", "myproject")
	want := filepath.Join(tmp, "2024-01-15", "term-myproject*.log")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestResolveTermGlobCustom(t *testing.T) {
	cfg := Config{TermPath: "/custom/<date>/terminal-<project>*.txt"}
	got := resolveTermGlob(cfg, "2024-01-15", "myproject")
	want := "/custom/2024-01-15/terminal-myproject*.txt"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestResolveClaudeCodeDirDefault(t *testing.T) {
	cfg := Config{}
	got := resolveClaudeCodeDir(cfg)
	home, _ := os.UserHomeDir()
	want := filepath.Join(home, ".claude", "projects")
	if got != want {
		t.Errorf("default: got %q, want %q", got, want)
	}
}

func TestResolveClaudeCodeDirCustom(t *testing.T) {
	custom := "/custom/claude/dir"
	cfg := Config{ClaudeCodeDir: &custom}
	got := resolveClaudeCodeDir(cfg)
	if got != custom {
		t.Errorf("custom: got %q, want %q", got, custom)
	}
}

func TestResolveClaudeCodeDirDisabled(t *testing.T) {
	empty := ""
	cfg := Config{ClaudeCodeDir: &empty}
	got := resolveClaudeCodeDir(cfg)
	if got != "" {
		t.Errorf("disabled: got %q, want empty string", got)
	}
}

func TestResolveClaudeCodeDirTilde(t *testing.T) {
	tilde := "~/.claude/custom"
	cfg := Config{ClaudeCodeDir: &tilde}
	got := resolveClaudeCodeDir(cfg)
	home, _ := os.UserHomeDir()
	want := filepath.Join(home, ".claude", "custom")
	if got != want {
		t.Errorf("tilde: got %q, want %q", got, want)
	}
}

func TestExtractProjectFromPath(t *testing.T) {
	tests := []struct {
		path, tmpl, rawDir, date, want string
	}{
		{
			"/data/raw/2024-01-15/git-foo.log",
			"<raw_dir>/<date>/git-<project>.log",
			"/data/raw", "2024-01-15", "foo",
		},
		{
			"/custom/2024-01-15/myproject-git.log",
			"/custom/<date>/<project>-git.log",
			"/data/raw", "2024-01-15", "myproject",
		},
	}
	for _, tt := range tests {
		got := extractProjectFromPath(tt.path, tt.tmpl, tt.rawDir, tt.date)
		if got != tt.want {
			t.Errorf("extractProjectFromPath(%q, %q, %q, %q) = %q, want %q",
				tt.path, tt.tmpl, tt.rawDir, tt.date, got, tt.want)
		}
	}
}

func TestDiscoverProjectsFromNotes(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("DEVLOG_RAW_DIR", tmp)

	dateDir := filepath.Join(tmp, "2024-01-15")
	os.MkdirAll(dateDir, 0o755)
	os.WriteFile(filepath.Join(dateDir, "notes.md"), []byte(
		"### At 09:00 #alpha\nfirst note\n\n"+
			"### At 10:00\nunaffiliated note\n\n"+
			"### At 11:00 #beta\nsecond note\n\n"+
			"### At 12:00 #alpha\nanother alpha note\n\n",
	), 0o644)

	cfg := Config{}
	projects := discoverProjectsFromNotes(cfg, "2024-01-15")

	if len(projects) != 2 {
		t.Fatalf("expected 2 projects, got %d: %v", len(projects), projects)
	}
	if projects[0] != "alpha" || projects[1] != "beta" {
		t.Errorf("expected [alpha beta], got %v", projects)
	}
}

func TestDiscoverProjectsFromNotesNoFile(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("DEVLOG_RAW_DIR", tmp)

	cfg := Config{}
	projects := discoverProjectsFromNotes(cfg, "2024-01-15")
	if len(projects) != 0 {
		t.Errorf("expected no projects, got %v", projects)
	}
}
