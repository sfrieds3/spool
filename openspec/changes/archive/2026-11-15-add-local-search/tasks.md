# Tasks

## 1. Search contract and text projection

- [x] 1.1 Add typed SearchRequest, SearchResponse, SearchHit and search-specific limits/diagnostics in `internal/history`, plus query/harness/limit validation; verify tests reject invalid UTF-8, blank and oversized queries, unknown harnesses, and invalid page limits before any history-file access.
- [x] 1.2 Implement pure allowlisted native-text projection in `internal/harness` for the exact Pi, Claude Code, and Codex fields in design.md; verify table-driven tests cover strings, text block ordering, nested Claude tool results, Codex output strings, summary fields, JSON pointers, escape decoding, and ignored metadata/arguments/images/unknown types without modifying existing adapter behavior.
- [x] 1.3 Implement linear-time literal matching with Unicode simple-fold and case-sensitive modes plus original-text snippets; verify tests cover literal regex characters, retained query whitespace, non-normalized Unicode, simple-fold equivalence, multiple matches/fields, 512-code-point limits, complete match retention, correct snippet offsets, and UTF-8-safe boundaries.
- [x] 1.4 Document the request/response model, exact extraction allowlist and exclusions, and case/snippet semantics in `docs/search.md` and `docs/formats.md`; verify each documented native shape is represented by a synthetic extractor test.

## 2. Bounded confined search service

- [x] 2.1 Implement deterministic source selection/discovery for Search using the existing confined helpers and narrowly factored shared source-state logic where appropriate; verify duplicate native IDs, Claude subagent paths, Codex archived sources, absent default roots, explicit-root failures, mixed-source success, and all-roots-failed behavior with service tests and existing List regressions.
- [x] 2.2 Implement context-aware captured-extent reads with a 64 MiB file cap, 128 MiB request cap, 256-row cap, and actual underlying byte accounting; verify exact-boundary fixtures, oversized-line draining, buffered read-ahead, unexpected EOF/shrink, append/captured-extent behavior, and cancellation inside long-line discard without changing existing JSONL semantics.
- [x] 2.3 Validate identity and standalone format through the captured extent before committing bounded provisional hits; verify early matching records followed by late Claude/Codex composite markers produce no hits, malformed headers/missing or conflicting identities invalidate files, an oversized line invalidates all hits from its file (including a padded late composite marker), malformed middle lines and incomplete tails produce correct diagnostics, and failed candidates do not suppress other usable files.
- [x] 2.4 Assemble source-referenced hits, bounded generated diagnostics, partial/EOF state, and versioned envelopes; verify exact session/line/field provenance, successful lookup using existing Read, no transcript/query leakage into diagnostics, at most 64 retained diagnostics with accurate omitted counts, and serialized response sizing including escaping and session metadata. Include a hit exceeding the empty-response allowance due to large native metadata and verify `record_too_large` skips it with advancing continuation rather than an infinite retry.
- [x] 2.5 Document file/request bounds, directory-discovery limitations, unsupported-file skips, captured extents, non-snapshot behavior, sensitive unredacted snippets, and no-persistence/no-network guarantees in `docs/search.md` and the architecture docs; verify descriptions match service fixtures and preserve existing List/Read claims.

## 3. Cursor and progressive result pagination

- [x] 3.1 Implement canonical versioned search cursors with bounded decoding, request/source fingerprinting, catalog-row and physical-line coordinates; verify malformed, oversized, noncanonical, incompatible and stale/out-of-range cursors fail while changing the page limit is permitted.
- [x] 3.2 Wire cursors through hit-limit, response-byte, request-byte and row-limit stops, revalidating partially delivered files without retaining unbounded matches; verify repeated pages over unchanged fixtures have no duplicate or omitted eligible records, later-file failures discard only their own provisional hits, and empty scan-limited pages advance until EOF.
- [x] 3.3 Define diagnostic delivery across resumed lines and whole-file validation failures, and finish traversal past incomplete tails without nonadvancing retry loops; verify multi-page damaged-file tests and document exact continuation/restart examples in `docs/search.md`.

## 4. CLI integration and user-facing examples

- [x] 4.1 Add `search` dispatch and flags to `cmd/spool` with shared service calls and existing output/exit handling; verify service-versus-CLI result parity, config overrides, interspersed flags, literal dash-prefixed queries after `--`, missing/extra positionals, unknown flags, invalid cursor/query/filter inputs, partial success and cancellation with CLI tests.
- [x] 4.2 Extend synthetic fixtures and end-to-end tests across all three harnesses, including subagents and archived Codex histories; verify fixture-backed search-to-read round trips and byte-for-byte source/directory immutability with no cache/index outputs or executable/network dependencies.
- [x] 4.3 Update root help, README status/examples and docs tests for the new command and continuation usage; verify documented commands execute against synthetic fixtures, response schema examples match actual JSON, and existing list/read/help/version tests remain unchanged in behavior. MCP remains a future consumer of the service; no MCP or SSH endpoint is added by this change.

## 5. Integration checks

- [x] 5.1 Run `mise run fmt`, `mise run test`, `mise run vet`, `mise run build`, and `go test -race ./...`; verify all succeed and the full regression suite covers existing List/Read behavior alongside Search.
- [x] 5.2 Run Ripwire quality-delta and per-symbol contract checks for modified shared symbols, `git diff --check`, and strict active/archived OpenSpec validation; verify no unexplained contract changes, regressions or invalid artifacts remain, and record any accepted limitations rather than silently weakening requirements.
- [x] 5.3 Verify Linux/macOS and amd64/arm64 build compatibility through existing CI or equivalent cross-builds, and inspect the final diff for confined read-only behavior, bounded byte accounting and no newly persisted transcript content; completion requires passing integration evidence, not only checked planning artifacts.

## Workflow follow-up

- If opening a PR, include `Closes https://github.com/sfrieds3/spool/issues/5` and the final `OpenSpec-Change: add-local-search` trailer; never substitute a local backlog ID.
- Close the linked backlog item with a summary only after implementation and verification are complete; leave it doing while the plan awaits approval.
- Let the existing CI archive automation archive the completed change after it lands; do not archive an unimplemented proposal.
