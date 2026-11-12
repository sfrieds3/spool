# session-catalog Specification

## Purpose
Let users and coding agents discover saved sessions across local harnesses without running those harnesses or depending on terminal management.

## Requirements

### Requirement: Local source configuration

Spool SHALL load versioned JSON configuration from an explicit `--config` path or its documented default config location, and SHALL allow Pi, Claude Code, and Codex sources to be enabled, disabled, and assigned absolute history roots. Without a default config file, Spool SHALL use documented native defaults. Invalid configuration SHALL fail before reading history.

#### Scenario: No configuration exists
- **WHEN** `spool list` runs without an explicit configuration and the default config file does not exist
- **THEN** Spool attempts the documented local default roots without creating a configuration file

#### Scenario: Custom source root
- **WHEN** a source has a valid absolute root override
- **THEN** Spool reads that source only from the overridden root rather than combining it with its default root

#### Scenario: Invalid explicit configuration
- **WHEN** `--config` names a missing file or configuration contains an unsupported version, unknown field, duplicate source identifier, or relative root
- **THEN** Spool reports `invalid_config` and does not scan history

### Requirement: Native session discovery

Spool SHALL discover supported Pi, Claude Code, and Codex saved conversation files without starting an agent process. Results SHALL contain an opaque session reference, source identifier, harness, native identity when available, original working directory when available, native title when available, and timestamps with documented provenance. Unknown metadata SHALL be null, not invented.

#### Scenario: Three harnesses with saved conversations
- **WHEN** enabled local roots contain supported saved conversations from all three harnesses
- **THEN** `spool list` returns their metadata without requiring Pi, Claude, Codex, or Node to be installed or running

#### Scenario: Missing metadata
- **WHEN** a valid session does not record a title or working directory
- **THEN** its corresponding fields are null rather than inferred from a prompt or a guessed checkout

### Requirement: Stable unambiguous references

Spool SHALL return session references that remain stable across unchanged repeated discovery and distinguish files with equal native IDs in different sources or locations. References SHALL resolve only through enabled configured sources and discovered relative file identities, never as arbitrary filesystem paths. Malformed references SHALL be invalid requests; disappeared valid references SHALL be missing sessions.

#### Scenario: Duplicate native identity
- **WHEN** two discovered files share a harness-native ID
- **THEN** their Spool references differ and each reads only its own file

#### Scenario: Traversal reference
- **WHEN** a supplied reference contains encoded traversal or names an unconfigured source
- **THEN** Spool rejects it without reading a file outside an enabled source

### Requirement: Bounded filtered listing

`spool list` SHALL support a harness filter, a nonnegative offset, and a positive limit with documented defaults and maximums. Results SHALL have deterministic ordering for unchanged inputs, continuation metadata, and bounded response size. Invalid filters and pagination arguments SHALL be invalid requests. Spool SHALL document that separate calls do not form a snapshot of changing histories.

#### Scenario: Repeated bounded pages
- **WHEN** an unchanged catalog has more sessions than the requested limit
- **THEN** repeated requests with the returned continuation offset retrieve the ordered catalog without duplicating or omitting sessions

#### Scenario: Invalid page size
- **WHEN** a limit is zero, negative, or above the maximum
- **THEN** Spool returns `invalid_request` instead of silently changing the limit

### Requirement: Source isolation and diagnostics

Spool SHALL distinguish an absent default root from an unreadable or missing explicitly configured root. A list with usable sources SHALL return their results and explicit diagnostics for failed sources or skipped candidate files. A wholly failed requested source set SHALL fail rather than report empty success. This local operation SHALL never initiate SSH or network access.

#### Scenario: Harness has never been used
- **WHEN** a native default root does not exist
- **THEN** Spool records that source as absent and returns a successful empty catalog if no requested source failed

#### Scenario: Partial source failure
- **WHEN** one requested source is unreadable and another is usable
- **THEN** Spool returns the usable results with `partial: true` and a source-scoped failure diagnostic

#### Scenario: All requested sources fail
- **WHEN** all requested sources fail to open or inspect their roots
- **THEN** Spool returns `sources_unavailable` rather than an empty successful catalog

### Requirement: Machine-readable CLI contract

List and read commands SHALL emit one versioned JSON response on stdout and use exit codes 0 for success including explicit partial results, 1 for operational failure, 2 for invalid requests or configuration, and 3 for a missing session. Error responses SHALL contain a stable error code and message. Diagnostic text SHALL NOT corrupt JSON stdout. Existing help and version behavior SHALL remain supported.

#### Scenario: Machine consumer receives partial success
- **WHEN** listing succeeds with one source failure
- **THEN** stdout contains one JSON response with `version: 1`, `ok: true`, `partial: true`, and diagnostics, and the process exits 0

#### Scenario: Invalid command input
- **WHEN** list or read receives invalid arguments
- **THEN** stdout contains one JSON error response with `ok: false` and an appropriate error code, and the process exits 2
