# Proposal

## Why

Spool currently prints help and a version, but cannot retrieve the history that makes switching agents useful. A read-only local session catalog and reader provide the first independently useful slice and the core that later SSH and MCP interfaces can expose without duplicating harness parsing.

## What Changes

- Add local source configuration and adapters for Pi, Claude Code, and Codex native session files on macOS and Linux. Read saved history without requiring an agent process or runtime.
- Add `spool list` with harness filtering and bounded pagination, returning stable opaque session references and source metadata.
- Add `spool read <session-id>` with bounded record pagination, preserving native record data, source provenance, and branch/compaction relationships rather than claiming to reconstruct hidden model state.
- Return versioned JSON on stdout, structured errors, and explicit source/record diagnostics. Distinguish missing default history, configured-source failures, unsupported formats, and invalid requests.
- Keep history access read-only and limited to discovered files inside selected source roots. Never execute transcript content or change source files.
- Non-goals: SSH transport and host configuration, MCP, full-text search, automatic handoff summaries, project checkout mapping, launching agents, code transfer, synchronization, persistent indexing, and exact cross-harness session import. These remain subsequent changes; this slice establishes their local data-access foundation.

## Capabilities

### New Capabilities

- `session-catalog`: Configure local harness roots and discover paginated session metadata with stable identities and explicit source diagnostics.
- `session-reading`: Retrieve bounded, provenance-bearing native history records safely, including incomplete and unsupported record handling.

### Modified Capabilities

None. There are no existing product specifications.

## Impact

The existing Go CLI gains list/read commands and configuration options while preserving help and version behavior. New internal source adapters, a shared application service, and response types will support later transport adapters. No SSH connections, MCP dependency, source-history mutation, Node runtime, or dependency on Agentmux/dotfiles is introduced. Tests use synthetic native-format fixtures only. The existing placeholder Go module name and publishing decisions remain unchanged.
