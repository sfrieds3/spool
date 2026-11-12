# Tasks

## 1. Local source configuration

- [x] 1.1 Add version-1 JSON configuration loading and source validation, with tests for default lookup, explicit paths, empty/disabled sources, duplicate keys/IDs, unknown fields, trailing values, unsupported versions/harnesses, and relative roots; verify with focused configuration tests.
- [x] 1.2 Add native default-root resolution for Pi, Claude, Codex active/archive, and documented environment overrides using injectable environment/home values; verify tests exercise macOS/Linux-style absolute roots and missing-default versus explicit-root classification without reading real histories.
- [x] 1.3 Document runtime configuration location, complete-source replacement semantics, environment handling, and a generic example; verify the documented JSON loads in a configuration test and explicitly states that SSH hosts are not supported yet.

## 2. Common session model and safe file references

- [x] 2.1 Introduce transport-independent session/source/record/diagnostic/error types and version-1 JSON response envelopes; verify serialization tests preserve null unknowns, native number precision, source provenance, and untrusted-history/view labels.
- [x] 2.2 Implement stable opaque reference encoding/decoding and native-identity revalidation; verify round-trip, duplicate-native-ID isolation, renamed/missing files, invalid encodings, disabled sources, absolute paths, traversal, and oversized-reference tests.
- [x] 2.3 Implement root-confined regular-file discovery/opening with cancellation and nested-symlink handling; verify temporary-directory tests reject outside targets and special files while accepting an explicitly selected root symlink, without executing subprocesses or changing source content.
- [x] 2.4 Add developer documentation for the source adapter boundary, reference stability limits, and response/error model; verify it matches exported CLI behavior as that surface lands and does not introduce speculative MCP/SSH APIs.

## 3. Native harness adapters

- [x] 3.1 Add a Pi v3 adapter and synthetic fixtures for header identity, original cwd, session naming, tool records, branches, compaction, unknown fields/types, and custom non-UUID IDs; verify adapter tests preserve native payload/relationships and explicitly reject unsupported v1/v2 or invalid headers without migration.
- [x] 3.2 Add a Claude JSONL adapter and synthetic project/subagent fixtures matching the inspected writer structure; verify tests extract recorded session identity/cwd, preserve parent/sidechain fields and summaries, discover nested subagents, and diagnose composite fork-context dependencies rather than fabricate history.
- [x] 3.3 Add a Codex self-contained rollout JSONL adapter and synthetic active/archive fixtures for session_meta, response_item, event_msg, turn_context, compaction, and unknown record types; verify tests preserve tool calls/results and reject composite/paginated/inherited-history dependency markers as unsupported without opening SQLite or materializing history.
- [x] 3.4 Record a supported-format matrix and fixture provenance notes based on the inspected packaged documentation/record structures; verify every claimed format has a fixture test and unsupported cases are explicitly documented without claiming exhaustive installed-version compatibility.

## 4. Bounded catalog and record access

- [x] 4.1 Implement deterministic catalog filtering/pagination, bounded native metadata extraction, and per-source/candidate diagnostics; verify tests cover offset boundaries, unchanged multi-page ordering, missing titles/cwd, timestamp provenance, oversized metadata, absent defaults, partial failures, all-source failures, and no enabled sources.
- [x] 4.2 Implement incremental native-line reading with physical-line offsets, record-count limits, 4 MiB line limits, and 8 MiB serialized-response limits; verify page traversal tests preserve every accepted record once, defer records at page-byte boundaries, retain exact JSON values, and explicitly report omissions.
- [x] 4.3 Add damaged/live-file handling and bounded diagnostics; verify tests cover malformed interior lines, fatal identity errors, oversized complete lines, huge unterminated tails, withheld final records with retry offsets, appended completion, diagnostic overflow, and clean EOF without infinite pagination loops.
- [x] 4.4 Document pagination defaults/maxima, line numbering, native-file-order semantics, unsupported-format handling, and non-snapshot limitations; verify examples and fixture assertions agree on continuation, partial, and EOF fields.

## 5. CLI integration

- [x] 5.1 Add list/read command routing and config, harness-filter, offset, and limit arguments while preserving existing help/version behavior; verify CLI tests cover successful operations, invalid/missing/extra arguments, unknown filters, empty catalogs, and all defined exit-code classes.
- [x] 5.2 Connect CLI commands to the shared local service and guarantee one JSON stdout object on each data-command result; verify subprocess-level golden/schema assertions cover success, partial success, missing sessions, invalid config, unsupported formats, and operational failures with no stray output.
- [x] 5.3 Update README status and local list/read examples, including secrets exposure, untrusted-history handling, unsupported composite formats, and deferred SSH/MCP/handoff behavior; verify example commands run against synthetic fixtures with harness executables absent from PATH.

## 6. End-to-end validation

- [x] 6.1 Add end-to-end fixture coverage for listing and reading all three harnesses through one explicit configuration; verify traversal attempts fail, source hashes/directory entries remain unchanged, no caches are created, and neither agents nor network clients are invoked.
- [x] 6.2 Run formatting, unit/integration tests, race tests, vet, and native build using the repository Mise toolchain; verify all pass and Linux amd64/arm64 cross-builds succeed, recording any lack of a Linux runtime test runner honestly.
- [x] 6.3 Validate the OpenSpec change with `openspec validate add-local-session-access --strict --no-interactive` and compare implementation against both delta specs; verify all tracked requirements have corresponding tests or explicit platform-validation evidence before marking implementation complete.

## Workflow follow-up

- Review and approve these artifacts before starting implementation with the apply workflow.
- Archive only after implementation, validation, and user review; archival is not part of this proposal run.
- Propose SSH host configuration and a versioned remote endpoint next, then MCP/search/handoff behavior as separate reviewed changes.
