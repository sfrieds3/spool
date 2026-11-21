# Session continuation spec delta

## Purpose

Help users find recorded local work and continue it through an explicitly selected native session or a reviewed, source-referenced handoff to a fresh agent session.

## ADDED Requirements

### Requirement: Interactive discovery and action choice

`spool pick` SHALL accept optional config, harness filter, and reference-only mode. Normal selection SHALL offer inspect, supported native resume, or prepare handoff; it SHALL NOT implicitly draft, publish, or launch. Existing list/read JSON contracts SHALL remain unchanged.

#### Scenario: Find without taking action
- **WHEN** a user selects a session in normal picker mode
- **THEN** Spool shows available actions without launching an agent or contacting a model
- **AND** inspecting shows bounded, provenance-bearing, untrusted native history rather than claiming restored context

#### Scenario: Reference-only mode
- **WHEN** a user accepts a session with `--ref`
- **THEN** Spool revalidates it and emits only its exact ref plus newline, with no action menu, model invocation, or launch

### Requirement: Complete local catalog and partial diagnostics

Discovery SHALL use shared history logic and follow every next_offset, including empty intermediate pages, until exhausted. It SHALL deduplicate references in catalog order and surface partial-source diagnostics on stderr. Local discovery SHALL NOT initiate network or SSH access.

#### Scenario: Large and sparse catalogs
- **WHEN** more than 500 sessions span pages including an empty nonterminal page
- **THEN** all unchanged available references are offered once using returned continuation offsets, not returned row counts

#### Scenario: Failed versus empty sources
- **WHEN** some sources fail but another is usable
- **THEN** usable sessions remain selectable with safe source diagnostics
- **WHEN** all requested sources fail or a later page request fails
- **THEN** Spool reports operational failure rather than a complete empty or silently truncated catalog

#### Scenario: Later page request fails
- **WHEN** one catalog page succeeds and the next page request fails
- **THEN** Spool exits 1 before starting fzf and does not offer previously accumulated rows

### Requirement: Safe fzf presentation and preview

The picker SHALL use one row per session with hidden ref then harness, started_at, cwd, and title. Only visible fields SHALL be displayed/searched. Controls, line separators, and bidi controls SHALL be removed from display and diagnostics. Preview SHALL invoke only the current Spool executable's bounded read on the ref with the same config.

#### Scenario: Hostile labels and records
- **WHEN** metadata contains tabs, newlines, escapes, or shell-looking text
- **THEN** it remains inert single-row data and never becomes shell arguments or commands
- **AND** preview retains JSON-escaped untrusted-history/provenance labels without automatic pagination

#### Scenario: Untrusted selection and fzf defaults
- **WHEN** fzf returns multiple rows, altered rows, or an unoffered reference
- **THEN** Spool rejects it without reading or launching that selection
- **WHEN** inherited fzf defaults request hooks or alternate framing
- **THEN** Spool ignores those defaults and preserves single-selection and reference-only preview behavior

### Requirement: Optional drafting configuration

Spool SHALL accept optional strict drafting configuration in its normal config file, defaulting to ~/.config/spool/config.json and respecting XDG_CONFIG_HOME. Drafting SHALL require explicit provider/model selection and use Pi's credential configuration, not store API keys in Spool config. Omitted drafting configuration SHALL leave existing commands usable without Pi.

#### Scenario: Configure model without source overrides
- **WHEN** a valid config has version and drafting configuration but omits sources
- **THEN** Spool preserves drafting settings and uses default history roots

#### Scenario: Missing model or dependency
- **WHEN** drafting is requested without configured provider/model or installed Pi
- **THEN** Spool explains the missing prerequisite without contacting a fallback model, publishing a handoff, or launching a receiver

### Requirement: Explicit model disclosure and bounded source scope

Drafting SHALL require confirmation of the selected source, intended continuation goal, target harness, provider/model, and bounded input scope before model transmission. Spool SHALL warn about secrets and provider costs, label incomplete/omitted history, and never silently claim a complete context or current workspace verification.

#### Scenario: Source input exceeds the bound
- **WHEN** selected history exceeds the documented drafting input budget or has damaged/incomplete records
- **THEN** the reviewable scope identifies included physical lines, diagnostics, and omissions before confirmation and in the resulting draft review

#### Scenario: Decline disclosure
- **WHEN** a user declines the model-transmission confirmation
- **THEN** no Pi drafting process starts and no history is sent to a model

### Requirement: Constrained headless drafting

Drafting SHALL use a fresh nonpersistent headless Pi process with no tools, MCP, extensions, skills, context-file discovery, or source-session resume. History SHALL be delimited untrusted input. Input, output, and runtime SHALL be bounded; failure, invalid model output, or timeout SHALL never publish or launch anything.

#### Scenario: Prompt injection in history
- **WHEN** history asks the drafter to execute commands, change configuration, or choose a different source/launch target
- **THEN** no such operation is available and generated launch parameters or source references are rejected

#### Scenario: Truncated or malformed output
- **WHEN** Pi fails, times out, exceeds output bounds, returns invalid brief JSON, or claims invented references
- **THEN** Spool reports drafting failure without treating partial output as approved content

### Requirement: Review before immutable publication

Generated content SHALL be validated and presented as an unverified draft for review/edit. Explicit approval SHALL apply to the exact validated final bytes and selected source/scope. Edits SHALL be revalidated; cancellation SHALL not publish a handoff. Publication SHALL reuse existing immutable handoff storage and revalidate sources.

#### Scenario: Edit and approve
- **WHEN** the user edits a draft and explicitly approves the validated result
- **THEN** Spool stores the approved brief with the selected source reference and visible drafting/scope caveats, without embedding transcript records or new fields in the version-1 stored schema

#### Scenario: Cancel or invalid edits
- **WHEN** review is canceled or edited content fails validation
- **THEN** no handoff is published, no receiver launches, and original history remains unchanged

### Requirement: Separate receiver launch authorization

After publication Spool SHALL offer save-only or a separately confirmed new interactive session in Pi, Claude Code, or Codex. Launch SHALL identify executable, target, confirmed existing local workspace, and approved brief. It SHALL use fixed argument arrays, preserve harness permission checks, and never derive commands from brief text or history.

#### Scenario: Cross-harness continuation
- **WHEN** a Pi source brief is approved and the user confirms launching Claude Code
- **THEN** a new Claude session receives only the approved rendered brief as context, not a fabricated Claude-native import of Pi state

#### Scenario: Save without launching
- **WHEN** the user chooses save-only or declines launch
- **THEN** the immutable handoff remains available and no receiving agent starts

#### Scenario: Missing target or launch failure
- **WHEN** the chosen harness is missing, the brief exceeds its safe input bound, or launch fails
- **THEN** Spool reports the failure and preserves the published handoff for manual use without retrying a different target

### Requirement: Capability-gated native resume

Native resume SHALL be a separate explicitly confirmed action available only for a proven source-to-session mapping in the original harness. Initial support SHALL cover validated local Pi session files. Unsupported kinds or mappings SHALL offer handoff instead of guessing. The user SHALL confirm the local workspace before launch.

#### Scenario: Resume Pi in Pi
- **WHEN** a validated Pi session is accepted for native resume and the user confirms its session path and existing workspace
- **THEN** Spool invokes Pi's explicit session-file resume without adding a synthetic prompt or granting tool approval

#### Scenario: Native session changes during confirmation
- **WHEN** the selected Pi file is replaced or its identity, supported format, or recorded cwd changes while confirmation is open
- **THEN** immediate pre-launch revalidation invalidates the confirmation and no child starts until the user reviews and confirms the new candidate
- **AND** Spool documents the residual pathname race between its final checks and Pi opening the file

#### Scenario: Readable but not resumable
- **WHEN** a source is a Claude subagent, archived Codex rollout, or another unproven resume mapping
- **THEN** native resume is unavailable with an explanation while preparing a handoff remains possible

### Requirement: Terminal, exit, and privacy boundaries

Picking SHALL require terminal stdin/stdout and optional fzf on inherited PATH on macOS/Linux. Exit codes SHALL be 0 completed action, 1 operational/empty/no-match, 2 invalid use/config/non-TTY, 3 missing selected session, and 130 cancellation. Spool's non-ref UI/results SHALL use stderr without stdout output; explicitly authorized interactive children SHALL inherit terminal streams. No transcript or credential SHALL appear in process arguments or logs.

#### Scenario: Redirected stream and cancellation
- **WHEN** stdin or stdout is not a terminal
- **THEN** Spool exits 2 with a clear stderr diagnostic
- **WHEN** the user cancels the picker, model confirmation, or draft review
- **THEN** Spool exits 130 without selection output or an unapproved launch/publication

#### Scenario: Read-only operations stay inert
- **WHEN** list, read, handoff create/list/show, or picker browsing runs without Pi and target harnesses installed
- **THEN** existing commands retain their contracts and no model or receiving agent starts
