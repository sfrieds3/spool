# Supported session formats

Spool reads native session files directly. Support is defined by the record structures below and the synthetic fixtures that test them, not by harness version numbers: a future release that keeps these structures should work, and one that changes them may not. New record types inside a supported file are returned as raw native records.

## Matrix

| Harness | Files | Identity | Title | Supported | Unsupported | Fixture |
| --- | --- | --- | --- | --- | --- | --- |
| Pi | Any `*.jsonl` beneath the source root | Line 1 header `{"type":"session","version":3,"id":...}`; `cwd` and `timestamp` from the header | Latest `session_info.name` | Version 3 sessions, including custom non-UUID IDs, message/tool records, branches, `branch_summary`, `compaction`, `custom`, and unknown entry types | Legacy version 1/2 sessions (Pi migrates them on load; Spool does not), missing or non-session first lines | [`testdata/sessions/pi`](../testdata/sessions/pi) |
| Claude Code | Any `*.jsonl` beneath the projects root, including `<session>/subagents/agent-*.jsonl` (kind `subagent`) | First record with a `sessionId`; `cwd` and `timestamp` from records with that ID | Latest `custom-title`, else latest `ai-title` | Self-contained project and subagent transcripts with `uuid`/`parentUuid`, `isSidechain`, `summary`, `compact_boundary`, tool use/result records, and unknown types | Files containing a `fork-context-ref` record, whose inherited prefix lives in another transcript; files without any `sessionId` | [`testdata/sessions/claude`](../testdata/sessions/claude) |
| Codex | `rollout-*.jsonl` beneath the active `sessions` and `archived_sessions` roots | Line 1 `session_meta` with `payload.id`; `payload.cwd` and `payload.timestamp` | Latest `event_msg` `thread_name_updated` | Self-contained rollouts with `session_meta`, `turn_context`, `response_item` (messages, function calls, outputs), `event_msg`, `compacted`, and unknown types; legacy forks that only record `forked_from_id` | `session_meta` with `history_mode` other than `legacy`, `history_base`, `forked_from_ordinal_exclusive`, or `subagent_history_start_ordinal` (paginated or inherited history held outside the file); first lines that are not `session_meta` | [`testdata/sessions/codex`](../testdata/sessions/codex), [`testdata/sessions/codex-archive`](../testdata/sessions/codex-archive) |

Spool never reads SQLite state databases, fork-context parents, attachments, or other files referenced by a transcript, and never reconstructs an active conversation. Unsupported files appear as list diagnostics and fail `spool read` with `unsupported_format`.

## Fixture provenance

All fixtures are synthetic and contain only generic example paths and content. They were written to match:

- Pi: the packaged `session-format.md` documentation of `@earendil-works/pi-coding-agent` 1.5.0 (session header, entry base, entry types, tree structure, version history).
- Claude Code 2.1.311: record shapes observed in the installed executable's transcript writer (`parentUuid`, `isSidechain`, `sessionId`, `cwd`, `uuid`, `timestamp`, `summary`/`leafUuid`, `compact_boundary`, `custom-title`, `ai-title`, nested `subagents/agent-*.jsonl`, and `fork-context-ref` with `parentSessionId`/`parentLastUuid`). Claude does not ship a complete stable transcript schema.
- Codex 0.164.0: rollout structures documented in the installed executable (`session_meta.payload.id`, `turn_context`, `event_msg`, `response_item`, `compacted`, active/archived roots) and session metadata fields for paginated or inherited history (`history_mode`, `history_base`, `forked_from_id`, `forked_from_ordinal_exclusive`, `subagent_history_start_ordinal`).

No personal transcripts were used. Compatibility with every installed release is not claimed; the adapters' tests in [`internal/harness`](../internal/harness/harness_test.go) and [`internal/history`](../internal/history) define what is supported.
