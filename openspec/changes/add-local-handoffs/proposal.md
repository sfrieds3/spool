# Proposal

## Why

Spool can list and read local Pi, Claude Code, and Codex history, but switching harnesses still means the next agent must reconstruct the goal, constraints, decisions, and remaining work from raw transcripts. A small, explicit continuation brief that points back to the recorded sessions lets work move between harnesses without pretending to port one agent's internal context into another.

## What Changes

- Add `spool handoff create`, which reads a version-1 JSON brief from `--input FILE` (or stdin with `--input -`). The outgoing agent or user authors the content: a title, a required goal, and lists of constraints, decisions, progress, outstanding work, and notes. Spool does not infer, summarize, or call a model.
- At creation, Spool validates the brief strictly and records provenance it can observe: creation time, the local host name, the Spool version, and the workspace path (the creating process's working directory unless the brief supplies an absolute path). Optional author-reported repository details (branch, commit, remote) are stored as reported and labeled unverified. Spool does not run `git` or any other process.
- Briefs may reference source sessions by their opaque `spool list` references. Creation resolves each reference through the existing local history service and snapshots its harness, source, native ID, original path, recorded working directory, and title. Unresolvable references fail creation instead of being stored silently.
- Persist each handoff as one JSON file that Spool never edits or overwrites, under `$XDG_STATE_HOME/spool/handoffs` (default `~/.local/state/spool/handoffs`), created with owner-only permissions. This is Spool's first write path. It refuses a handoff directory that overlaps a harness history root or has insecure permissions.
- Add `spool handoff list` (paginated, newest first) and `spool handoff show ID`. `show` prints the stored JSON by default; `--format markdown` renders a continuation brief for pasting into the next harness. The rendering states that it is a brief, not the previous agent's context, that its claims and any referenced history are untrusted until verified, and that recorded paths describe the originating machine at creation time.
- Implement handoffs in a transport-independent service package so the CLI and a future MCP tool share one implementation. This change does not add an MCP server.
- Non-goals: automatic summarization or extraction from transcripts, launching or resuming agents, sending prompts, approving tools, executing recorded commands, transferring or verifying workspace changes, editing or deleting handoffs through Spool, remote hosts and SSH, synchronization, and target-harness-specific import formats.

## Capabilities

### New Capabilities

- `session-handoff`: Authoring, storing, listing, and presenting local continuation briefs that capture goals, constraints, decisions, progress, outstanding work, workspace provenance, and references to source sessions, without transferring agent state or performing actions.

### Modified Capabilities

None. List and read behavior is unchanged; handoff creation only reads history through the existing reference resolution.

## Impact

- New `internal/handoff` package with typed create/list/show requests and responses, strict brief validation, state-directory resolution, confined storage, and Markdown rendering. It calls `internal/history` to resolve session references.
- Shared `internal/strictjson` and `internal/safefile` helpers extracted from config and history without behavior changes, and a small `internal/history` addition to describe one session by reference without reading its records, reused by handoff creation.
- `cmd/spool` gains `handoff create|list|show` routing, help text, and the `handoff_not_found` (exit 3), `handoff_invalid` (exit 1), and `state_unavailable` (exit 1) error classes.
- New `docs/handoffs.md`; README and architecture docs updated to describe the handoff write path and its safety boundaries. No configuration schema change and no new dependencies.
- Tracked by https://github.com/sfrieds3/spool/issues/6.
