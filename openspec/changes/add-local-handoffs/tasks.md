# Tasks

## 1. Shared foundations

- [x] 1.1 Move the strict-JSON duplicate-key walk and strict decode out of `internal/config` into `internal/strictjson` (adding an optional required-key check per object path), and the confined regular-file open out of `internal/history` into `internal/safefile`; verify existing config and history tests pass unchanged and new package tests cover required keys present as `null` versus omitted, duplicate keys at depth, unknown fields, trailing data, symlinks, FIFOs, and replaced files.
- [x] 1.2 Add `config.StateDir(env)` resolving `$XDG_STATE_HOME/spool`, else `~/.local/state/spool`, rejecting a relative `XDG_STATE_HOME` as invalid configuration; verify injectable-environment tests for both branches and the relative-path error, and document it in `docs/configuration.md`.
- [x] 1.3 Add `history.Service.Describe` as a no-page mode of Read's existing path (full scan, identity comparison, lenient session build, no records or line diagnostics collected); verify tests return the same `session` as `read` for each fixture harness and the same error codes as `read` for malformed, disabled-source, missing, replaced (different native ID), and unsupported references, including an unsupported marker after the first 1 MiB, with existing read tests unchanged.

## 2. Handoff model and validation

- [ ] 2.1 Add `internal/handoff` brief and stored-handoff types, version constants, limits, and the `handoff_not_found`/`handoff_invalid`/`state_unavailable` codes; verify serialization tests show null title/repository, empty lists as `[]`, and provenance labels.
- [ ] 2.2 Implement strict brief parsing and validation (size cap with overrun detection, version, omission vs `null` vs empty for every optional field, required non-blank goal, invalid raw UTF-8, NUL, per-field and per-list limits, whitespace-only strings and items, absolute clean workspace path, empty repository, duplicate session refs); verify table tests cover each rejection with `invalid_request` and that valid decoded items are preserved exactly in order.
- [ ] 2.3 Implement handoff ID generation and validation with an injected clock and random source; verify generated IDs match the documented pattern and sort by second, and that IDs containing `/`, `..`, uppercase, or extra characters are rejected.
- [ ] 2.4 Implement the shared stored-handoff validator (strict decode, required fields, version, ID/filename/`created_at` agreement, provenance enums, origin and snapshot key sets, all field limits, encoded size) and the final-encoding size check used by create; verify tests for each invalid stored shape, an unsupported future version, and a maximal brief with maximal session snapshots that encodes under `MaxHandoffBytes`.

## 3. Storage service

- [ ] 3.1 Implement history-root exclusion and the state-directory security check: canonicalize source roots (enabled or not) and the longest existing ancestor of the handoff directory, reject equality or containment in either direction (lexically after shared canonicalization, and by `os.SameFile` identity to catch case-insensitive aliases) before any directory is created, and reject non-directory, symlinked, other-owned, or group/other-accessible handoff directories without changing them; verify tests for nested, equal, containing, symlinked-ancestor, missing-root-under-symlink, and (where the test filesystem is case-insensitive) case-variant overlaps, a missing state path, a pre-existing 0755 directory, a state path that is a file, and that rejected cases create nothing.
- [ ] 3.2 Implement `Service.Create`: workspace provenance from injected cwd/host/version, session snapshots via `history.Service.Describe`, 0700 directory creation, exclusive 0600 temp file with full write and fsync, no-overwrite link with ID regeneration and re-encoding on collision, temp removal, directory sync, and post-publication warnings; use an injectable filesystem seam to verify short-write, sync, and link failures leave no published handoff or temp file, cleanup and directory-sync failures return `ok` with warnings, collisions retry with a new random suffix and embedded ID but the original timestamp, concurrent creators all publish distinct handoffs, and no file is left on validation or reference failure.
- [ ] 3.3 Implement `Service.Show` with ID validation before filesystem access, the directory security check, `safefile` open, a `MaxHandoffBytes` read cap, and the stored validator; verify tests for success, unknown ID and missing directory → `handoff_not_found`, symlinked, FIFO, oversized, edited, and future-version files → `handoff_invalid`, and that a maximal created handoff is readable.
- [ ] 3.4 Implement `Service.List`: descending ID order, offset/limit with history defaults and maxima, damaged rows consuming offsets as diagnostics, ignored non-handoff and temp entries, goal excerpts, the 8 MiB response byte budget, and no directory creation; verify tests for empty and missing directories, unchanged multi-page traversal without duplicates or omissions, partial results with diagnostics at the right offsets, a page ending before a 65th diagnostic, invalid pagination, and an early page end at the byte budget.

## 4. Markdown rendering

- [ ] 4.1 Implement the Markdown renderer with the ID-only heading, the always-present unverified-brief preamble, provenance lines, recorded-metadata labels, omitted empty sections, and per-session `spool read` lines; verify golden-file tests for minimal and full handoffs.
- [ ] 4.2 Implement the literal-text function for every dynamic field (control and bidi escapes, inline-syntax escaping, leading-whitespace removal, line-start block escapes, continuation indentation, blank-line markers); verify adversarial tests over authored items, titles, notes, repository fields, host, and recorded session metadata (headings, setext underlines, fences, indentation, block quotes, ordered and unordered list markers, links, images, autolinks, HTML, entities, tables, ANSI and C1 escapes, CR, bidi overrides) by parsing the output structure and checking no new headings, links, HTML, code, or raw control bytes appear.

## 5. CLI integration

- [ ] 5.1 Add `spool handoff create --input FILE|-`, `list [--offset N] [--limit N]`, and `show [--format json|markdown] ID` routing, help text, `--config` support, and exit-code mapping for the new codes; verify CLI tests for success, missing or unknown subcommands, missing `--input`, invalid format, list pagination flags and out-of-range values, JSON errors when Markdown was requested, and every exit class.
- [ ] 5.2 Add subprocess end-to-end tests over the synthetic fixtures with an isolated `XDG_STATE_HOME` and harness executables absent from PATH: create a handoff referencing Pi and Codex fixture sessions, list it, and show it in both formats; verify fixture hashes and directory entries are unchanged, only the handoff directory and file were written, and a test that walks `internal/handoff` and `cmd/spool` imports confirms no `os/exec`, `net`, or `net/http` use.
- [ ] 5.3 Write `docs/handoffs.md` (brief schema, value rules, limits, storage location, permissions and trust model, provenance labels, ordering and pagination semantics, rendering safety and its limits, error table, secrets warning, non-goals) and update README status/examples and `docs/architecture.md` packages, write-path, and error table; verify a docs test checks that the documented example brief is accepted by the parser.

## 6. Validation

- [ ] 6.1 Run `mise run fmt`, `mise run test`, `go test -race ./...`, `mise run vet`, `mise run build`, and Linux amd64/arm64 cross-builds locally on macOS; verify all pass, and confirm the filesystem tests (links, permissions, directory sync, FIFOs) also pass on the Linux CI runners before merge.
- [ ] 6.2 Run `openspec validate add-local-handoffs --strict --no-interactive` and check each spec scenario against a test; verify every requirement has corresponding coverage before marking implementation complete.

## Workflow follow-up

- Open a PR with `Closes https://github.com/sfrieds3/spool/issues/6` and an `OpenSpec-Change: add-local-handoffs` trailer; CI archives the change after merge once all tasks are complete.
- An MCP server exposing `handoff_create`, `handoff_list`, and `handoff_show` over the same service is a separate change.
