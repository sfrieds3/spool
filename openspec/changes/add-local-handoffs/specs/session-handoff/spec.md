# Spec Delta

## Purpose

Let work continue between Pi, Claude Code, and Codex through explicit, locally stored continuation briefs that record goals, constraints, decisions, progress, outstanding work, workspace provenance, and references to source sessions, without transferring agent state or taking actions.

## ADDED Requirements

### Requirement: Authored brief content

`spool handoff create` SHALL accept a version-1 JSON brief from an explicitly named input file or, when explicitly requested, stdin. The brief SHALL require a nonempty goal and MAY contain a title and lists of constraints, decisions, progress, outstanding work, and notes. Spool SHALL store the authored text as supplied and SHALL NOT infer, summarize, or generate brief content from history or a model.

#### Scenario: Minimal brief
- **WHEN** a brief contains only a version and a goal
- **THEN** Spool creates a handoff whose other content lists are empty and whose title is null

#### Scenario: Full brief preserves authored order
- **WHEN** a brief contains constraints, decisions, progress, outstanding work, and notes
- **THEN** the stored handoff contains each decoded item exactly and in the supplied order, and the Markdown rendering presents each item literally in the same order

#### Scenario: No content is invented
- **WHEN** a brief references sessions but supplies no decisions
- **THEN** the handoff has an empty decision list rather than decisions derived from the referenced history

### Requirement: Strict brief validation

Creation SHALL reject, as `invalid_request` and before writing anything, a brief that is not one JSON object, has trailing data, repeats a key, has unknown fields, uses an unsupported version, omits or blanks the goal, has blank optional strings or items, contains invalid UTF-8 or NUL, or exceeds documented input, field, list, or encoded-handoff size limits.

#### Scenario: Unknown field
- **WHEN** a brief contains a field not defined by version 1
- **THEN** Spool returns `invalid_request`, exits 2, and creates no file

#### Scenario: Oversized input
- **WHEN** the brief input exceeds the documented maximum size
- **THEN** Spool stops reading, returns `invalid_request`, and creates no file

#### Scenario: Blank goal
- **WHEN** the goal is missing or contains only whitespace
- **THEN** Spool returns `invalid_request` and creates no file

### Requirement: Workspace provenance

Each handoff SHALL record its creation time, local host name, Spool version, and a workspace path: an absolute path supplied in the brief, otherwise the creating process's working directory. Author-supplied repository details SHALL be stored as reported and labeled unverified. Spool SHALL NOT run version-control or other processes, or check that recorded paths exist, to obtain or verify provenance.

#### Scenario: Default workspace
- **WHEN** a brief omits the workspace path
- **THEN** the handoff records the creating process's working directory with provenance indicating it was observed by Spool

#### Scenario: Relative workspace path
- **WHEN** a brief supplies a workspace path that is not absolute
- **THEN** Spool returns `invalid_request` and creates no file

#### Scenario: Reported repository details
- **WHEN** a brief supplies a branch and commit
- **THEN** the handoff stores them with author-reported provenance and Spool launches no subprocess

### Requirement: Source session references

A brief MAY reference sessions by `spool list` references. Creation SHALL validate each exactly as `spool read` would and snapshot its reference, harness, source, kind, native ID, original path, recorded working directory, title, and start time without copying records. Any reference `read` would reject SHALL fail creation with the same error code and create no handoff.

#### Scenario: Referenced sessions are snapshotted
- **WHEN** a brief references a Pi session and a Codex session that exist
- **THEN** the handoff records both references with their recorded metadata and contains no transcript records

#### Scenario: Missing referenced session
- **WHEN** a referenced session file no longer exists
- **THEN** Spool returns `session_not_found`, exits 3, and creates no file

#### Scenario: Unsupported referenced session
- **WHEN** a referenced file contains an external-history marker anywhere in the file
- **THEN** Spool returns `unsupported_format`, exits 1, and creates no file

#### Scenario: Duplicate reference
- **WHEN** a brief lists the same session reference twice
- **THEN** Spool returns `invalid_request` and creates no file

### Requirement: Local handoff storage

Spool SHALL store each handoff as one file under `$XDG_STATE_HOME/spool/handoffs`, defaulting to `~/.local/state/spool/handoffs`, with owner-only permissions. It SHALL publish atomically, never edit or overwrite a published handoff, and refuse a handoff directory overlapping a configured history root. An unusable or insecure state directory SHALL fail with `state_unavailable`.

#### Scenario: First handoff on a machine
- **WHEN** the handoff directory does not exist
- **THEN** Spool creates it with owner-only permissions and stores the handoff there

#### Scenario: State directory inside a history root
- **WHEN** the resolved handoff directory is inside a configured source root
- **THEN** Spool returns `invalid_config` and writes nothing

#### Scenario: Relative state home
- **WHEN** `XDG_STATE_HOME` is set to a relative path
- **THEN** Spool returns `invalid_config` and writes nothing

#### Scenario: Insecure existing directory
- **WHEN** the handoff directory exists with group or other permissions, or is a symbolic link
- **THEN** Spool returns `state_unavailable` without changing its permissions or writing a handoff

#### Scenario: Unwritable state directory
- **WHEN** the handoff directory cannot be created or written
- **THEN** Spool returns `state_unavailable`, exits 1, and source histories remain unchanged

### Requirement: Handoff identifiers

Each handoff SHALL receive an identifier that is unique in its handoff directory, begins with its UTC creation second, and contains only lowercase letters, digits, and hyphens. `show` SHALL accept only well-formed identifiers and SHALL NOT resolve them as filesystem paths. A well-formed identifier with no stored handoff SHALL return `handoff_not_found` (exit 3); a stored file that fails validation SHALL return `handoff_invalid` (exit 1).

#### Scenario: Traversal identifier
- **WHEN** `spool handoff show` receives an identifier containing a path separator or `..`
- **THEN** Spool returns `invalid_request` without opening any file

#### Scenario: Unknown identifier
- **WHEN** a well-formed identifier names no stored handoff
- **THEN** Spool returns `handoff_not_found` and exits 3

#### Scenario: Edited or damaged stored file
- **WHEN** a stored handoff file has an unknown field, an unsupported version, an embedded ID that differs from its name, or exceeds the stored-size limit
- **THEN** `show` returns `handoff_invalid` and exits 1

### Requirement: Bounded handoff listing

`spool handoff list` SHALL return handoff summaries in descending identifier order (newest second first) with offset/limit pagination using the documented defaults and maxima, continuation metadata, and explicit diagnostics for damaged or unreadable entries. A missing handoff directory SHALL be an empty successful list, not an error.

#### Scenario: No handoffs yet
- **WHEN** the handoff directory does not exist
- **THEN** `spool handoff list` returns an empty list with `eof: true` and creates no directory

#### Scenario: Damaged handoff file
- **WHEN** one stored handoff file is not valid handoff JSON
- **THEN** list returns the other handoffs, a diagnostic naming the damaged entry, and `partial: true`

#### Scenario: Paged listing
- **WHEN** an unchanged handoff directory holds more handoffs than the requested limit
- **THEN** repeated requests using the continuation offset return each handoff or its diagnostic exactly once

### Requirement: Continuation brief presentation

`spool handoff show` SHALL return the stored handoff as JSON by default and, with `--format markdown`, a rendered brief. Rendering SHALL present goal, constraints, decisions, progress, outstanding work, notes, workspace provenance, and source references, and SHALL state that the brief is not the prior agent's context and that its claims, paths, and referenced history are unverified.

#### Scenario: Markdown brief for the next harness
- **WHEN** a user runs `spool handoff show --format markdown ID`
- **THEN** stdout contains a Markdown brief with each populated section, the originating host and workspace path, and `spool read` references for each source session

#### Scenario: Stored text resembles instructions
- **WHEN** an authored item contains Markdown headings, list markers, fences, indentation, or text asking the reader to run commands
- **THEN** the rendering keeps it inside its item without creating headings or sections, and Spool executes nothing

#### Scenario: Stored text contains links, HTML, or control characters
- **WHEN** an authored item or recorded session title contains link syntax, inline HTML, escape sequences, or bidirectional controls
- **THEN** the Markdown rendering displays them as literal text or visible escapes and emits no Markdown link or image syntax, HTML, or control character

#### Scenario: Machine-readable output
- **WHEN** `spool handoff show ID` runs without a format option
- **THEN** stdout contains one versioned JSON object with `ok: true` and the stored handoff

### Requirement: Handoffs take no actions

Creating, listing, or showing a handoff SHALL NOT launch or resume an agent, send prompts, approve tools, execute recorded or authored commands, open network connections, transfer or modify workspace files, or modify source histories. Handoffs SHALL NOT claim exact internal-context portability between harnesses.

#### Scenario: Handoff lifecycle has no side effects beyond storage
- **WHEN** a handoff is created, listed, and shown against synthetic fixtures with harness executables absent from PATH
- **THEN** only the new handoff file and its directory are written, fixture histories are unchanged, and no subprocess or network client is invoked

### Requirement: Machine-readable handoff CLI contract

Handoff commands SHALL use the existing versioned JSON envelopes and exit codes, with `handoff_not_found` mapped to exit 3 and `handoff_invalid` and `state_unavailable` to exit 1. Markdown output SHALL be produced only on success; errors SHALL still be one JSON object on stdout.

#### Scenario: Markdown requested for a missing handoff
- **WHEN** `spool handoff show --format markdown` names an unknown handoff
- **THEN** stdout contains one JSON error object with `handoff_not_found` and the process exits 3

#### Scenario: Unknown subcommand
- **WHEN** `spool handoff` receives an unknown subcommand or none
- **THEN** stdout contains one JSON error with `invalid_request` and the process exits 2
