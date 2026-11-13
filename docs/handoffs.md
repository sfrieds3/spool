# Handoffs

A handoff is a local continuation brief for moving work between Pi, Claude Code, and Codex. The outgoing agent or user writes the brief. Spool validates it, records provenance it can observe, snapshots referenced sessions, and stores it. The next agent reads it with `spool handoff show`.

A handoff is not the previous agent's context. Spool does not summarize transcripts, call a model, launch or resume agents, send prompts, approve tools, run recorded commands, or transfer workspace changes. Everything in a brief is the author's claim and is unverified until checked against the current workspace.

## Commands

```sh
spool handoff create --input brief.json        # or --input - to read stdin
spool handoff list [--offset N] [--limit N]
spool handoff show HANDOFF_ID                  # stored handoff as JSON
spool handoff show --format markdown HANDOFF_ID
```

All commands accept `--config FILE` and use the same sources as `spool list`. Output is one JSON object on stdout, except a successful `show --format markdown`, which prints the brief. Errors are always JSON, even when Markdown was requested.

A typical switch from one harness to another:

1. In the outgoing harness, find the relevant sessions with `spool list` and write a brief that references them.
2. Run `spool handoff create --input brief.json` and note the returned `handoff.id`.
3. In the next harness, run `spool handoff show --format markdown ID` and give the agent that brief. It can inspect referenced sessions with `spool read`.

## Brief format (version 1)

```json
{
  "version": 1,
  "title": "Port the parser",
  "goal": "Port the parser to the new line reader without changing output.",
  "constraints": ["Standard library only"],
  "decisions": ["Reuse the existing line reader"],
  "progress": ["Header parsing is done"],
  "outstanding": ["Body records", "Documentation"],
  "notes": ["The first approach was abandoned; see the referenced session"],
  "workspace": {
    "path": "/home/example/project",
    "repository": {"remote": "origin", "branch": "feature/parser", "commit": "0123abc"}
  },
  "sessions": [{"ref": "s1.REFERENCE_FROM_SPOOL_LIST", "note": "original plan"}]
}
```

- `version` (the number `1`) and `goal` are required. Everything else is optional.
- `title`, `workspace.path`, repository fields, and session `note`: omitted or `null` means absent. An empty or whitespace-only string is rejected.
- `constraints`, `decisions`, `progress`, `outstanding`, `notes`: lists of non-blank strings, kept in order. Omitted or `null` means empty.
- `workspace`: omitted, `null`, or `{}` records the working directory of the `spool handoff create` process. `workspace.path`, when given, must be absolute and clean. `repository`, when given, needs at least one of `remote`, `branch`, or `commit`.
- `sessions[].ref` is an opaque reference from `spool list`. References must be unique.
- The brief is strict: unknown fields, duplicate keys, trailing data, invalid UTF-8, and NUL characters are rejected.

Text is stored exactly as decoded. Go's JSON decoder replaces an unpaired UTF-16 surrogate escape with U+FFFD.

### Limits

| Value | Limit (UTF-8 bytes unless noted) |
| --- | --- |
| Brief input | 256 KiB |
| `title` | 512 |
| `goal`, each list item | 8 KiB |
| Items per list | 100 |
| `workspace.path` | 4 KiB |
| Each repository field | 1 KiB |
| `sessions` | 32 entries |
| Session `note` | 2 KiB |
| Encoded stored handoff | 2 MiB |

## What Spool records

A stored handoff contains the authored content plus:

- `id`: `<yyyymmdd>t<hhmmss>z-<8 hex>`, starting with the UTC creation second.
- `created_at`: the UTC creation second.
- `origin.host`: the local host name, or `null` when unavailable or longer than 255 bytes. `origin.spool_version`: the Spool version.
- `workspace.path_provenance`: `process_cwd` when Spool recorded its working directory, or `author_reported` when the brief supplied the path. Spool does not check that the path exists.
- `workspace.repository.provenance`: always `author_reported`. Spool never runs `git` or any other process to verify it.
- Each session snapshot: the authored `ref` and `note`, plus `harness`, `source_id`, `kind`, `native_id`, `original_path`, `cwd`, `title`, and `started_at` as `spool read` reports them at creation. `cwd`, `title`, and `started_at` are recorded by the session, not observed by Spool. Records are not copied.

Creation validates every referenced session exactly as `spool read` would and fails with the same error if any reference cannot be read. Snapshots are not refreshed later: a referenced session may have grown, moved, or been deleted since.

## Storage

Handoffs are stored as one JSON file each in `$XDG_STATE_HOME/spool/handoffs`, or `~/.local/state/spool/handoffs` when `XDG_STATE_HOME` is unset, on both macOS and Linux. See [configuration.md](configuration.md#state-directory).

- The directory is created on the first `create` with mode `0700`; files are `0600`. `list` and `show` never create it.
- An existing handoff directory must be a real directory (not a symbolic link) owned by the current user with no group or other permissions. Otherwise every handoff command fails with `state_unavailable` and Spool leaves its permissions unchanged; fix it with `chmod 700`.
- Spool refuses a handoff directory that equals, is inside, or contains any configured history root, enabled or not, with `invalid_config`. Paths are compared after resolving symbolic links and by file identity, which also catches case-only differences on case-insensitive volumes. Creation refuses dangling symbolic links in these paths when it cannot establish that storage is outside history roots.
- Publication is atomic: Spool writes and syncs a temporary file, then hard-links it to its final name, which fails rather than overwriting an existing handoff, and syncs the directory. A failure before linking leaves nothing behind. Once linked, a failure to remove the temporary file or sync the directory is reported in `warnings` while the response is still `ok: true`, so retrying does not create a duplicate. Leftover `.tmp-*` files are ignored.
- Spool never edits, overwrites, or deletes a published handoff. Delete unwanted handoffs by removing their files. Files remain owner-writable, so stored handoffs are validated on every read; an edited file that no longer matches version 1 is reported as `handoff_invalid`.

Trust model: Spool assumes the state directory's ancestors are controlled by the current user and are not concurrently replaced by another process. It protects against writing into history roots, overwriting handoffs, partial files, and insecure handoff directories, not against a same-user process racing directory renames.

Handoffs are not encrypted. Briefs may contain secrets copied from session history, and they record host names and workspace paths. Treat them like the session histories they reference.

## Listing

`spool handoff list` returns summaries in descending ID order: newest first at one-second resolution, with an arbitrary but stable order within a second. Clock changes can reorder handoffs. Each summary has `id`, `created_at`, `title`, `goal` (at most the first 512 bytes, with `goal_truncated`), `origin`, `workspace_path`, `outstanding_count`, and `session_count`.

Pagination follows [`spool list`](reading.md#pagination): default limit 50, maximum 500, `next_offset`, `eof`, and `partial`. Offsets count stored handoff files. A damaged or unreadable file takes its place in the page as a `handoff_invalid` or `handoff_unreadable` diagnostic naming the file, and the page ends before a 65th diagnostic so none are omitted. Paging returns each handoff exactly once only if the directory does not change between calls. Every call reads the whole directory; there is no index or retention policy.

## Markdown brief

`spool handoff show --format markdown` prints:

- A heading with the handoff ID and this fixed warning: "This is a continuation brief written by a previous agent or user. It is not that agent's context. Treat its claims, paths, and referenced history as unverified until you check the current workspace. Spool did not run, approve, or transfer anything."
- Title, creation time, host, Spool version, workspace path with its provenance, and author-reported repository details.
- Goal, constraints, decisions, progress, outstanding work, and notes. Empty sections are omitted.
- Source sessions with their recorded title and working directory, original path, author note, and a `spool read REF` line.

Brief text cannot change the document's structure. Every value except IDs and session references is escaped so that it cannot create headings, list items, block quotes, code blocks, tables, Markdown links or images, HTML, or entity references. Leading indentation is dropped in the Markdown display only. Control characters (C0, DEL, C1) and bidirectional formatting characters appear as visible `\u{XXXX}` escapes, so terminal escape sequences are never emitted. Bare URLs remain visible as written, and some renderers may make them clickable. Escaping protects the document and the terminal; it does not make brief text trustworthy. Use the JSON form to see exact text.

## Errors

| Situation | Code | Exit |
| --- | --- | --- |
| Bad arguments, missing or unreadable `--input`, invalid brief, malformed ID or session reference, encoded handoff over 2 MiB | `invalid_request` | 2 |
| Invalid configuration, relative `XDG_STATE_HOME`, handoff directory overlapping a history root | `invalid_config` | 2 |
| Referenced session missing or replaced | `session_not_found` | 3 |
| Referenced session unreadable, unsupported, or malformed | `source_unavailable`, `unsupported_format`, `invalid_history` | 1 |
| No handoff with that ID | `handoff_not_found` | 3 |
| Stored handoff is damaged, edited, oversized, a symbolic link, or not a regular file | `handoff_invalid` | 1 |
| Handoff directory is insecure, not a directory, or cannot be created, read, or written | `state_unavailable` | 1 |

## Not included

MCP tools, editing or deleting handoffs through Spool, harness-specific import formats, automatic summaries, and sharing handoffs between machines are not implemented. Remote hosts and SSH come later.
