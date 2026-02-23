# CLAUDE.md

## Build environment

This project uses a Nix flake for development dependencies. You must activate the Nix shell before running any Go commands:

```sh
nix develop --command <cmd>
```

Examples:

```sh
nix develop --command go build ./...
nix develop --command go test ./...
nix develop --command go vet ./...
```

## Build and test

```sh
nix develop --command go build ./...   # compile
nix develop --command go test ./...    # run all tests
nix develop --command go vet ./...     # static analysis
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

- Raw data paths are template-based: `<raw_dir>`, `<date>`, `<project>` are substituted at runtime. Defaults: `<raw_dir>/<date>/git-<project>.log` and `<raw_dir>/<date>/notes-<project>.md`.
- Config file: `$XDG_CONFIG_HOME/devlog/config.toml`
- XDG base directories are used throughout (data, config, state, runtime).
- Tests use `t.TempDir()` and `t.Setenv()` for isolation — no global state.
