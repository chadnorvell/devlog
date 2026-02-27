# devlog design specification

## 1. Overview

### 1.1 Purpose

The `devlog` tool automatically generates summaries of your daily work on
software engineering projects and outputs those summaries to Markdown files.
This makes it easy to remember what you were working on, what progress you
made, what you attempted, what worked, what didn't, and what was left to be
done, even if you come back to the project after a long time.

The tool accomplishes this by:

- Continuously collecting raw data during development: snapshots of code
  changes and manually logged notes/snippets. It also ingests external data
  sources like terminal session recordings and Claude Code session logs.

- Storing collected raw data in plain text files, timestamped and organized by
  date and project. External data sources (terminal logs, Claude Code
  sessions) are read in place from their original locations. This raw data is
  not intended for human consumption.

- Using an AI summarizer (defaulting to Claude Code) in headless mode to
  generate a rich, concise summary of the day's work from the raw data.

- Writing the summaries to Markdown files that work well with tools like
  Obsidian but are not specific to any tool.

### 1.2 Goals

- Require minimal manual effort. Git diff collection is fully automatic once a
  repo is watched. Summary generation can be triggered manually or via cron.

- Capture the *process* of development, not just the end result. The
  fine-grained diff history reveals dead ends, pivots, and the reasoning behind
  decisions, which is exactly what's hardest to reconstruct later.

- Produce summaries that let you resume work immediately, even after weeks away
  from a project.

### 1.3 Non-goals

- Web browsing data collection. This may be added in a future version, but is
  out of scope for the initial implementation.

- GUI note capture. Notes are captured via the terminal only.

### 1.4 Project standards

- Written in Go for portability and simplicity.

- Developed on NixOS. The repository includes a Nix flake for the development
  environment.

- Single binary. All subcommands (client and server) are compiled into one
  `devlog` executable.

- No external dependencies at runtime other than `git` and an AI CLI (like
  `claude` or `gemini-cli`). The KRunner integration (section 2.3) optionally
  requires a D-Bus session bus and KDialog; if either is unavailable, the
  feature is silently disabled and all other functionality works normally.

## 2. Architecture

### 2.1 Process model

The system has two process roles compiled into a single binary:

- **Server**: A long-running daemon that periodically snapshots git diffs for
  watched repositories and handles D-Bus requests to provide runtime data to
  other services. It listens on a Unix domain socket for commands from the
  client. It is typically managed by a systemd user service.

- **Client**: Short-lived CLI invocations (`devlog watch`, `devlog stop`,
  etc.) that send commands to the server over the Unix socket and print
  responses.

Some commands do not require a running server:

- `devlog -m <message>` / `devlog` (log a note): Writes directly to the raw
  data files. Does not contact the server.

- `devlog gen`: Reads raw data files and invokes the configured AI
  summarizer. Does not contact the server.

- `devlog watch` / `devlog unwatch`: If the server is running, these send an
  IPC command so the change takes effect immediately. If the server is not
  running, they modify `state.json` directly; the server will pick up the
  change on next startup.

### 2.2 IPC: Unix domain socket

The server listens on a Unix domain socket at:

```
$XDG_RUNTIME_DIR/devlog.sock
```

If `$XDG_RUNTIME_DIR` is not set, fall back to `/tmp/devlog-<uid>.sock`.

The protocol is line-delimited JSON over the socket. Each request is a single
JSON line; each response is a single JSON line.

#### Request format

```json
{"command": "<command>", "args": {"key": "value"}}
```

#### Response format

```json
{"ok": true, "data": { ... }}
{"ok": false, "error": "message"}
```

#### Commands

| Command     | Args                                  | Response `data`                                                    |
|-------------|---------------------------------------|--------------------------------------------------------------------|
| `watch`     | `{"path": "...", "name": "..."}`      | `{"watched": [{"path": "...", "name": "..."}, ...]}`               |
| `unwatch`   | `{"path": "..."}`                     | `{"watched": [{"path": "...", "name": "..."}, ...]}`               |
| `status`    | (none)                                | `{"watched": [{"path": "...", "name": "..."}, ...], "pid": 12345}` |
| `stop`      | (none)                                | `{}`                                                               |

The `name` field in the `watch` args is optional; if omitted, the server
derives the name from the repo directory basename.

### 2.3 D-Bus integration

To allow other services to integrate with `devlog`, the server optionally
registers on the D-Bus session bus. On startup, the server checks whether both
D-Bus and KDialog are available. If either is missing, D-Bus registration is
skipped with a log message and the server continues normally. When D-Bus is
enabled, the server implements these interfaces:

#### org.kde.krunner1

This allows KRunner to be used to submit a manual note for any project outside
of a terminal environment. The input starts with a hashtag indicating the
project. The D-Bus service provides auto-completion of known projects, such
that partial hashtag entry provides candidate projects for selection in
KRunner. If the hashtag is followed by whitespace and then note content,
submitting/running the action will call `devlog -m <content> -p <project>`. If
there is no non-whitespace content other than the hashtag, a KDialog will be
launched with a multi-line text box to capture longer input. When that dialog
is submitted, the action will call `devlog -m <content> -p <project>`.

- Destination path: `/krunner`
- Destination name: `org.devlog.krunner`

- **Match**

  - Is triggered when the query starts with `#`

  - The `#` hashtag is used to identify the project the note should be
    associated with, and this service provides auto-completion for all projects
    currently being watched

  - If the project name does not match any watched project and the query
    includes note content (e.g., `#newproject some text`), a lower-relevance
    fallback match is offered so that users can log notes for unwatched
    projects. If only the hashtag and project name are entered with no content,
    no fallback is offered (to avoid launching a dialog for a potential typo
    mid-autocomplete).

- **Run**

  - Is triggered when the user submits the KRunner input

  - Calls `devlog -m <content> -p <project>`

#### KRunner .desktop file

The `.desktop` file (`org.devlog.krunner.desktop`) must be installed to
`~/.local/share/krunner/dbusplugins/` for KRunner to discover it. Key metadata
entries:

- `X-Plasma-DBusRunner-Service=org.devlog.krunner*` — The trailing wildcard
  causes KRunner to dynamically discover the service by scanning the session
  bus for matching names. Without the wildcard, KRunner treats the service as
  D-Bus-activatable, which requires a separate D-Bus `.service` file. The
  wildcard approach allows KRunner to use the service when the devlog server is
  running and silently skip it when it is not.

- `X-Plasma-Runner-Match-Regex=^#` — Tells KRunner to only query this runner
  for queries starting with `#`, avoiding unnecessary D-Bus calls.

- `X-Plasma-Runner-Min-Letter-Count=2` — Requires at least two characters
  (the `#` plus one letter) before querying.

### 2.4 Server lifecycle

- **PID file**: The server writes its PID to
  `$XDG_RUNTIME_DIR/devlog.pid` (or `/tmp/devlog-<uid>.pid`). Before
  starting, it checks this file. If a process with that PID is still running,
  it prints a message and exits. If the PID file is stale (process not
  running), it removes it and proceeds.

- **Startup**: Create the PID file, open the Unix socket, begin the watch
  loop.

- **Shutdown**: On `SIGTERM`, `SIGINT`, or receiving a `stop` command: stop
  all watch goroutines, close the socket, remove the PID file and socket file,
  and exit cleanly.

### 2.5 Server state persistence

The set of watched repositories is persisted to a JSON file so that the server
can resume watching after a restart:

```
$XDG_STATE_HOME/devlog/state.json
```

If `$XDG_STATE_HOME` is not set, fall back to `~/.local/state/devlog/state.json`.

```json
{
  "watched": [
    {"path": "/home/user/dev/project-a", "name": "project-a"},
    {"path": "/home/user/dev/project-b", "name": "my-custom-name"}
  ]
}
```

On startup, the server reads this file and begins watching any repos listed.
When a `watch` or `unwatch` command is processed, the file is updated
atomically (write to a temp file, then rename).

## 3. Configuration

### 3.1 Configuration file

Configuration is read from:

```
$XDG_CONFIG_HOME/devlog/config.toml
```

If `$XDG_CONFIG_HOME` is not set, fall back to `~/.config/devlog/config.toml`.

```toml
# Directory for generated summary Markdown files.
# This is the directory you'd point at your Obsidian vault, documents folder,
# etc. Default: $XDG_DATA_HOME/devlog/log (typically ~/.local/share/devlog/log)
log_dir = ""

# Directory for raw data files (git diffs, notes). These are machine-generated
# and not intended for human consumption; keep them separate from your notes.
# Default: $XDG_DATA_HOME/devlog/raw (typically ~/.local/share/devlog/raw)
raw_dir = ""

# Path template for the notes file. Unlike the other path templates, this
# one does not include a <project> variable — there is a single notes file
# per day, with project associations indicated by hashtags in the headings
# (see section 4.2). The special <raw_dir> variable expands to the resolved
# raw data file directory. Default places the file in raw_dir.
notes_path = "<raw_dir>/<date>/notes.md"

# Path templates for raw data files. Each template must include both
# <date> and <project> variables. Defaults place files in raw_dir.
git_path = "<raw_dir>/<date>/git-<project>.log"
term_path = "<raw_dir>/<date>/term-<project>*.log"

# Interval in seconds between git diff snapshots. Default: 300 (5 minutes).
snapshot_interval = 300

# Editor to use for `devlog` (no -m). Falls back to $EDITOR, then "vi".
editor = ""

# AI summarizer command. Change this to use other AI tools.
gen_cmd = "claude -p"

# AI compressor command. Change this to use other AI tools.
comp_cmd = "gemini --model gemini-3-flash"

# Directory where Claude Code stores project session logs. Set to "" to
# disable Claude Code session ingestion. Default: ~/.claude/projects
claude_code_dir = "~/.claude/projects"
```

The configuration file is optional. All values have sensible defaults.

**Path templates**: The `git_path`, `notes_path`, and `term_path` settings are
path templates that control where raw data files are read from and written to.
All templates support the `<raw_dir>` variable (expands to the resolved raw
directory) and the `<date>` variable (substituted with `YYYY-MM-DD`).

| Template     | `<project>` | Glob | Discovers projects | Notes |
|--------------|:-----------:|:----:|:------------------:|-------|
| `git_path`   | required    | no   | yes                | Per-project file. Project names are extracted by globbing for `<project>`. |
| `notes_path` | no          | no   | yes (via content)  | Single daily file shared across projects. Projects are discovered by parsing `#project` hashtags from headings (see section 4.2), not from the file path. |
| `term_path`  | required    | yes  | no                 | Per-project file(s). The glob wildcard makes it ambiguous where the project name ends, so this template cannot discover projects. It only matches files for projects already discovered through other sources. |

These templates are used throughout the application for writing raw data files,
discovering projects, and reading data for summary generation. See section 5.4
for details on project discovery.

### 3.2 Data directories

**Log directory** (generated summaries): The directory where `<YYYY-MM-DD>.md`
summary files are written. Determined by, in order of precedence:

1. The `DEVLOG_LOG_DIR` environment variable
2. The `log_dir` setting in `config.toml`
3. `$XDG_DATA_HOME/devlog/log`; if `$XDG_DATA_HOME` is not set, use
   `~/.local/share/devlog/log`

**Raw directory** (collected data): The directory where raw data files are
stored. Determined by, in order of precedence:

1. The `DEVLOG_RAW_DIR` environment variable
2. The `raw_dir` setting in `config.toml`
3. `$XDG_DATA_HOME/devlog/raw`; if `$XDG_DATA_HOME` is not set, use
   `~/.local/share/devlog/raw`

### 3.3 Directory structure

**Log directory** (`$XDG_DATA_HOME/devlog/log/` by default):

```
<log_dir>/
└── <YYYY-MM-DD>.md
```

**Raw directory** (`$XDG_DATA_HOME/devlog/raw/` by default):

```
<raw_dir>/
└── <YYYY-MM-DD>/
    ├── notes.md
    ├── git-<project>.log
    ├── term-<project>*.log
    ├── comp-git-<project>.md
    ├── comp-term-<project>.md
    └── comp-claude-<project>.md
```

These are the default locations. Paths for raw data files are configurable via
the `git_path`, `notes_path`, and `term_path` templates in `config.toml` (see
section 3.1). The `comp-*` files are generated by `devlog gen` during the
compression step (section 5.3) and are always stored alongside the raw data.

**Claude Code sessions** (`~/.claude/projects/` by default):

```
<claude_code_dir>/
└── -<path-with-slashes-replaced-by-dashes>/
    ├── <session-uuid>.jsonl
    ├── <session-uuid>.jsonl
    └── ...
```

Claude Code session logs are external to devlog's data directories. They are
read (not written) during summary generation. See section 4.5.

**Config** (`$XDG_CONFIG_HOME/devlog/`):

```
config.toml
```

**State** (`$XDG_STATE_HOME/devlog/`):

```
state.json
```

**Runtime** (`$XDG_RUNTIME_DIR/`):

```
devlog.sock
devlog.pid
```

## 4. Data collection

### 4.1 Project identification

A "project" is identified by a name, which defaults to the basename of the git
repository's root directory as returned by `git rev-parse --show-toplevel`.
For example, the repo at `/home/chad/dev/devlog` has the default project name
`devlog`.

The project name can be overridden with the `--name` flag on the `watch`
command (see section 6.4). This allows watching two repos that have the same
directory basename — e.g., `devlog watch /home/chad/work/foo --name work-foo`.

**Collision handling**: If a `watch` command would result in two watched repos
having the same project name (whether default or overridden), the command must
reject it with an error message identifying the conflict. Project names must be
unique across all watched repos.

### 4.2 Manually-logged notes and snippets

Data can be captured by any external method or tooling into a notes file.
Examples include direct notes logged by the user, snippets from the web or
documentation, and snippets from the terminal or code. The file path is
determined by the `notes_path` template (see section 3.1). By default, notes
are read from `<raw_dir>/<YYYY-MM-DD>/notes.md`.

The `devlog` command (see section 6.1) provides one way to log this data, which
will be appended to the file at `notes_path`.

#### Raw data file format: `notes.md`

```
### At 14:35 #project
<message text>

```

Since this file can contain data provided by multiple external sources, we
can't count on it having a perfectly consistent format. These are the
formatting assumptions that will be used by the AI summarizer:

- There will be a single notes file for each day, in Markdown format.

- Each note entry will have a Markdown heading with the time the entry was
  recorded, and optionally the name of the project the note is associated with
  in hashtag format. The `devlog` command will write these headings as `### At
  HH:MM #project`. A heading of `### At HH:MM` suggests a notes entry that
  isn't associated with any particular project. After the heading, the note
  text follows verbatim (may be multiple lines), terminated by a blank line.

- The source of the note will be inferred from the note text. For example, if
  it contains something like `URL: https://...`, it can be assumed to be
  clipped from a website, or if it contains something like `Path:
  ~/dev/foo/...` and a code block, it could be inferred to be a code snippet or
  terminal output, depending on the content.

### 4.3 Git diff snapshots

The server captures git diffs at a configurable interval (default: 5 minutes)
for each watched repository. The goal is to record the evolution of code
changes at a much finer granularity than the git commit history.

#### Shadow index technique

To capture a diff that includes untracked files without disturbing the user's
real staging area, the snapshot process uses a **shadow git index**:

1. Construct the absolute path to the shadow index:
   `<repo_path>/.git/devlog_shadow_index`.
2. Run `git -C <repo_path> add -A` with the environment variable
   `GIT_INDEX_FILE` set to the absolute shadow index path.
3. Run `git -C <repo_path> diff --no-color HEAD` with the same
   `GIT_INDEX_FILE` environment variable.

**Important**: The `GIT_INDEX_FILE` must be an absolute path (not relative),
because `git -C` changes the working directory internally. And in Go, it must
be set per-command via `cmd.Env`, not globally via `os.Setenv`, because the
snapshot ticker and socket listener run concurrently:

```go
shadowIndex := filepath.Join(repoPath, ".git", "devlog_shadow_index")
cmd := exec.Command("git", "-C", repoPath, "add", "-A")
cmd.Env = append(os.Environ(), "GIT_INDEX_FILE="+shadowIndex)
```

This produces a diff that includes all tracked changes *and* new untracked
files, without touching the user's real index or staging area.

#### Deduplication

The server keeps the most recent diff for each repo in memory (or in a
temp file at `.git/devlog_last.patch`). Before appending a new snapshot, it
compares the current diff to the previous one. If they are identical, the
snapshot is skipped. This avoids filling the log with duplicate diffs when the
user is idle.

#### Raw data file format: `git-<project>.log`

The file path is determined by the `git_path` template (see section 3.1).
Each snapshot is appended as a fenced block:

```
=== SNAPSHOT 14:30 ===
<git diff output>

```

The delimiter line format is `=== SNAPSHOT HH:MM ===` where `HH:MM` is the
local time of the snapshot. The diff output follows verbatim, terminated by a
blank line.

If the diff is empty (no changes at all), nothing is appended.

#### Date boundary handling

At the beginning of each snapshot cycle, the server checks the current date.
If the date has changed since the last cycle, it:

1. Starts writing to a new raw data directory for the new date.
2. Resets the deduplication state (previous diff) so the first snapshot of
   the new day captures the full current diff, even if it's unchanged from the
   last snapshot of the previous day.

### 4.4 Terminal session logs

Terminal session logs capture terminal input and output recorded during
development, for example using the Linux `script` command. These logs provide
insight into what the developer was doing beyond just code changes — running
tests, reading documentation, debugging, using REPLs, etc.

Terminal logs are **not collected by devlog**. The user is responsible for
recording them with an external tool (e.g., `script`) and placing them in the
expected location. Devlog only reads these files during summary generation.

The file path is determined by the `term_path` template (see section 3.1). The
default template is `<raw_dir>/<date>/term-<project>*.log`. The trailing `*`
wildcard allows multiple session files per project per day (e.g.,
`term-devlog-morning.log`, `term-devlog-2.log`). The naming convention after the
project name is up to the user; devlog matches any suffix.

#### Raw data file format

Terminal logs are plain text as produced by `script` or similar tools. Devlog
does not impose any internal format — the file contents are passed directly to
the AI summarizer. They may contain ANSI escape codes, which the summarizer is
expected to handle gracefully (ignoring them for content extraction).

### 4.5 Claude Code session logs

Claude Code automatically logs all sessions to JSONL files in
`~/.claude/projects/`. These logs can serve as a rich data source for
understanding what the developer was working on and trying to accomplish,
especially when Claude Code is used heavily during development.

**Devlog does not collect or copy these files.** It reads them directly from
Claude Code's log directory during summary generation and extracts the content
relevant to the requested date.

#### Log directory structure

Claude Code organizes logs by project. The project directory name is derived
from the absolute path to the project's working directory, with `/` characters
replaced by `-`. For example:

| Project path          | Claude Code log directory        |
|-----------------------|----------------------------------|
| `/home/chad/dev/ctrl` | `-home-chad-dev-ctrl/`           |
| `/home/user/work/api` | `-home-user-work-api/`           |

Within each project directory, session files are named `<uuid>.jsonl`, where
each file contains the full transcript of one Claude Code session. Sessions may
also have subdirectories (`<uuid>/subagents/`) containing subagent transcripts.

Devlog maps from watched projects to Claude Code log directories using the
project's `path` field in `state.json`. The path is converted to the directory
name format by replacing `/` with `-` (e.g., `/home/chad/dev/ctrl` becomes
`-home-chad-dev-ctrl`). Claude Code session ingestion is only available for
projects that are in the watch list, because reversing the encoding is ambiguous
(a `-` could be a path separator or a literal hyphen in a directory name).

#### JSONL entry format

Each line in a session JSONL file is a JSON object with a `type` field. The
relevant entry types are:

| Type                    | Description                                              |
|-------------------------|----------------------------------------------------------|
| `user`                  | A user message or tool result submission                 |
| `assistant`             | A Claude response (text, tool calls, or thinking)        |
| `system`                | System/session metadata                                  |
| `file-history-snapshot` | File state backup (internal bookkeeping)                 |
| `progress`              | Progress events like hook execution                      |

Every `user` and `assistant` entry has a `timestamp` field in ISO 8601 format
with millisecond precision (e.g., `2026-02-22T08:15:47.273Z`, always UTC).

**User entries** have a `message` field with `role: "user"` and a `content`
field that is either a string (the user's prompt) or an array of content blocks
(typically tool results being returned to the model):

```json
{"type": "user", "timestamp": "...", "message": {"role": "user", "content": "user prompt text"}, ...}
{"type": "user", "timestamp": "...", "message": {"role": "user", "content": [{"type": "tool_result", ...}]}, ...}
```

**Assistant entries** have a `message` field with `role: "assistant"` and a
`content` array containing blocks of different types:

- `text`: The assistant's text response.
- `tool_use`: A tool invocation with `name` and `input` fields.
- `thinking`: The model's extended thinking (internal reasoning, can be very
  large).

```json
{"type": "assistant", "timestamp": "...", "message": {"role": "assistant", "content": [
  {"type": "thinking", "thinking": "..."},
  {"type": "text", "text": "response text"},
  {"type": "tool_use", "name": "Edit", "input": {"file_path": "...", ...}}
]}, ...}
```

Entries are linked via `parentUuid`/`uuid` fields, forming a thread. The
`sessionId` field groups all entries in a session.

#### Preprocessing for summary generation

Because the raw JSONL files are large and contain much data that is not useful
for summarization (thinking blocks, token usage, signatures, file-history
snapshots), devlog preprocesses them before including the content in the
summarizer prompt. The preprocessing step:

1. Scans all `*.jsonl` files in the project's Claude Code log directory
   (ignoring `subagents/` subdirectories).

2. Parses each file line by line. For each entry with a `timestamp` field,
   checks whether the timestamp falls on the target date (converting from UTC
   to local time). Entries outside the target date are skipped.

3. If a session has no entries on the target date, it is skipped entirely.

4. For sessions that do have entries on the target date, extracts a compact
   transcript containing:

   - **User text messages**: The `content` string from user entries (skipping
     tool-result entries where `content` is an array).
   - **Assistant text responses**: The `text` field from `text`-type content
     blocks.
   - **Tool use summaries**: The tool `name` and a brief summary of the key
     input parameters (e.g., file paths for `Read`/`Edit`/`Write`, commands for
     `Bash`, patterns for `Grep`/`Glob`). Full tool inputs and outputs are
     omitted to keep the prompt manageable.
   - **Thinking blocks are excluded** entirely (they are large and their
     reasoning is reflected in the assistant's visible responses).

5. Formats the extracted content as a readable transcript (see below).

#### Preprocessed output format

The preprocessed output for each session is formatted as:

```
=== SESSION started HH:MM ===

> user prompt text

assistant response text

[Tool: Edit file_path="main.go"]
[Tool: Bash command="go test ./..."]

> next user prompt

next assistant response

```

The `HH:MM` is the local time of the first entry on the target date.
User messages are prefixed with `> `. Tool uses are shown as single-line
summaries in `[Tool: Name key="value"]` format. Assistant text is shown
verbatim. Entries are separated by blank lines.

If multiple sessions have activity on the target date, they are concatenated
in chronological order (sorted by the timestamp of their first entry on that
date).

## 5. Summary generation

### 5.1 Invocation

Summary generation is triggered by `devlog gen` (see section 6.2). It does not
require a running server. It reads raw data files directly from disk and
invokes the configured AI summarizer to produce the summary.

### 5.2 Staleness check

Before generating, the command checks whether regeneration is needed:

1. Look for an existing summary at `<log_dir>/<date>.md`.
2. If it exists, get its mtime.
3. For each per-project source path template (`git_path`, `term_path`),
   substitute `<date>` and glob for `<project>`. Also check the mtime of the
   notes file (resolved from `notes_path` for `<date>`). Also check the mtime
   of Claude Code session JSONL files (if `claude_code_dir` is configured) for
   any projects whose paths map to a Claude Code log directory. Collect the max
   mtime across all matching files.
4. If the summary's mtime is more recent than the max raw data mtime, print
   a message ("Summary is up to date, no new data since last generation") and
   exit without invoking the AI.
5. Otherwise, delete the existing summary file and proceed with generation.

### 5.3 Bulk data compression

Data sources that automatically collect large amounts of data like git snapshots, terminal logs, and Claude Code sessions
can be significantly compressed via summarization without losing any details relevant to the final dev log entry. For example,
one hour of this kind of data, comprising hundreds of kilobytes of text, could be summarized in a few bullet points explaining
what the goal was, what was attempted, what worked, and what didn't. Doing this compression for each of these data sources before
doing the overall summarization reduces the size of the final prompt (and the risk of exceeding the input token limit) and permits
the use of smaller, cheaper models during the compression phase.

For each of git snapshots, terminal logs, and processed Claude Code sessions,
this process will be used to generate compressed representations:

1. Determines the output path for the compressed artifact:
   - Git snapshots: `<raw_dir>/<date>/comp-git-<project>.md`
   - Terminal logs: `<raw_dir>/<date>/comp-term-<project>.md`
   - Claude Code sessions: `<raw_dir>/<date>/comp-claude-<project>.md`

2. Collects the source files for this data type. If no source files exist,
   skip this data type.

3. Checks whether compression can be skipped: if the compressed artifact file
   exists and its mtime is more recent than the mtime of all source files,
   the existing compressed file is used and steps 4–6 are skipped.

4. Assembles the full prompt by substituting the file contents directly into
   this prompt template:

```
You are summarizing data automatically logged during a software engineering
session.

Description of the data:

<if the data is git snapshots>
- Time-stamped snapshots of uncommitted code changes, taken every 5 minutes.
  These show the evolution of the code over the day, including approaches that
  were tried and abandoned.
</if>
<if the data is terminal logs>
- Terminal session recordings captured with tools like `script`. These show the
  developer's terminal activity: commands run, test output, debugging sessions,
  REPL interactions, etc. May contain ANSI escape codes which should be
  ignored.
</if>
<if the data is Claude Code sessions>
- Preprocessed transcripts of Claude Code sessions for the day, showing the
  developer's interactions with an AI coding assistant. Contains user prompts,
  assistant responses, and tool use summaries. This reveals what the developer
  was trying to accomplish, what approaches were discussed, and what changes
  were made through the AI assistant.
</if>

Below is the raw data collected during the day.
<for each raw data file that exists>

--- <filename> ---
<file contents>
</for each>

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
```

5. Passes the assembled prompt to the AI compressor via stdin. For example,
   if `comp_cmd = "gemini --model gemini-3-flash"`:

   ```bash
   echo "<assembled prompt>" | gemini --model gemini-3-flash
   ```

   In Go, this means parsing `comp_cmd` into a command and arguments, writing
   the prompt to `cmd.Stdin`, and reading the response from `cmd.Stdout`:

   ```go
   // Pseudo-code for dynamic command execution
   args := strings.Fields(config.CompCmd)
   cmd := exec.Command(args[0], args[1:]...)
   cmd.Stdin = strings.NewReader(assembledPrompt)
   output, err := cmd.Output()
   ```

6. Writes the command's stdout to the compressed artifact file.

If the command specified in `comp_cmd` is not found on `$PATH`, exit with an
error: "Compressor command '<cmd>' not found on $PATH."

### 5.4 Per-project summarization

The generation process:

1. **Discover projects from per-project templates**: For each path template
   that contains a `<project>` variable and does not contain literal glob
   wildcards (`git_path`), substitute `<date>` and replace `<project>` with a
   glob wildcard `*`. Glob the filesystem and extract project names from
   matches using the template as a pattern. (Templates without `<project>`,
   like `notes_path`, and glob-enabled templates, like `term_path`, are not
   used for this step.)

2. **Discover projects from notes entries**: Resolve the `notes_path` template
   for `<date>` and, if the file exists, parse the headings for project
   hashtags. Each unique hashtag adds a project to the discovered set. Notes
   entries without a hashtag are grouped under a pseudo-project (see below).

3. **Discover projects from Claude Code sessions**: If `claude_code_dir` is
   configured, use the watched repos from `state.json` to find Claude Code
   session directories. For each watched repo, convert its absolute path to
   the Claude Code directory name format (replace `/` with `-`, e.g.,
   `/home/chad/dev/ctrl` becomes `-home-chad-dev-ctrl`) and check whether the
   corresponding directory exists in `claude_code_dir`. If it does, and any
   JSONL file in that directory contains entries on the target date, add the
   project to the discovered set using the watched repo's name. (Claude Code
   discovery is limited to watched projects because reversing the path
   encoding is ambiguous — a `-` in the directory name could be a path
   separator or a literal hyphen in a directory name.)

4. Take the union of project names across all discovery methods.

5. For each project:
   a. Resolve per-project path templates (`git_path`) by substituting `<date>`
      and `<project>`, and read whichever files exist.
   b. Resolve glob-enabled templates (like `term_path`) by substituting
      `<date>` and `<project>` with the known values, then globbing the
      result. Read all matching files and concatenate their contents.
   c. Filter the notes file for entries matching this project's hashtag. Only
      the matching entries are included in this project's prompt (see section
      4.2 for the heading format). Notes entries without a hashtag are not
      included — they belong to the unaffiliated pseudo-project (see below).
   d. If `claude_code_dir` is configured and the project has a known repo path
      (from `state.json`), run the Claude Code preprocessing step (section
      4.5) to extract a transcript for the target date.
   e. Run the AI compressor on bulk data (section 5.3).

6. Invoke the AI summarizer per project (section 5.5).
7. Assemble the per-project summaries into a single Markdown file.

**Unaffiliated notes**: Notes entries without a project hashtag (`### At HH:MM`
with no `#project`) are treated as a separate pseudo-project called "general"
for the purposes of summary generation. If any unaffiliated notes exist for the
target date, they are collected and summarized as their own project section,
appearing in the output file under a `## general` heading alongside the real
project sections. No other data sources (git diffs, terminal logs, Claude Code
sessions) contribute to this pseudo-project — it contains only the unaffiliated
notes.

#### Data source availability by project status

The table below summarizes which data sources are available depending on whether
a project is in the watch list. Important projects should be watched to get full
coverage. Data collection for unwatched projects is best-effort.

| Data source          | Watched projects | Unwatched projects                        |
|----------------------|------------------|-------------------------------------------|
| Notes/snippets       | Yes              | Yes (via `devlog -m -p <project>`)        |
| Git diffs            | Yes (auto)       | No (server collects only watched repos)   |
| Terminal logs        | Yes              | Only if the project is also discovered through another source (e.g., notes) |
| Claude Code sessions | Yes              | No (requires repo path from watch list)   |

A project appears in a summary if it is discovered through at least one
discovery-capable source: git diffs, manual notes, or Claude Code sessions. An
unwatched project with only terminal logs will not be discovered and will not
appear in the summary.

### 5.5 AI summarizer invocation

For each project, the tool:

1. Collects the data for this project as described in section 5.4 step 5:
   filtered notes entries, and the compressed artifact files generated in
   section 5.4 step 5e (`comp-git-<project>.md`, `comp-term-<project>.md`,
   `comp-claude-<project>.md` — whichever exist).

2. Assembles the full prompt by substituting the file contents directly into
   the prompt template (see section 5.6).

3. Passes the assembled prompt to the AI summarizer via stdin. For example,
   if `gen_cmd = "claude -p"`:

   ```bash
   echo "<assembled prompt>" | claude -p
   ```

   In Go, this means parsing `gen_cmd` into a command and arguments, writing the
   prompt to `cmd.Stdin`, and reading the response from `cmd.Stdout`:

   ```go
   // Pseudo-code for dynamic command execution
   args := strings.Fields(config.GenCmd)
   cmd := exec.Command(args[0], args[1:]...)
   cmd.Stdin = strings.NewReader(assembledPrompt)
   output, err := cmd.Output()
   ```

4. Captures the command's stdout as the summary text for this project.

If the command specified in `gen_cmd` is not found on `$PATH`, exit with an
error: "Summarizer command '<cmd>' not found on $PATH."

If the command exits with a non-zero status, print the error output and exit
with a non-zero status. Do not write a partial summary file.

### 5.6 Prompt template

The prompt template below is used for each project. The tool substitutes
`<project>`, `<date>`, and the data file contents before sending to the AI
summarizer. Sections for files that don't exist are omitted entirely.

```
You are summarizing a day of software engineering work on the project
"<project>" for the date <date>.

Below is the data collected during the day.
<for each data file that exists>

--- <filename> ---
<file contents>
</for each>

Description of data sources:

- notes.md: Manually logged notes and snippets with timestamps. These can be
  developer notes expressing intent, observations, and decisions. They can
  also be snippets captured from code, docs, the web, or terminal sessions.

- comp-git-<project>.md: AI-compressed summary of time-stamped snapshots of
  uncommitted code changes, taken every 5 minutes. Describes the evolution of
  the code over the day, including approaches that were tried and abandoned.

- comp-term-<project>.md: AI-compressed summary of terminal session
  recordings. Describes the developer's terminal activity: commands run, test
  output, debugging sessions, REPL interactions, etc.

- comp-claude-<project>.md: AI-compressed summary of Claude Code session
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
```

The `<for each>` block is pseudo-template notation. In the assembled prompt,
it expands to one section per file, e.g.:

```
--- notes.md ---
### At 10:20 #myproject
Starting work on the CLI parser

--- comp-git-myproject.md ---
Worked on CLI parser implementation. Started with a flag-based approach around
10:15, then pivoted to a subcommand-based design around 11:00 when the flag
approach couldn't handle nested commands. By 12:30 the basic dispatch was
working with watch, unwatch, and gen subcommands.

--- comp-term-myproject.md ---
Ran go test ./... repeatedly during CLI parser development. Tests were initially
failing due to incorrect argument parsing. Fixed by switching from flag.Parse()
to flag.FlagSet per subcommand. Also ran go vet which caught an unused variable.

--- comp-claude-myproject.md ---
Asked Claude to help fix the failing test in main_test.go. The test was failing
because the flag parsing was global. Claude suggested using flag.FlagSet per
subcommand and edited main.go to implement this pattern.
```

### 5.7 Output format: `<date>.md`

The generated summary file lives at `<log_dir>/<YYYY-MM-DD>.md`.

```markdown
# <YYYY-MM-DD>

## <project-1>

<AI-generated summary for project-1>

## <project-2>

<AI-generated summary for project-2>
```

Projects are listed in alphabetical order. The file begins with a top-level
heading of the date, followed by second-level headings for each project.

## 6. Command line interface

The `devlog` command is the single entry point. Behavior is determined by the
subcommand (or lack thereof).

### 6.1 `devlog [-m <message>] [-c <code>] [-p <project>]` (no subcommand)

Log a note for the current project.

**Behavior**:

1. Determine the project name: If the `-p` argument is provided, use it as the
   project name. Otherwise, resolve the absolute path to the current repo root,
   then read `state.json` and look for an entry whose `path` matches the repo
   root. If found, use its `name`. If not found (repo is not watched), fall
   back to the basename of the repo root. This ensures notes use the same
   project name as the watch command, including any `--name` override. If
   invoked outside of a git repo without the `-p` argument, record a note
   without a project hashtag.
2. Determine today's date (`YYYY-MM-DD`).
3. If `-m <message>` is provided, use `<message>` as the note text.
4. If `-m` is not provided, create a temporary file pre-filled with:
   ```
   # Project: <project>
   # Enter your note below. Lines starting with # are ignored.
   ```
   If there is no project (outside a git repo without `-p`), substitute `N/A`
   for `<project>` in the template. Open this file in `$EDITOR` (falling back
   to the configured editor, then `vi`). When the editor exits, read the file,
   strip lines starting with `#`, and trim whitespace. If the result is empty,
   print "Note cancelled (empty message)" and exit 0.
5. If `-c` is provided, after the message add a newline and the content
   wrapped in Markdown code block delimiters.
6. Resolve the `notes_path` template for today's date. Append the note to the
   resulting path using the format defined in section 4.2. Create parent
   directories if needed.
7. If a project was determined, print "Logged note for <project>." If no
   project, print "Logged note."

**Does not require a running server.**

### 6.2 `devlog gen [<date>]`

Generate a summary for `<date>` (default: today).

**Behavior**:

1. Validate date format if provided (must be `YYYY-MM-DD`). If invalid, print
   an error and exit 1.
2. Discover projects using the template-based method described in section 5.4
   (substitute `<date>`, glob for `<project>`). If no files match any template,
   print "No raw data for <date>" and exit 0.
3. Run the staleness check (section 5.2). If the summary is up to date, print
   a message and exit 0.
4. For each project found in the raw data, invoke the configured AI
   summarizer (section 5.5).
5. Assemble and write the summary file (section 5.7).
6. Print "Summary written to <path>".

**Does not require a running server.**

### 6.3 `devlog gen-prompt [<date>]`

Print the prompt that will be used to generate the summary for `<date>`
(default: today).

**Behavior**:

1. Validate date format if provided (must be `YYYY-MM-DD`). If invalid, print
   an error and exit 1.
2. Discover projects using the template-based method described in section 5.4
   (substitute `<date>`, glob for `<project>`). If no files match any template,
   print "No raw data for <date>" and exit 0.
3. For each project, assemble the prompt (section 5.6) from whichever data
   files exist.
4. Print the assembled prompt to `stdout`.

This can be used to inspect the data and prompt that `devlog gen` would send to
the AI summarizer. Unlike `gen`, this command does not perform a staleness
check — it always assembles and prints the prompt, even if the summary is
already up to date.

**Does not require a running server.**

### 6.4 `devlog watch [<path>] [--name <name>]`

Start watching a git repository.

**Precondition**: If `<path>` is not provided, the command must be invoked
from within a git repository (use its root). If not, print an error and exit 1.
If `<path>` is provided, resolve it to the git repo root via
`git -C <path> rev-parse --show-toplevel`. If this fails, print an error and
exit 1.

**Options**:

- `--name <name>`: Override the project name used for this repo instead of
  deriving it from the directory basename. This is useful when watching two
  repos that have the same directory name (see section 4.1).

**Behavior**:

1. Resolve the absolute path to the repo root.
2. Determine the project name: use `--name` if provided, otherwise use the
   basename of the repo root.
3. Try to send a `watch` command to the server via the Unix socket.
4. If the server is running, print the server's response. If the repo was
   already watched, indicate that. Always print the full list of currently
   watched repos (showing both path and project name).
5. If the server is not running (socket doesn't exist or connection refused),
   fall back to modifying `state.json` directly:
   a. Read the current `state.json` (or start with an empty watch list if
      the file doesn't exist).
   b. Check for name collisions against existing entries (see section 4.1).
      If a collision is found, print an error and exit 1.
   c. If the repo path is already in the watch list, indicate that.
      Otherwise, add the entry and write `state.json` atomically.
   d. Print the updated watch list and note that the server is not running,
      so snapshot collection will begin when it starts.

**Does not require a running server.**

### 6.5 `devlog unwatch [<path>]`

Stop watching a git repository.

**Precondition**: Same resolution logic as `watch`.

**Behavior**:

1. Resolve the absolute path to the repo root.
2. Try to send an `unwatch` command to the server via the Unix socket.
3. If the server is running, print the server's response. If the repo was
   already not being watched, indicate that. Always print the full list of
   currently watched repos.
4. If the server is not running (socket doesn't exist or connection refused),
   fall back to modifying `state.json` directly:
   a. Read the current `state.json`. If the file doesn't exist, the watch
      list is empty — print that the repo is not being watched and exit 0.
   b. If the repo path is not in the watch list, indicate that.
      Otherwise, remove the entry and write `state.json` atomically.
   c. Print the updated watch list.

**Does not require a running server.**

### 6.6 `devlog start`

Start the devlog server in the foreground.

**Behavior**:

1. Check the PID file. If a server is already running, print "devlog server is
   already running (PID <pid>)" and exit 0.
2. Write the PID file.
3. Create the Unix socket and begin listening.
4. Load `state.json` and start watching any previously-watched repos.
5. Enter the main loop (handle socket commands, run snapshot cycles).
6. On shutdown signal, clean up and exit.

This command runs in the foreground and does not daemonize itself. Backgrounding
is handled by systemd or the user's shell.

#### Server concurrency model

The server runs three kinds of concurrent work:

- **Socket listener goroutine**: Accepts connections on the Unix socket. For
  each connection, reads one JSON request line, dispatches the command, writes
  one JSON response line, and closes the connection.

- **Snapshot ticker**: A single `time.Ticker` fires at the configured snapshot
  interval. On each tick, the server iterates over all watched repos
  sequentially and takes a snapshot for each. Snapshots are I/O-bound (running
  `git`), so sequential execution per tick is fine.

- **D-Bus listener goroutine** (optional): If D-Bus integration is enabled
  (see section 2.3), handles incoming D-Bus method calls for the KRunner
  interface. Reads the watched repo list (takes a read lock).

- **Main goroutine**: Coordinates shutdown. Listens for OS signals (`SIGTERM`,
  `SIGINT`) and the `stop` IPC command. When triggered, cancels a shared
  `context.Context`, which causes the socket listener, snapshot ticker, and
  D-Bus listener (if active) to stop.

**Shared state**: The list of watched repos is the only mutable shared state.
It is accessed by the socket listener (watch/unwatch commands), the snapshot
ticker, and the D-Bus listener (if active). Protect it with a `sync.RWMutex`:
the snapshot ticker and D-Bus listener take a read lock; watch/unwatch commands
take a write lock.

### 6.7 `devlog stop`

Stop the running devlog server.

**Behavior**:

1. Send a `stop` command to the server via the Unix socket.
2. If the server is not running (socket doesn't exist or connection refused),
   print "devlog server is not running" and exit 0.
3. Wait briefly for the server process to exit (check PID file removal, with a
   timeout of 5 seconds).
4. Print "devlog server stopped."

### 6.8 `devlog status`

Print the current server status.

**Behavior**:

1. Send a `status` command to the server via the Unix socket.
2. If the server is not running, print "devlog server is not running" and
   exit 0.
3. Print the server PID and the list of watched repos.

## 7. Error handling

### 7.1 Server errors

| Condition | Behavior |
|-----------|----------|
| Watched repo directory is deleted or becomes inaccessible | Log a warning to stderr. Skip that repo's snapshot cycle. Do not remove it from the watch list. |
| Git commands fail for a watched repo (e.g., corrupt repo) | Log a warning to stderr with the git error message. Skip that repo's snapshot cycle. |
| Cannot create raw data directory | Log error to stderr. Skip snapshot. |
| Socket file already exists but no server running | Remove stale socket file and proceed with startup. |

### 7.2 Client errors

| Condition | Behavior |
|-----------|----------|
| Server not running for `stop` | Print "devlog server is not running" and exit 0. |
| Not in a git repo when required | Print "Error: not in a git repository" to stderr. Exit 1. |
| Invalid date format | Print "Error: invalid date format, expected YYYY-MM-DD" to stderr. Exit 1. |
| `gen_cmd` not on PATH for `gen` | Print error with instructions. Exit 1. |
| `comp_cmd` not on PATH for `gen` | Print "Compressor command '<cmd>' not found on $PATH." Exit 1. |
| AI summarizer returns non-zero | Print command's stderr. Exit 1. Do not write partial summary. |

## 8. Systemd integration

The project should include a systemd user service file:

```ini
# devlog.service — install to ~/.config/systemd/user/
[Unit]
Description=Devlog server

[Service]
Type=simple
ExecStart=/path/to/devlog start
Restart=on-failure
RestartSec=5

[Install]
WantedBy=default.target
```

The actual `ExecStart` path depends on where the user installs the binary.
This file is provided as a reference template; installation is left to the
user (or to a NixOS module in the future).

## 9. Project setup

### 9.1 Go module

- Module path: to be determined (e.g., `github.com/chadnorvell/devlog`)
- Minimum Go version: 1.22
- Use the standard library where possible. Minimize third-party dependencies.
- Recommended libraries:
  - `github.com/BurntSushi/toml` for config parsing
  - `github.com/godbus/dbus/v5` for D-Bus integration (KRunner)
  - Standard `encoding/json` for IPC
  - Standard `net` for Unix sockets
  - Standard `os/exec` for invoking `git`, the AI summarizer, and KDialog

### 9.2 Code structure

```
devlog/
├── main.go                # Entry point: parse top-level subcommand, dispatch
├── cmd.go                 # CLI command implementations (note, gen, watch, etc.)
├── server.go              # Server: socket listener, snapshot ticker, shutdown
├── snapshot.go            # Git diff snapshot logic (shadow index, dedup)
├── config.go              # Config file and path resolution (XDG dirs)
├── state.go               # state.json read/write
├── ipc.go                 # IPC request/response types and client helper
├── generate.go            # Summary generation: summarizer invocation, prompt assembly
├── claudecode.go          # Claude Code session log parsing and preprocessing
├── krunner.go             # D-Bus KRunner integration (optional)
├── org.devlog.krunner.desktop  # KRunner plugin descriptor (install to dbusplugins/)
├── flake.nix
├── go.mod
├── go.sum
└── devlog.service         # Systemd unit template
```

**CLI parsing**: Use Go's standard `flag` package for per-command flags (e.g.,
`-m`, `--name`). Use manual dispatch in `main.go` based on `os.Args[1]`:

```go
func main() {
    if len(os.Args) < 2 {
        // No subcommand: log a note (open editor)
        cmdNote()
        return
    }
    switch os.Args[1] {
    case "gen":
        cmdGen()
    case "gen-prompt":
        cmdGenPrompt()
    case "watch":
        cmdWatch()
    case "unwatch":
        cmdUnwatch()
    case "start":
        cmdStart()
    case "stop":
        cmdStop()
    case "status":
        cmdStatus()
    default:
        // Not a known subcommand: treat as note command
        // (handles `devlog -m "msg"`)
        cmdNote()
    }
}
```

This dispatch logic means `devlog -m "msg"` hits the `default` case (since
`-m` is not a subcommand), which calls `cmdNote()` and parses `-m` with its
own `flag.FlagSet`. Keep all files in the `main` package — there's no need for
`internal/` sub-packages at this scale.

### 9.3 Nix flake

The flake should provide:

- A dev shell with Go, git, and an AI CLI (like `claude` or `gemini-cli`)
- A package that builds the `devlog` binary

### 9.4 Build and install

```bash
go build -o devlog .
```

### 9.5 Testing

The project should include tests for the components that are most likely to
have subtle bugs:

- **Snapshot logic** (`snapshot_test.go`): Test the shadow index technique by
  creating a temporary git repo, staging files, running the snapshot function,
  and verifying the output diff and deduplication behavior. Mock the git
  commands if needed, but prefer real git operations in a temp dir for
  accuracy.

- **State persistence** (`state_test.go`): Test read/write of `state.json`,
  including atomic write (crash safety), handling of missing files, and
  correct serialization of name overrides.

- **IPC protocol** (`ipc_test.go`): Test that request/response JSON
  serialization round-trips correctly. Test client behavior when the server is
  not running (connection refused).

- **Config resolution** (`config_test.go`): Test the XDG path resolution
  precedence (env var > default) and config file parsing with missing/empty
  files.

- **Note logging** (`cmd_test.go`): Test project name resolution from
  `state.json` with and without `--name` overrides.

- **Summary generation** (`generate_test.go`): Test prompt assembly with
  various combinations of present/absent raw data files, including terminal
  logs and Claude Code sessions. Mock the AI summarizer command (e.g., with a
  shell script that echoes a canned response) to test the end-to-end flow
  without making real AI calls.

- **Claude Code preprocessing** (`claudecode_test.go`): Test JSONL parsing
  with synthetic session files. Test date filtering (entries spanning multiple
  dates, UTC-to-local conversion). Test extraction of user messages, assistant
  text, and tool use summaries. Test that thinking blocks, file-history
  snapshots, and progress entries are excluded. Test the project path to
  directory name mapping.

Use `t.TempDir()` for all tests that touch the filesystem. Tests should not
depend on any external state or services.
