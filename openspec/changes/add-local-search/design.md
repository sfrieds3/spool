# Design

## Context

See proposal.md for motivation and specs/session-search/spec.md for the new contract. `internal/history.Service` currently owns List/Read, source selection, filesystem confinement, native identity, diagnostics, serialization, and pagination. `internal/harness.Adapter` parses identity/type/title but does not extract searchable text. `internal/jsonl.Reader` bounds buffered lines but drains an oversized line to its end, and Read scans the whole file to detect late composite-history markers. Neither calling List then Read for every page nor returning matches before full validation provides a bounded, reliable search implementation.

The user chose a case-insensitive literal MVP without a persistent index. Existing native-format fixtures and session-reading requirements remain authoritative; native file order is not an active conversation. MCP is a separate milestone and no MCP server is present in this branch.

## Goals / Non-Goals

**Goals:** Add one transport-independent search operation with deterministic extraction, matching, bounded candidate validation, source citations, and resumable result pages on macOS and Linux. Reuse confinement and source/error semantics without regressing List/Read.

**Non-Goals:** Persistent storage, remote access, tool-input or arbitrary metadata search, inferred reasoning, active-branch selection, semantic retrieval, and a new MCP endpoint. Semantic retrieval, hybrid ranking/evaluation, and index lifecycle/privacy have separate backlog items.

## Decisions

### 1. Shared service, thin CLI, narrow reuse

Add typed `SearchRequest`, `SearchResponse`, and `SearchHit` models in `internal/history` and `Service.Search(ctx, req)` in a new search implementation file. SearchRequest holds Query, Harness, CaseSensitive, Limit, and Cursor. Reuse `openSourceRoot`, `discover`, `mergeRows`, `openRegular`, adapter parsing, `meta.observe`, `buildSession`, reference validation/encoding, bounded diagnostics, and actual response serialization. Extract narrowly shared source-opening/status helpers from List only where they remove real duplication; do not change its existing pagination contract. Add CLI dispatch with `spool search [flags] QUERY`, using existing interspersed flag parsing, configuration loading, context cancellation, JSON output, and exit-code mapping. A query beginning with a dash can be supplied after `--`.

Do not compose repeated List/Read calls: List's metadata prefix cannot establish complete standalone support, while Read's whole-file rescan for each result page repeats work and has no search byte budget. Do not add a package dependency or runtime requirement; development remains managed by Mise, distributed binaries do not require it.

### 2. Pure allowlisted text projection

Add a pure adapter-adjacent text projection function returning ordered `(JSON pointer, decoded string)` fields. Keep this separate from mandatory Adapter methods unless extending that interface proves simpler across all three current implementations. It accepts only already validated native JSON objects; it never opens files. Define field support and fixture coverage explicitly:

- Pi: `message.message.content` strings or ordered `type: text` blocks' `text` strings, including toolResult content; `branch_summary.summary` and `compaction.summary` strings.
- Claude: `user`/`assistant` message content strings or ordered text blocks; tool_result content strings or ordered text blocks nested in that content; top-level `summary.summary` strings. Do not separately search duplicate `toolUseResult.stdout/stderr` mirrors.
- Codex: `response_item` messages' ordered `input_text`/`output_text`/`text` blocks; `function_call_output.payload.output` when it is a string; `compacted.payload.message` strings. A string containing JSON output is searched as that recorded string, not recursively reparsed as another document. Do not search event_msg message mirrors.

JSON pointers use RFC 6901 escaping and actual array indexes. Ignore wrong-shaped fields and unknown block/record types without interpreting them as text; document this projection limitation rather than calling it a complete conversation search. Do not include thinking blocks, tool-call arguments, images, binary/base64 data, titles, IDs, paths, usage records, or arbitrary extension strings. These remain accessible through Read. No joining blocks: matches cannot cross fields or records. Choosing arbitrary recursive JSON-string matching was rejected because it creates unstable matches in metadata and future unknown fields.

### 3. Literal matching and snippets

Validate a nonblank UTF-8 query, at most 256 bytes, before accessing roots. Keep its original whitespace. Default matching uses one-code-point Unicode simple-fold equivalence (Go unicode.SimpleFold), not locale-aware matching, multi-code-point folding, or normalization; case-sensitive mode compares original code points. Compute original text offsets while matching, so Unicode folding cannot corrupt citations. Use a linear-time substring algorithm with at most query-sized matching state, not repeated whole-field lowercasing or quadratic rescans.

Return one result per record, for the first matching projected field and its first occurrence. Select a snippet beginning up to 96 code points before the match, extend through at most 512 code points total, and backfill context near the end when possible. The whole match fits because the query is at most 256 bytes. Preserve original text and do not insert ellipses into it; return `truncated_before`, `truncated_after`, and `match_start`/`match_end` as half-open code-point offsets relative to the snippet. Result fields are `session` (existing Session), `line`, nullable native `type`, `subtype`, `id`, `parent_id`, `timestamp`, `field` (pointer), `snippet`, match offsets, and truncation flags. Do not include full native records in search results.

### 4. Validate a captured file extent before committing hits

Reuse deterministic catalog rows ordered by source ID and slash-relative path. For each candidate, confined-open a regular file, capture its size, and skip it with `session_too_large` if it exceeds 64 MiB. Read at most that captured size through an underlying context-aware, byte-counted reader, checking cancellation on every bounded read (at most 64 KiB). This enforces limits even while JSONL discards an oversized line. Count actual underlying bytes, including read-ahead, skipped lines and validation after the result page fills. Later appends are outside this invocation's captured extent and visible on restart.

Validate identity and format through the captured extent on the same open file. Retain only bounded provisional hits that could fit the output page, plus bounded diagnostics; continue parsing after the provisional page fills to find late fatal markers. Commit hits only after successful validation. Any FormatError, unsupported marker, missing/conflicting identity, read error, shrink, or detected replacement/modification of the captured content discards that file's provisional hits and produces an appropriate file diagnostic. Detect observable changes with post-read stat checks; this is best-effort, not an atomic snapshot. Any oversized physical line makes standalone validation incomplete: discard all provisional hits for that file with `oversized_line` and advance to later candidates, even if the oversized record might be ordinary content. This fails closed when a padded Claude/Codex composite-history marker cannot be parsed within the line bound. Normal incomplete tails are diagnosed and withheld, but do not invalidate eligible earlier records; an incomplete required header does invalidate them. A same-length undetectable concurrent rewrite cannot be guaranteed away and is documented as a non-snapshot limitation.

Use a limited-reader boundary to distinguish reaching the captured extent from unexpected underlying EOF; do not label intentional byte-bound exhaustion as a completed validation. Files unable to finish validation are not eligible for hits. Avoid reopening the same file for a separate validation pass, which could match different content than was validated. This design deliberately trades large-file coverage for trustworthy, bounded results.

### 5. Work budgets and continuation

A request processes at most 256 catalog rows and reads at most 128 MiB of native file bytes. Reserve each candidate's captured extent against the remaining budget before beginning it; if it does not fit, stop before the candidate, report `scan_limit`, and return its position. The per-file cap is smaller than the request cap, so a fresh request can always attempt the next eligible file. Oversized files and filesystem issue rows advance normally with diagnostics. Discovery still enumerates directory entries to establish sorted order: these limits bound native-content scanning, not the existing discovery tree's memory/entry count. Document that distinction rather than claiming a hard whole-command latency bound.

The cursor is `q1.` plus canonical unpadded base64url JSON containing a SHA-256 request fingerprint, catalog row index, and physical-line offset within that row. Fingerprint the exact query, case mode, harness, and selected source configuration (including roots/default status); changing the hit limit is allowed. Reject noncanonical or oversized cursors (maximum 4 KiB), invalid numeric ranges, unsupported versions, and fingerprint mismatches before file reads. Decode against discovered rows only; cursors are locators, not credentials or arbitrary paths. A cursor beyond a now-shorter catalog is rejected as stale; callers restart after known mutations.

Within a file, the line offset is the last physical record consumed for result delivery, not bytes validated. When a limit or response budget defers the next eligible hit, retain that line's predecessor as continuation and revalidate the same file on the next call. When that file is exhausted, advance to the next catalog row and reset the line offset. Damaged lines advance line consumption. Diagnostics before the cursor's line are not repeated, but late validation failures remain file-scoped and can recur until the caller moves past the file. A whole-file validation failure discards provisional hits and advances to the next row rather than trapping the cursor. Retaining only one page of provisional hits avoids buffering an entire file's matches.

The JSON response has `version`, `ok`, `content: untrusted_history`, `view: native_file_order`, `sources`, `hits`, `limit`, `next_cursor`, `eof`, `partial`, `diagnostics`, and `omitted_diagnostics`. It does not reuse Page's offset fields because these represent different coordinates. Enforce the existing 8 MiB serialization cap with reserved envelope/diagnostic space and measured hit serialization. If a hit fits an empty response's allowance but not the current page, defer it without consuming it. If a hit exceeds that empty-page allowance by itself (for example because native session and record timestamps are huge), omit it with source/line-scoped `record_too_large` and advance past that record; do not truncate metadata or retry it indefinitely. The unchanged-input completeness promise applies to eligible hits fitting that allowance, and every oversized exclusion is diagnosed. Stop before result overflow only after validating the current candidate. Page fullness alone is not partial; diagnostics including `scan_limit` are. An empty non-EOF page with an advancing cursor is valid. EOF means the captured traversal was exhausted, not that damaged or oversized data was searched successfully. Incomplete tails do not pin continuation forever: finish traversal and instruct callers to restart after source changes.

### 6. Isolation and failure handling

Reuse built-in absent versus explicit-source error semantics. Return `sources_unavailable` only when all selected source roots fail; candidate errors are diagnostics even if every candidate is unsupported. Zero selected enabled sources or all absent defaults is empty success. Bound diagnostic retention at 64 plus an omitted count and keep generated diagnostic text free of query/transcript content. Preserve references and original machine paths and distinguish duplicate native identities by source/path.

No network, history writes, persistent indexes, attachments, or executables are introduced. Search returns sensitive native text without redaction, matching Read's trust boundary; docs must call this out. New tests cover nested symlinks, special files, source failures, source immutability, and cancellation in oversized-line discard. Future MCP tools consume the same typed service response rather than extracting independently.

## Risks / Trade-offs

- [Large histories cost repeated reads] → No-index scanning is explicit; document row/byte caps and progressive continuation, with persistent indexing deferred to a separate design.
- [Files above 64 MiB are excluded] → Emit `session_too_large`, set partial, and never claim their contents were searched; this is a proposed MVP bound to review before applying.
- [Late composite markers invalidate early hits] → Validate to the captured extent before publishing; decisive fixtures place those markers after early matches and beyond the prospective page boundary.
- [Live mutation shifts row/line cursors] → Document non-snapshot pagination and restart after source changes; check observable per-file mutation and reject incompatible request cursors.
- [Projection misses unfamiliar text shapes] → Publish the exact allowlist, test each supported shape, and keep native Read available; never recursively search unknown strings by default.
- [Shared helper extraction changes existing behavior] → Keep refactors narrow and run full List/Read regression tests on all supported platforms.

## Migration Plan

This is additive: no source configuration, existing reference, or response migration is required. Add search implementation and docs after proposal review, run fmt/test/vet/build and race tests, and preserve existing CLI outputs. Rollback removes the additive command/service capability without cleaning up persistent data because none is created. Any resulting PR closes https://github.com/sfrieds3/spool/issues/5 and carries `OpenSpec-Change: add-local-search`; the backlog item closes only after the implemented feature is verified, not when this plan is complete.
