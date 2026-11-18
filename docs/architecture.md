# Architecture: local session access

This document describes the internal boundaries behind `spool list`, `spool read`, `spool search`, `spool handoff`, and `spool pick`. It covers only local data; SSH and MCP access are not implemented and have no interfaces yet.

## Packages

- [`internal/config`](../internal/config) loads the strict version-1 runtime configuration and resolves built-in roots from an injectable environment. See [configuration.md](configuration.md).
- [`internal/harness`](../internal/harness) contains one adapter per harness. An adapter interprets individual native JSON lines: it identifies session identity, record type, native ID/parent/timestamp fields, native titles, and markers for history stored outside the file. Pure `SearchText` projects only allowlisted decoded text leaves (see [search.md](search.md)); adapters never open files, follow references, or execute content.
- [`internal/jsonl`](../internal/jsonl) reads physical lines incrementally and discards bytes of lines larger than the line limit without buffering them.
- [`internal/history`](../internal/history) is the transport-independent service. It owns source roots, confined file access, discovery, references, pagination, diagnostics, and response types. `Service.List`, `Service.Read`, and `Service.Search` take a context and typed requests and return response values or a typed `*history.Error`. Search alone validates each captured file extent on its confined open handle before releasing provisional hits; neither List nor Read pagination changes. `Service.Describe` validates a reference exactly as `Read` does and returns only its session metadata.
- [`internal/handoff`](../internal/handoff) is the transport-independent handoff service: brief validation, session snapshots through `history.Service.Describe`, confined storage in the state directory, stored-data validation, and Markdown rendering. See [handoffs.md](handoffs.md).
- [`internal/strictjson`](../internal/strictjson) and [`internal/safefile`](../internal/safefile) hold strict JSON decoding and confined regular-file opening shared by configuration, history, and handoffs.
- [`internal/continuation`](../internal/continuation) composes shared history and handoff interfaces for scope preparation, explicit disclosure, constrained Pi drafting, exact-byte review approval, immutable publication, and separately confirmed typed resume/launch plans. It never derives executables or arguments from historical text.
- [`cmd/spool`](../cmd/spool) parses arguments, calls services, and owns terminal probes, fzf, prompts, and editor UI. Data commands write one JSON object; interactive `pick` uses stderr except `--ref` and explicitly authorized child terminal streams. Future interfaces should call the same services rather than parse histories themselves.

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

Each source root is opened once with `os.OpenRoot`; a root that is itself a symbolic link is resolved at that point and the opened directory is the boundary. Discovery walks directory entries without following nested symbolic links and reports them as `symlink_skipped`. Opening a file checks every path component with `Lstat`, rejects symbolic links and non-regular files, opens through the root with `O_NONBLOCK` on Unix, and verifies the opened file is the inspected regular file. History reads never write to history roots, create caches, change permissions, or launch processes. Separately confirmed native Pi resume can let Pi append to its own session; fresh receiving harnesses have their normal startup behavior. Search adds no native file mutations, persistence, network access, or subprocesses; it returns unredacted sensitive snippets that must remain untrusted historical data.

## Handoff write path

`spool handoff create` is the immutable publication boundary; approved continuation uses the same service. Continuation also uses private temporary draft/brief files outside every configured history root, removed on ordinary completion. It writes only inside the handoff directory under the state directory, which must not overlap any configured history root and must be a user-owned `0700` directory. Files are published with an exclusive temporary file, fsync, and a hard link that never overwrites. Details are in [handoffs.md](handoffs.md#storage).

## Responses and errors

Every `list`, `read`, `search`, or `handoff` invocation writes exactly one JSON object (except a successful `handoff show --format markdown`) and a newline to stdout. Successful responses have `version: 1` and `ok: true`; failures have `version: 1`, `ok: false`, and `error: {code, message}`. For these data commands, stderr is used only if stdout cannot be written and never contains transcript content. `pick` has a distinct stderr UI contract, including explicitly requested bounded inspection and safe draft review; see [continuation.md](continuation.md).

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

Successful responses may be partial. Source states are `ready`, `absent` (a built-in default root that does not exist), and `error`. Diagnostics carry `code`, `message`, `source_id`, `path`, and `line` (null when not applicable). At most 64 diagnostics are returned per response; `omitted_diagnostics` counts the rest. See [reading.md](reading.md) for existing List/Read pagination and [search.md](search.md) for search's separate canonical request-bound row/line cursor, captured-extent checks, response and native byte limits, diagnostic delivery, non-snapshot semantics, and the fact that existing tree discovery is not itself bounded by those native-byte/row scan limits.


## Continuation trust transitions

The shared service exposes `Prepare`, `Generate`, `Draft.Replace/Approve`, `Publish`, `ResolveResume/Resume`, and `PlanLaunch/Launch`. Prepared input and draft bytes are private; accessors return copies. Disclosure binds a particular immutable source/scope/target/goal/provider/model; review approval binds exact validated final bytes and that scope. Editor changes invalidate old approval. Neither token is inferred from a file's existence. Sources are revalidated using Describe plus confined regular-file identity and a cancelable hash over the initially observed file size, detecting growth or replacement before publication/resume. Pages are not a snapshot; concurrent writes can still cause incomplete input or fail publication. Native resume retains a residual pathname race before Pi opens the original file.

The model returns only content fields. Spool attaches and pins source references, confirmed workspace, and drafting/scope caveats; strict version-1 parsing is shared with authored create. Publication is one-way and post-link cleanup warnings remain successful publication. Only a saved approved handoff can produce a fresh-session launch plan, bound to target/executable/confirmed cwd/brief. CLI/editor/fzf interaction stays outside shared application orchestration. All integrations are optional inherited-PATH direct executables; the installed integration/provider is trusted code, not a sandbox. No new MCP or remote endpoint is added.
