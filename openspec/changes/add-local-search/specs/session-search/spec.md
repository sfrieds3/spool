# Spec Delta

## Purpose

Let people and agents locate relevant text in supported local coding-agent histories while retaining exact source references, bounded resource use, and explicit completeness limitations.

## ADDED Requirements

### Requirement: Literal query semantics

Search SHALL accept one nonblank valid UTF-8 literal query of at most 256 bytes, an optional supported harness filter, and a case-sensitive option. Matching SHALL default to Unicode simple case folding without normalization, regex syntax, tokenization, or relevance ranking. Whitespace within a valid query SHALL remain literal. Invalid requests SHALL fail before history scanning.

#### Scenario: Case-insensitive literal match
- **WHEN** the query is `widget.*` and searchable text contains `WIDGET.*`
- **THEN** default search matches that literal substring, not arbitrary text beginning with widget

#### Scenario: Exact case requested
- **WHEN** case-sensitive search requests `Widget` and a record contains only `widget`
- **THEN** that record is not a match

#### Scenario: Invalid request
- **WHEN** the query is empty, whitespace-only, invalid UTF-8, or exceeds 256 bytes, or the harness or page size is invalid
- **THEN** search returns `invalid_request` before opening native history files

### Requirement: Documented native text projection

Search SHALL match decoded text in documented, allowlisted message, tool-output, and summary fields for supported Pi, Claude Code, and Codex histories. Text fields SHALL be searched separately in deterministic native block order. Metadata, tool-call arguments, images, attachments, unknown record shapes, and arbitrary JSON keys or string leaves SHALL NOT become searchable implicitly.

#### Scenario: Escaped text and multiple blocks
- **WHEN** a supported message has JSON-escaped text and multiple text blocks
- **THEN** search matches decoded text within each block and does not match a phrase spanning blocks or records

#### Scenario: Metadata-only occurrence
- **WHEN** a query appears only in a session ID, working directory, title, tool input, image payload, or unknown field
- **THEN** that occurrence does not produce a hit

#### Scenario: Supported harness text
- **WHEN** supported local Pi, Claude Code, and Codex files contain matching allowlisted message or tool-output text, including Claude subagent files and archived Codex files
- **THEN** search returns record-level hits without installing or launching those harnesses

### Requirement: Source-referenced bounded snippets

Search SHALL return at most one hit per physical record, using its first matching text field and first occurrence. A hit SHALL include session metadata and reference, physical line, native record metadata, text-field JSON pointer, and a snippet of at most 512 Unicode code points containing the whole match. Match positions SHALL be zero-based half-open code-point offsets within the snippet, with explicit prefix/suffix truncation flags.

#### Scenario: Recover the original record
- **WHEN** a search hit names physical line n in a referenced session
- **THEN** reading that reference with offset n minus one can retrieve the original native record under the existing read contract

#### Scenario: Multiple matches and Unicode
- **WHEN** a record contains multiple matching fields and the first matching field is longer than the snippet bound with multibyte characters near the match
- **THEN** one hit identifies that field, includes the entire first match, and contains valid UTF-8 with accurate code-point offsets and truncation flags

### Requirement: Deterministic resumable pages

Search SHALL order hits by source ID, source-relative path, and physical line, not relevance. The default hit limit SHALL be 50 and the maximum 500. Responses SHALL provide an opaque continuation cursor or null and an explicit EOF flag. Unchanged inputs SHALL paginate without duplicate or omitted eligible records. Invalid or request-mismatched cursors SHALL fail; changing histories SHALL be documented as non-snapshot inputs.

#### Scenario: Continue after matching records
- **WHEN** an unchanged search spans several pages and callers pass each returned cursor with the same query, harness, and case mode
- **THEN** pages return eligible hits in order without repeating or skipping records, even when consecutive hits belong to one file

#### Scenario: Cursor cannot be reused for another search
- **WHEN** a cursor is malformed, unsupported, or used with a different query, case mode, harness, or enabled-source configuration
- **THEN** search returns `invalid_request` rather than accepting it as a path or silently changing its meaning

#### Scenario: Scan page has no hits
- **WHEN** a bounded scan finds no matches but unexamined candidates remain
- **THEN** it returns an advancing cursor with `eof: false`, not a claim that the entire history has no matches

### Requirement: Explicit search work and response bounds

Search SHALL validate at most 256 candidate/issue rows and consume at most 128 MiB of native-file bytes per request, with a 64 MiB per-file validation cap, a 4 MiB physical-line bound, an 8 MiB serialized-response cap, and at most 64 retained diagnostics plus an omitted count. Scan-bound stops SHALL return resumable continuation and a diagnostic; files exceeding the validation cap SHALL be skipped explicitly.

#### Scenario: Large candidate
- **WHEN** a candidate exceeds 64 MiB or cannot be completely validated within that cap
- **THEN** search returns no hits from that file, reports `session_too_large`, and can continue with later candidates

#### Scenario: Request budget reached
- **WHEN** the next file cannot fit in the remaining request byte budget or the row budget is reached
- **THEN** search stops before that candidate with a continuation cursor and a `scan_limit` diagnostic rather than silently treating it as searched

#### Scenario: Oversized physical line hides a marker
- **WHEN** any physical line exceeds 4 MiB, including a padded Claude fork-context reference or Codex composite-history marker after an early match
- **THEN** search excludes all hits from that file with an `oversized_line` diagnostic because its standalone format cannot be fully validated, counts consumed bytes against its work limits, and advances to later candidates

### Requirement: Oversized hits cannot stall continuation

A hit that cannot fit within an otherwise empty response's byte allowance SHALL be omitted with `record_too_large` at its source line. Continuation SHALL advance past that record instead of returning a cursor that retries it forever. The unchanged-input pagination guarantee SHALL cover eligible hits that fit the documented per-response allowance, with every oversized exclusion diagnosed.

#### Scenario: Native metadata makes one hit too large
- **WHEN** an otherwise supported matching record and its session metadata exceed the response allowance even though their individual physical lines fit the line limit
- **THEN** search diagnoses and skips that hit, returns later eligible hits when available, and does not repeat the same continuation position

### Requirement: Complete standalone format validation

Search SHALL validate each candidate's identity and standalone format through its captured file extent before publishing any hits from that candidate. Unsupported composite histories, fatal identity errors, and incomplete required headers SHALL produce file-scoped diagnostics and no hits. A later unsupported marker SHALL invalidate earlier provisional hits. External histories SHALL NOT be followed.

#### Scenario: Late composite marker
- **WHEN** an early matching record is followed by a Claude fork-context reference or an unsupported Codex history marker
- **THEN** search discards all hits from that file and reports `unsupported_format`, including when the output hit limit was already reached provisionally

#### Scenario: Live incomplete tail
- **WHEN** an otherwise supported file has an unterminated final record in its captured extent
- **THEN** search withholds that record, reports `incomplete_tail`, and returns only eligible earlier hits; callers are told to restart after a source change

### Requirement: Distinguish empty history from unavailable sources

Search SHALL reuse local source selection and distinguish absent built-in roots from failed explicit roots. Usable sources SHALL remain searchable when others fail, with source states, diagnostics, and `partial: true`. All selected roots failing SHALL return `sources_unavailable`. Unsupported or damaged candidate files SHALL be diagnosed rather than treated as root failures. Zero matches alone SHALL NOT imply a failure.

#### Scenario: Mixed source availability
- **WHEN** one selected root fails and another is usable
- **THEN** search returns usable results, the failed source state, and a source-scoped diagnostic with `partial: true`

#### Scenario: Empty and wholly failed sources
- **WHEN** selected built-in roots are absent or usable roots have no matches
- **THEN** search succeeds with an empty hit list, whereas all selected roots failing returns `sources_unavailable`

#### Scenario: Damaged middle record
- **WHEN** a supported file contains malformed complete non-header lines between valid matching records
- **THEN** search returns the eligible hits with physical-line diagnostics and `partial: true`

### Requirement: Shared machine-readable search contract

`spool search QUERY` SHALL expose the shared application search operation with `--config`, `--harness`, `--case-sensitive`, `--limit`, and `--cursor`. It SHALL emit one version-1 JSON response, using exit 0 for success including partial results, 1 for operational failures/cancellation, and 2 for invalid requests/configuration. CLI-specific parsing SHALL NOT duplicate native extraction or matching logic. Existing list/read behavior SHALL remain unchanged.

#### Scenario: Machine consumer continues search
- **WHEN** a CLI request succeeds or partially succeeds
- **THEN** stdout contains exactly one JSON object with `ok: true`, hits, source states, continuation, and diagnostics, and the process exits 0

#### Scenario: Cancellation
- **WHEN** a search context is canceled, including during a very long line
- **THEN** search stops reading promptly and returns `canceled` rather than a successful complete result

### Requirement: Local confined untrusted retrieval

Search SHALL access only discovered regular files within enabled local source roots using the existing confinement policy. Responses SHALL label snippets `untrusted_history` and `native_file_order`, preserve original paths, and make no active-context claims. Search SHALL NOT execute content, launch agents, follow attachments, contact networks, or persist indexes, embeddings, caches, or native-history changes.

#### Scenario: Instructions and escaping symlinks
- **WHEN** history contains executable instructions or discovery encounters a nested symlink
- **THEN** matching instructions remain untrusted snippet data and the symlink is skipped without reading its target

#### Scenario: No persistent side effects
- **WHEN** search runs against synthetic histories on macOS or Linux
- **THEN** native files and directory entries remain unchanged, no cache/index is created, and no external executable or service is required
