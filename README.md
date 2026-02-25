# devlog

Devlog automatically generates summaries of your daily work on software
engineering projects. It continuously collects git diffs and manual notes,
then uses an AI summarizer to produce concise daily summaries in Markdown.

See [DESIGN.md](DESIGN.md) for the full specification.

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
