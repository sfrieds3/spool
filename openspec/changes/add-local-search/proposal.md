# Proposal

## Why

Spool can list and read local sessions, but finding relevant prior work still requires manually opening many native records. Literal search makes existing Pi, Claude Code, and Codex history useful to both people and agents before remote access is introduced.

## What Changes

- Add `spool search QUERY` over supported local histories, with a harness filter, case-insensitive literal matching by default, and an explicit case-sensitive option. Search decoded message and tool text, not raw JSON syntax or arbitrary metadata.
- Return bounded snippets with opaque session references, physical record lines, native record metadata, original paths, and explicit untrusted-history labeling. Each result identifies a matching record rather than claiming to reconstruct an active conversation.
- Define deterministic ordering, bounded pages and scan work, resumable continuation, UTF-8-safe snippets, and explicit diagnostics for unavailable sources, damaged files, unsupported histories, and scan limits.
- Implement search in the shared history service so the CLI and a future MCP tool use the same contract. This change does not create an MCP server; an existing MCP surface, if present when applying, must delegate to the same service.
- Rescan native files on demand without a persistent index, cache, model, embedding service, or network access. No source files are modified.
- Non-goals: regex or semantic search, relevance ranking, cross-record matches, project identity mapping, remote/SSH access, automatic summaries or handoffs, external-history reconstruction, and following attachments or transcript instructions.

## Capabilities

### New Capabilities

- `session-search`: Local literal search across supported native history content, including source-referenced results, bounded continuation, diagnostics, and a machine-readable CLI contract.

### Modified Capabilities

None. Existing list/read behavior and their contracts remain unchanged.

## Impact

- Extend `internal/history` with typed search requests/responses and a transport-independent search operation, reusing source discovery, confined regular-file access, identity/format validation, diagnostics, and limits where practical.
- Add pure text extraction alongside `internal/harness` adapters and tests for the supported message/tool shapes; reuse the incremental `internal/jsonl` reader.
- Add CLI parsing and help in `cmd/spool`, synthetic fixtures and service/CLI regression tests, and search documentation alongside the existing format/reading docs.
- No new runtime dependencies or source configuration schema are needed. Search has no persistent storage and remains a standalone local Go operation on macOS and Linux.
- Tracked by https://github.com/sfrieds3/spool/issues/5; implementation and closure follow review and approval of these artifacts.
