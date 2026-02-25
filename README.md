# devlog

Devlog automatically generates summaries of your daily work on software
engineering projects. It collects data from multiple sources and uses an AI
summarizer to produce concise daily summaries in Markdown.

See [DESIGN.md](DESIGN.md) for the full specification.

## Data sources

Devlog draws from four data sources when generating summaries. Not all sources
require watching a project, but watched projects get the most complete coverage.

| Data source          | How it works                                                       | Requires watching? |
|----------------------|--------------------------------------------------------------------|--------------------|
| **Git diffs**        | Server snapshots uncommitted changes every 5 minutes               | Yes                |
| **Manual notes**     | `devlog -m "msg"` or editor-based note entry                       | No                 |
| **Terminal logs**    | Recorded externally (e.g., `script`), placed in the raw data dir   | No\*               |
| **Claude Code sessions** | Read directly from `~/.claude/projects/` at generation time   | Yes                |

\*Terminal logs are included for any project that appears in the summary, but
they alone don't cause a project to appear. A project must also have git diffs,
notes, or Claude Code sessions to be discovered.

In general, important projects should be watched (`devlog watch`). Unwatched
projects can still appear in summaries if they have manual notes, but coverage
is best-effort.

## KRunner integration

The devlog server optionally registers on the D-Bus session bus as a KRunner
plugin, allowing you to log notes from anywhere on your desktop by typing
`#project note text` into KRunner.

### Setup

1. Copy the plugin descriptor to the KRunner dbusplugins directory:

   ```sh
   mkdir -p ~/.local/share/krunner/dbusplugins
   cp org.devlog.krunner.desktop ~/.local/share/krunner/dbusplugins/
   ```

2. Restart KRunner so it picks up the new plugin:

   ```sh
   kquitapp6 krunner && kstart krunner
   ```

3. Enable the plugin in KRunner settings (System Settings > Search > KRunner).

### Prioritizing devlog results

By default, KRunner sorts results from "favorite" runners (like the
application launcher) above other runners. To prioritize devlog results when
typing `#`, add `devlog` to the favorites list in `~/.config/krunnerrc`:

```ini
[Plugins]
Favorites=devlog,krunner_services,krunner_sessions,krunner_powerdevil,krunner_systemsettings
```

### Usage

- `#project note text` — logs "note text" to the project immediately
- `#project` (with no text) — opens a KDialog for longer multi-line input
- Partial project names autocomplete against watched projects
- Unwatched project names are accepted too, appearing as lower-priority matches
