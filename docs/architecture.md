# Architecture: local session access

This document describes the internal boundaries behind `spool list`, `spool read`, and `spool handoff`. It covers only local data; SSH and MCP access are not implemented and have no interfaces yet.

## Packages

- [`internal/config`](../internal/config) loads the strict version-1 runtime configuration and resolves built-in roots from an injectable environment. See [configuration.md](configuration.md).
- [`internal/harness`](../internal/harness) contains one adapter per harness. An adapter interprets individual native JSON lines: it identifies session identity, record type, native ID/parent/timestamp fields, native titles, and markers for history stored outside the file. Adapters never open files, follow references, or execute content.
- [`internal/jsonl`](../internal/jsonl) reads physical lines incrementally and discards bytes of lines larger than the line limit without buffering them.
- [`internal/history`](../internal/history) is the transport-independent service. It owns source roots, confined file access, discovery, references, pagination, diagnostics, and response types. `Service.List` and `Service.Read` take a context and typed requests and return response values or a typed `*history.Error`. `Service.Describe` validates a reference exactly as `Read` does and returns only its session metadata.
- [`internal/handoff`](../internal/handoff) is the transport-independent handoff service: brief validation, session snapshots through `history.Service.Describe`, confined storage in the state directory, stored-data validation, and Markdown rendering. See [handoffs.md](handoffs.md).
- [`internal/strictjson`](../internal/strictjson) and [`internal/safefile`](../internal/safefile) hold strict JSON decoding and confined regular-file opening shared by configuration, history, and handoffs.
- [`cmd/spool`](../cmd/spool) parses arguments, calls the service, and writes one JSON object to stdout. Future interfaces should call the same service rather than parse histories themselves.

## Source adapter boundary

An adapter implements `harness.Adapter`:

- `Candidate(rel)` selects files during discovery by their source-relative slash path.
- `Kind(rel)` labels a session file, for example `session` or Claude's `subagent`.
- `FixedHeader()` states whether line 1 must establish identity (Pi, Codex) or whether the first record carrying identity does (Claude).
- `Parse(line, raw)` returns an `Entry` for one valid JSON object, or a `*harness.FormatError` (`unsupported_format` or `invalid_history`) that is fatal for the file. An entry with `Unsupported` set marks a dependency on history outside the file; the service then rejects the whole file rather than return a prefix.

Adding a harness or format means adding an adapter, synthetic fixtures, tests, and a row in [formats.md](formats.md).

## References

A session reference is `s1.` followed by unpadded base64url of the canonical JSON array `[source_id, harness, path, native_id]`, where `path` is relative to the source root. References are opaque locators, not credentials. Decoding rejects other versions, non-canonical encodings, unknown harnesses, invalid source IDs, absolute paths, `.`/`..` segments, backslashes, NUL, empty or oversized native IDs, and strings over 12 KiB.

A reference resolves only through a configured, enabled source with the same harness. Reading reopens the file and checks that it still records the same native ID, so a replaced file is reported as `session_not_found` rather than read as the old conversation. References are stable while source configuration and file locations are unchanged; renaming a source ID or moving a file changes them. Two files with the same native ID in different locations always have different references. References carry no host identity yet.

## Confinement

Each source root is opened once with `os.OpenRoot`; a root that is itself a symbolic link is resolved at that point and the opened directory is the boundary. Discovery walks directory entries without following nested symbolic links and reports them as `symlink_skipped`. Opening a file checks every path component with `Lstat`, rejects symbolic links and non-regular files, opens through the root with `O_NONBLOCK` on Unix, and verifies the opened file is the inspected regular file. Spool never writes to history roots, creates caches, changes permissions, or launches processes.

## Handoff write path

`spool handoff create` is Spool's only write. It writes only inside the handoff directory under the state directory, which must not overlap any configured history root and must be a user-owned `0700` directory. Files are published with an exclusive temporary file, fsync, and a hard link that never overwrites. Details are in [handoffs.md](handoffs.md#storage).

## Responses and errors

Every `list`, `read`, or `handoff` invocation writes exactly one JSON object (except a successful `handoff show --format markdown`) and a newline to stdout. Successful responses have `version: 1` and `ok: true`; failures have `version: 1`, `ok: false`, and `error: {code, message}`. Stderr is used only if stdout cannot be written and never contains transcript content.

| Error code | Exit | Meaning |
| --- | --- | --- |
| `invalid_request` | 2 | Bad arguments, filter, pagination, or reference; disabled or unconfigured source. |
| `invalid_config` | 2 | The configuration, environment-selected roots, or state directory are invalid, or the handoff directory overlaps a history root. |
| `session_not_found` | 3 | The referenced file no longer exists or records a different session. |
| `sources_unavailable` | 1 | Every requested source root failed. Includes per-source status. |
| `source_unavailable` | 1 | The referenced session's source or file cannot be read. |
| `unsupported_format` | 1 | The session uses an unsupported format or depends on external history. |
| `invalid_history` | 1 | The required header or identity record is malformed. |
| `handoff_not_found` | 3 | No stored handoff has the requested ID. |
| `handoff_invalid` | 1 | A stored handoff file is damaged, edited, oversized, or not a regular file. |
| `state_unavailable` | 1 | The handoff directory is insecure, not a directory, or cannot be created, read, or written. |
| `canceled`, `internal_error` | 1 | Interrupted or unexpected failure. |

Successful responses may be partial. Source states are `ready`, `absent` (a built-in default root that does not exist), and `error`. Diagnostics carry `code`, `message`, `source_id`, `path`, and `line` (null when not applicable). At most 64 diagnostics are returned per response; `omitted_diagnostics` counts the rest. See [reading.md](reading.md) for pagination semantics.
