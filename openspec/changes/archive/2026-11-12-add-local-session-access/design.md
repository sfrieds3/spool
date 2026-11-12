# Design

## Context

The repository has `cmd/spool/main.go`, table-driven CLI tests, a dependency-free Go module, and Mise build/test/vet tasks. There is no application service, source configuration, parser, or transport to preserve. Existing help/version behavior is the only compatibility surface. See `proposal.md` for motivation and the two delta specs for behavior contracts.

Planning inspected the project README, instructions, Go entry point/tests, toolchain configuration, Pi's packaged session-format documentation, and locally installed Claude Code 2.1.311 and Codex 0.164.0 executable/package evidence. No personal transcripts or Agentmux/dotfiles implementation were consulted. Pi v3 is documented as JSONL with a session header and an entry tree. Claude's packaged writer stores project JSONL and nested subagent JSONL with parent UUIDs; it does not ship a complete stable transcript schema. Codex's packaged evidence identifies active/archive roots, `session_meta`, `response_item`, `event_msg`, `turn_context`, and newer composite/paginated histories. This evidence supports a bounded native-record reader, not a complete conversation replay engine.

## Goals / Non-Goals

**Goals:** Keep source parsing behind one transport-independent local service; preserve native information; make missing and damaged data visible; establish deterministic, testable JSON CLI responses without runtime dependencies on the harnesses.

**Non-Goals:** No model calls, semantic extraction, active-branch projection, exact prompt reconstruction, external attachment reads, network access, remote protocol, or persistent database. This does not make sessions automatically portable between harnesses. It supplies the records from which later recall/handoff behavior can be built.

## Decisions

### 1. Shared local service, thin CLI

Introduce small internal packages for configuration, session models/service, and individual harness adapters. The service accepts contexts and typed list/read requests; the CLI handles arguments and serializes responses. Do not introduce a generic plugin framework, MCP dependency, or speculative transport layer. Later MCP and SSH endpoints can call this service directly. This avoids implementing parsers separately in each interface while keeping the first change small enough to test independently.

### 2. Explicit JSON configuration using the standard library

Use schema version 1 and the default `${XDG_CONFIG_HOME}/spool/config.json`, falling back to `~/.config/spool/config.json` on both macOS and Linux. `--config` selects an explicit file and suppresses default-file lookup. Require an absolute `XDG_CONFIG_HOME` when set. An absent implicit default config uses built-in sources; an absent explicit file is an error. Parse configuration strictly, including duplicate JSON keys, trailing values, unknown fields, unsupported versions, duplicate source IDs, unsupported harnesses, and relative root paths.

Example:

```json
{
  "version": 1,
  "sources": [
    {"id": "pi", "harness": "pi", "root": "/home/example/.pi/agent/sessions", "enabled": true},
    {"id": "claude", "harness": "claude", "root": "/home/example/.claude/projects", "enabled": true}
  ]
}
```

An explicit `sources` array is the complete source selection, not a merge with implicit sources. Each source has a unique restricted identifier, a harness, one absolute root, and an `enabled` field defaulting to true. Multiple sources can use the same harness. `sources: []` means intentionally empty, not all defaults. Source roots explicitly configured but missing are failures. No host entries are accepted yet; a later SSH change will add and specify them deliberately. JSON rather than TOML keeps this first implementation dependency-free; this is Spool's runtime config, separate from the development `mise.toml`.

Built-in source roots are Pi's sessions directory beneath `PI_CODING_AGENT_DIR` or `~/.pi/agent` (with `PI_CODING_AGENT_SESSION_DIR` taking precedence), Claude's projects directory beneath `CLAUDE_CONFIG_DIR` or `~/.claude`, and both `sessions` and `archived_sessions` beneath `CODEX_HOME` or `~/.codex`. Use separate stable IDs for Codex active/archive sources. Resolve these defaults in the executing host's environment. Environment-selected paths must be absolute; invalid values fail configuration. Spool does not infer another running process's CLI options or parse harness settings files. Explicit roots handle nonstandard session directories.

### 3. Supported native layouts, not a universal import promise

Discover regular `.jsonl` candidates recursively within the selected root without guessing cwd from encoded directory names. Pi supports documented v3 session headers and preserves all native records. Legacy Pi v1/v2 is explicitly unsupported initially, with no migration. Claude supports independently readable project and nested subagent JSONL, identifies sessions from recorded `sessionId`, and preserves parent UUIDs and sidechain metadata. Codex supports independently readable rollout JSONL with `session_meta.payload.id`, in both active and archived roots. Discovery reads bounded identifying metadata rather than rendering prompt bodies into titles.

For Claude/Codex, add a documented compatibility matrix tied to synthetic fixtures and observed record structures rather than claiming compatibility with every release sharing a version string. New record types inside an otherwise supported envelope remain raw native records. Files with composite/paginated/inherited-history markers that require unavailable external records are diagnosed as `unsupported_format`; do not return a prefix as a complete standalone session. This slice does not query SQLite, migrate rollouts, follow fork-context files, or materialize subagent inherited prefixes. Candidate failures appear in list diagnostics; directly reading an identified unsupported file fails explicitly. Additional formats require later adapter tests and changes.

Metadata comes from native fields. Use filesystem modification time only as an explicitly labeled fallback; never use it as a claim of last agent activity. Unknown title/cwd values are null. No prompt-derived titles. Listing metadata extraction uses a documented per-file byte budget and reports unavailable metadata rather than silently assuming the rest of a large file is empty. Preserve original working directories even when deleted or unreachable.

### 4. Stable local references with confined resolution

Represent references as a version-prefixed base64url encoding of a strictly validated tuple: source ID, harness, source-relative file path, and observed native identity. This is an opaque locator, not authentication or a secret. Reject traversal, absolute paths, separators incompatible with the local platform, unconfigured/disabled sources, invalid encodings, and oversized reference strings. A subsequent read checks the file's current native identity so a replaced file is not silently confused with an older conversation. Missing files or changed identities return `session_not_found`.

Distinct relative files retain distinct references even if their native IDs collide. Reference stability is scoped to unchanged source configuration and file location; moving files or renaming source IDs changes references. Reserve host namespacing for the future aggregator rather than pretending the OS hostname is a durable identity now.

Use Go's root-confined filesystem operations (`os.OpenRoot` and `os.Root`) for actual opens, alongside regular-file checks and discovery that skips nested symlinks. A user-selected root may itself be a symlink; resolve/select it once and treat the opened root as the boundary. Do not rely on a string prefix check or resolve-then-open alone, which permits traversal races. Do not interpret a session reference as an arbitrary path.

### 5. Native records with minimal normalized metadata

Each read returns session metadata plus records containing the physical 1-based line number, extracted native type/ID/parent/timestamp where available, and the full parsed native JSON object. Preserve unknown fields and number precision with `json.RawMessage` or `json.Number`; do not decode everything into floating-point numbers. The top-level response labels content as `untrusted_history` and the view as `native_file_order`. Preserve headers, branch summaries, compaction entries, tool calls/results, and system/custom records. These records are data, not instructions.

Reject reconstructing an active conversation in this slice. Pi's tree/compaction model and Claude's parent chains would make simple concatenation incorrect. Normalized text views and branch selection can be added later without losing the preserved raw information.

### 6. Bounded pagination without hidden snapshots

Both commands accept `--offset` (default 0) and `--limit` (default 50, maximum 500). List offsets count catalog rows sorted by source ID then source-relative path, bytewise. Read offsets count physical lines consumed, including skipped malformed or oversized lines; `next_offset` is directly reusable, while record locations are 1-based. This makes progress and diagnostics unambiguous without a persisted cursor store.

Limit a native line to 4 MiB and each serialized response to 8 MiB. A page ends before a valid record that would exceed the response budget, leaving that record for the next offset. A record that cannot fit by itself, including envelope overhead, is explicitly omitted with its line diagnostic and advances the offset. Do not silently truncate JSON or strings. Bound diagnostics and report a count of additional omitted diagnostics so error-heavy files cannot bypass the response cap. Listing stops before its response budget and supplies a continuation offset; oversized metadata receives a bounded diagnostic instead of invalid JSON. All pages include `next_offset` or null, `eof`, `partial`, and diagnostics.

Scan line data incrementally, discarding oversized line bytes without buffering the complete line. A final unterminated line is withheld, diagnosed as `incomplete_tail`, and retried at its starting offset; report `eof: false` even if no more bytes were present at observation. Never spin or wait for writers. A malformed required header/identity is fatal; a bad complete body line is skipped with a diagnostic and partial result. A normal EOF has `eof: true` and null continuation. Callers must not auto-loop indefinitely on an incomplete-tail offset.

Separate calls are not a snapshot: append-only growth is naturally readable, but rewrites/removals can shift offsets. Document that callers should restart at zero after known mutation; metadata and provenance do not promise consistency across calls. Re-scanning rather than an index keeps sources read-only and avoids stale cached remote data later, at an initial latency cost.

### 7. Explicit result and failure semantics

Data commands emit exactly one JSON object on stdout for both success and failure. Envelope fields are `version: 1`, `ok`, and either command data with diagnostics/partial status or `error: {code, message}`. Keep stderr for non-machine diagnostics, never transcript content. Maintain the current plain help/version behavior. Use exit 0 for success/partial success, 1 for operational failures, 2 for invalid input/config, and 3 for a missing session.

Source states distinguish `ready`, `absent`, and `error`; absent implicit defaults and no enabled sources are valid empty results. Unknown/invalid formats or failed candidate files produce diagnostics and partial listing; root-level failures remain source errors. When every selected source root fails, return `sources_unavailable`. A direct read uses `invalid_request`, `session_not_found`, `source_unavailable`, `unsupported_format`, or `invalid_history` as appropriate rather than list-style partial success for a missing/unsupported session. Preserve usable body records under record-level damage.

There is no remote-host state yet: local commands neither contact hosts nor claim unreachable hosts have empty history. Later transport changes must preserve the same distinction at the host level.

## Risks / Trade-offs

- [Harness formats evolve, especially Codex composite storage] → Preserve raw data, pin fixture compatibility documentation, reject unsupported dependencies explicitly, and keep each adapter isolated. The initial release is not universal format coverage.
- [Local histories may contain credentials and prompt injection] → Read only explicitly selected roots; return content only on read, not list; label it untrusted; avoid transcript logging and any execution. Secret redaction is not promised, because silently changing history would be misleading.
- [Scanning large histories can be slow] → Use bounded metadata/record reads, cancellation, and response limits. Accept rescans initially; persistent indexing requires a separate privacy/consistency design.
- [File mutation during pagination can change results] → Promise deterministic pages only for unchanged input and explicitly document non-snapshot semantics; keep source locations in every result.
- [Oversized records omit useful context] → Report the exact omission and never pretend the response is complete; do not automatically read linked attachments to compensate.
- [Subagent records duplicate or depend on other sessions] → Preserve their identities and relationships where self-contained, and reject unsupported inherited-history assembly rather than silently deduplicating or fabricating context.

## Migration Plan

This is additive behavior over the scaffold. No history/config migration or writes are performed. Retain help/version tests, introduce synthetic fixtures and CLI contracts, then document list/read usage and the supported-format matrix. Validate on macOS and build for Linux amd64/arm64; run filesystem tests on Linux when a runner is available. Rollback consists of using the previous binary; native histories remain untouched. SSH, MCP, search, and handoff proposals follow only after this local boundary is verified.
