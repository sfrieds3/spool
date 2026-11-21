# Proposal

## Why

Finding old history is useful primarily when it helps someone continue work. Spool needs a terminal workflow that finds a saved session, offers native resume where supported, or prepares a reviewable brief for a fresh session in the same or a different harness.

## What Changes

- Add `spool pick [--config FILE] [--harness pi|claude|codex] [--ref]`. Enter opens an action menu rather than dumping JSON: inspect, resume in the original harness where supported, or prepare a handoff for Pi, Claude Code, or Codex. `--ref` remains an explicit reference-only escape hatch.
- Discover all sessions through shared history application logic and every `next_offset`, with safe fzf rows, a bounded reference-only read preview, and stderr partial-source diagnostics. Preserve list/read JSON contracts.
- Add optional headless Pi drafting, selected by a `drafting` block in Spool's existing config file under `$XDG_CONFIG_HOME/spool`, defaulting to `~/.config/spool/config.json`. Select provider/model explicitly; credentials remain with Pi. Browsing does not require Pi or invoke a model.
- Before drafting, show the source, bounded input scope, selected provider/model, and disclosure/cost warning and require confirmation. Run a fresh, tool-free, nonpersistent Pi invocation; historical text is input data, never authority for tool use or launch parameters.
- Review/edit and explicitly approve the generated brief before immutable publication. Offer a separate, confirmed launch into the chosen harness, with an independently confirmed local working directory. Canceling review neither publishes nor launches. Cross-harness continuation starts a new session, not restored internal state.
- Native resume is a separately confirmed action, not a side effect of browsing. Start with validated local Pi sessions; Claude/Codex native resume is capability-gated until their exact source/session identity mapping is proven. All three targets can receive an approved brief in a fresh interactive session through tested CLI adapters.
- Non-goals: SSH/cloud-session access, transcript replication, background agent orchestration, automatic tool approval, workspace transfer, transcript execution, exact context portability, and a mandatory Pi/Node dependency for existing Spool commands.

## Capabilities

### New Capabilities

- `session-continuation`: Safe interactive discovery followed by explicit native resume or model-drafted, reviewed handoff and separately authorized local launch.

### Modified Capabilities

- `session-handoff`: Scope the no-generation promise to existing authored create behavior, allowing a separate explicitly authorized drafting workflow without changing create/list/show semantics, immutable storage, or stored schema version.

## Impact

- Add shared continuation orchestration with injectable catalog, drafter, review, and harness-launch boundaries; keep fzf/editor UI in `cmd/spool`. Reuse history List/Read/Describe and handoff ParseBrief/Create/Render.
- Extend strict config parsing without losing omitted-sources defaulting. Add optional inherited-PATH dependencies fzf, Pi for drafting, a user-selected editor, and the chosen launch harness. Spool itself remains a standalone Go binary.
- Update security/architecture/configuration/handoff docs and generic fixture tests with fake fzf, Pi, editor, and harness executables. No implemented MCP/remote interface exists on this baseline; shared logic enables later interfaces without duplicating behavior.
- This supersedes the local, unpublished `add-session-picker` exploration; it is not an implementation dependency and must not be applied alongside this change. This proposal-only PR publishes only `add-session-continuation` artifacts. No product behavior is implemented by merging planning artifacts.
