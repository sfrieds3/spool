# session-reading Specification

## Purpose
Provide bounded read-only access to saved conversation records so agents can consult prior work while retaining its source, structure, and limitations.

## Requirements

### Requirement: Read discovered histories

`spool read <session-id>` SHALL read a discovered saved session by its opaque reference without requiring the original agent to be running or its original working directory to exist. Reading SHALL revalidate the reference against enabled sources and the current file. A missing session SHALL return `session_not_found`; an unsupported history format SHALL return `unsupported_format`, not an empty conversation.

#### Scenario: Original checkout is gone
- **WHEN** a supported session exists but its recorded working directory no longer exists
- **THEN** Spool returns its history and original working-directory metadata without attempting to enter or recreate that directory

#### Scenario: Session disappeared
- **WHEN** a valid discovered reference points to a file deleted before reading
- **THEN** Spool returns `session_not_found` and exits 3

#### Scenario: Unsupported session schema
- **WHEN** a file uses a schema that the adapter cannot safely identify as supported
- **THEN** Spool reports `unsupported_format` rather than pretending that it contains no history

### Requirement: Preserve recorded information and provenance

Read responses SHALL identify the source, harness, native session identity when available, original path, and original working directory when available. Each returned record SHALL carry its physical line number, original type when available, available native relationships, and parsed native payload without dropping unknown fields. Historical paths SHALL NOT be rewritten to local checkout paths.

#### Scenario: Tool record has unfamiliar fields
- **WHEN** a supported session contains a valid record with additional tool-result fields
- **THEN** Spool retains those fields in the native payload rather than silently discarding them

#### Scenario: History references a different machine layout
- **WHEN** a record includes an absolute source-machine path
- **THEN** the returned record preserves that value and does not claim the file exists in the current environment

### Requirement: Expose structural history without invented context

Spool SHALL preserve available branch, parent, compaction, and summary records, and SHALL label the read as native file-order history rather than a reconstructed active conversation. Unknown record types within a supported format SHALL remain available as native data. Spool SHALL NOT claim to recover unrecorded reasoning or the exact model context used by another harness.

#### Scenario: Branched Pi conversation
- **WHEN** a session contains records from multiple branches and a compaction entry
- **THEN** Spool returns their recorded identities and relationships without concatenating them into a purported single active chat

#### Scenario: New record type in a supported format
- **WHEN** a valid JSON record has an unfamiliar type but the session format is supported
- **THEN** Spool returns the native record with its source location and unknown type intact

### Requirement: Bounded record pagination

Read SHALL accept a nonnegative physical-line offset and a positive record limit with documented defaults and maxima. Responses SHALL include continuation metadata and explicit end-of-file state, and SHALL enforce record and response byte limits without silently truncating payloads. Unchanged files SHALL paginate without omissions or duplicates. Changes between calls SHALL be documented as non-snapshot behavior.

#### Scenario: More records than one page
- **WHEN** an unchanged file contains more complete records than the requested page allows
- **THEN** the response supplies a continuation offset and subsequent pages retrieve the remaining complete records in file order

#### Scenario: One record exceeds the byte limit
- **WHEN** a record exceeds the documented maximum record size
- **THEN** Spool reports its location as an omitted oversized record and advances past it without silently returning truncated content

### Requirement: Handle incomplete and damaged files explicitly

Spool SHALL return usable records from supported histories with record-scoped diagnostics for malformed complete lines, oversized records, and an incomplete final line. Such responses SHALL be marked partial. A malformed required identity or format header SHALL fail the read instead of guessing a session identity. Incomplete final data SHALL NOT be interpreted as a complete record.

#### Scenario: Agent is appending a record
- **WHEN** a supported JSONL session ends with an unterminated record while an agent is writing
- **THEN** Spool returns prior complete records, reports the incomplete tail, and provides an offset that permits retrying that tail later

#### Scenario: Invalid middle line
- **WHEN** a malformed complete line appears between supported valid records
- **THEN** Spool returns the valid records with a diagnostic locating the skipped line and `partial: true`

### Requirement: Read-only confined access

History operations SHALL open only regular discovered files within enabled source roots, reject paths escaping those roots, and avoid following nested symlinks outside them. Spool SHALL NOT execute history content, invoke an agent, inspect referenced attachments outside source history, create caches, alter source files, or change filesystem permissions. Source content SHALL be explicitly labeled untrusted historical data.

#### Scenario: Nested symlink targets an unrelated file
- **WHEN** a candidate transcript path traverses a symlink outside the configured source root
- **THEN** Spool rejects or skips it with a diagnostic and does not return the target's content

#### Scenario: Transcript contains commands or instructions
- **WHEN** a history record contains executable commands or text asking the reader to ignore instructions
- **THEN** Spool returns that text only as untrusted record data and launches no subprocess or agent

#### Scenario: Sources remain unchanged
- **WHEN** list and read complete against synthetic fixtures
- **THEN** fixture contents and directory entries remain unchanged and no cache files are created
