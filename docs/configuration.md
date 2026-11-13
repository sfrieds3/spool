# Configuration

Spool reads local agent history from configured source roots. Configuration is optional: without a configuration file Spool uses the native default roots of the host it runs on.

## Location

- `spool list --config /path/to/config.json` and `spool read --config ...` load exactly that file. The file must exist; default lookup is skipped.
- Without `--config`, Spool loads `$XDG_CONFIG_HOME/spool/config.json`, or `~/.config/spool/config.json` when `XDG_CONFIG_HOME` is unset. This is the same on macOS and Linux. `XDG_CONFIG_HOME` must be absolute when set.
- When the default file does not exist, Spool uses the built-in sources below. Spool never creates a configuration file.

This is Spool's runtime configuration. It is unrelated to the development `mise.toml`.

## State directory

Spool stores its own data, currently only [handoffs](handoffs.md), under `$XDG_STATE_HOME/spool`, or `~/.local/state/spool` when `XDG_STATE_HOME` is unset. This is the same on macOS and Linux. `XDG_STATE_HOME` must be absolute when set; a relative value is `invalid_config`. The state directory is created only when a handoff is first created, and is never inside a configured history root.

## Format

Configuration is strict JSON with schema version 1:

```json
{
  "version": 1,
  "sources": [
    {"id": "pi", "harness": "pi", "root": "/home/example/.pi/agent/sessions"},
    {"id": "claude", "harness": "claude", "root": "/home/example/.claude/projects", "enabled": true},
    {"id": "codex", "harness": "codex", "root": "/home/example/.codex/sessions"},
    {"id": "codex-archive", "harness": "codex", "root": "/home/example/.codex/archived_sessions", "enabled": false}
  ]
}
```

- `version` is required and must be `1`.
- `sources`, when present, is the complete source selection. It replaces the built-in sources rather than merging with them. `"sources": []` intentionally selects no sources. Omitting `sources` selects the built-in sources.
- Each source has a unique `id` (lowercase letters, digits, `-`, `_`; at most 64 characters, starting with a letter or digit), a `harness` of `pi`, `claude`, or `codex`, and an absolute `root`. `enabled` defaults to `true`. Several sources may use the same harness.
- Unknown fields, duplicate keys, trailing data, unsupported versions or harnesses, duplicate source IDs, and relative roots are rejected with `invalid_config` before any history is read.
- A configured root that does not exist or cannot be read is a source error. A missing built-in default root only means that harness has not been used on this host.

Remote hosts are not supported yet. Spool reads only local roots and never opens SSH or network connections; host configuration will be added by a later change.

## Built-in sources

Built-in roots are resolved from the environment of the process running Spool. Environment paths must be absolute; a relative value is an `invalid_config` error. Spool does not parse harness settings files or infer another process's command-line options. Use an explicit configuration for nonstandard session directories.

| Source ID | Harness | Root |
| --- | --- | --- |
| `pi` | `pi` | `$PI_CODING_AGENT_SESSION_DIR`, else `$PI_CODING_AGENT_DIR/sessions`, else `~/.pi/agent/sessions` |
| `claude` | `claude` | `$CLAUDE_CONFIG_DIR/projects`, else `~/.claude/projects` |
| `codex` | `codex` | `$CODEX_HOME/sessions`, else `~/.codex/sessions` |
| `codex-archive` | `codex` | `$CODEX_HOME/archived_sessions`, else `~/.codex/archived_sessions` |
