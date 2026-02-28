# CLAUDE.md

## Build and test

```sh
go build ./...   # compile
go test ./...    # run all tests
go vet ./...     # static analysis
```

All three must pass cleanly before submitting changes.

## Project structure

Single-package Go binary (`package main`). No subdirectories for source.

- `main.go` — entrypoint, subcommand dispatch
- `config.go` — config loading, path resolution, template helpers
- `server.go` — background daemon (snapshot loop, IPC socket)
- `snapshot.go` — git shadow-index snapshots
- `cmd.go` — CLI subcommands (note, gen, watch, start, stop, status)
- `generate.go` — summary generation (prompt assembly, LLM invocation)
- `ipc.go` — IPC types and client
- `state.go` — persistent state (watched repos)

## Key conventions

- Raw data paths are template-based: `<raw_dir>`, `<date>`, `<project>` are substituted at runtime. Per-project defaults: `<raw_dir>/<date>/git-<project>.log`. Notes use a single daily file: `<raw_dir>/<date>/notes.md` (project association via `#project` hashtags in headings).
- Config file: `$XDG_CONFIG_HOME/devlog/config.toml`
- XDG base directories are used throughout (data, config, state, runtime).
- Tests use `t.TempDir()` and `t.Setenv()` for isolation — no global state.
