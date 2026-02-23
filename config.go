package main

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
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
