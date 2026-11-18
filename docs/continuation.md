# Find and continue local work

`spool pick` finds saved local history and offers an explicit next action. It does not restore exact internal context or transfer code, branches, processes, or workspace changes. Source histories and generated assertions are untrusted and do not prove current workspace state.

```sh
spool pick
spool pick --harness pi
spool pick --harness claude --config /absolute/path/config.json
spool pick --ref
```

## Find, inspect, or emit a reference

On macOS/Linux, pick requires both terminal stdin and terminal stdout (not `/dev/null` or separately redirected streams), and `fzf` found through inherited PATH. Help requires no optional dependency. All Spool UI, diagnostics, and inspection use stderr. Normal mode emits no stdout of its own; `--ref` emits exactly the selected, revalidated opaque ref and LF. After separately authorized interactive launch/resume, the child inherits stdin/stdout/stderr and can emit terminal output normally. Existing list/read/handoff JSON contracts are unchanged and work without any integration installed.

Fzf Enter opens an action menu: inspect one bounded native-file-order read page and return, resume original where supported, prepare a handoff, or cancel. Inspect is escaped JSON labeled untrusted history and never invokes a model. `--ref` has no action menu and no drafting/launch. Labels are sanitized five-field rows (hidden ref, harness, recorded start time, cwd, title); refs are neither displayed nor searched. Controls, line separators, and bidi controls cannot create rows or terminal commands. Selection must match an offered row exactly. Fzf defaults are cleared, ANSI interpretation is disabled, and the reference-only preview uses this same Spool executable's private `__pick-preview` presentation path over the first bounded shared read page, with the same absolute explicit config path and `--` before the ref. Preview additionally escapes bidi, invisible controls, and line separators without changing ordinary `read` JSON; it does not paginate or decode escapes.

Catalog discovery follows every `next_offset`, even empty/short intermediate pages, deduplicates refs in first-seen source/path order, and surfaces deduplicated safe partial-source diagnostics. Any failed page aborts before fzf, including a failure after earlier successful pages. All-source failures are operational failures, not empty results. The entire catalog is collected before selection; memory grows with catalog size, with no hidden 500-row cap. Ordering is catalog order, not newest-first. Changing history is not a snapshot and may cause omissions. Ctrl-C cancels scans, processes, and prompt reads without hidden retries.

## Prepare a reviewed handoff

Choose prepare handoff, a fresh target (`pi`, `claude`, or `codex`), your continuation goal, and an independently confirmed existing absolute local workspace. Recorded cwd is unverified metadata, not a command or automatic launch directory. [Configuration](configuration.md#optional-continuation-drafting) requires optional `drafting.provider` and `drafting.model`; absence does not break browsing. Optional Pi must be installed only when drafting is requested. The drafting model is independent of whichever model the later receiving harness uses.

Spool prepares exactly one source from physical-line offset zero through shared Read. It includes whole native records only, counts fixed system prompt plus serialized framing/metadata/diagnostics against the configured input byte budget (default 128 KiB, maximum 1 MiB), and stops at the budget, EOF, incomplete tail, or nonadvancing continuation. The disclosure screen names source, target, goal, exact included physical-line ranges, diagnostic codes/lines, omitted-diagnostic count, EOF/non-EOF coverage, encoded size, provider, and model. Oversized/malformed records and incomplete or omitted history are not silently presented as complete context. Byte limits are not token limits; a provider context overflow fails visibly without truncation/retry or a fallback model. Reads are not a snapshot.

**Configuration is not consent. History may contain secrets and private code; no redaction is promised. A remote provider can receive it and charge for it.** Only typing `yes` on the disclosure screen authorizes transmission for that scope. Declining invokes no model child/provider, publishes nothing, and exits 130. Changing target/goal/scope requires new disclosure. The installed Pi executable and provider configuration are trusted integrations, not an OS sandbox; provider credential commands/custom endpoints retain their own security implications.

### Constrained drafting contract

Spool runs a fresh private isolated cwd outside all configured history roots (including disabled/aliased roots), recorded and confirmed workspaces. It directly invokes Pi through inherited PATH with `--print`, explicit provider/model/optional thinking, `--no-session`, `--no-tools`, `--no-mcp`, `--no-extensions`, `--no-skills`, `--no-prompt-templates`, `--no-themes`, `--no-context-files`, `--no-approve`, and `--offline`. A fixed system prompt and explicit empty `--append-system-prompt` prevent default system/appended prompt discovery. Before inference, the same resource-disabled offline Pi performs a bounded `--list-models` exact provider/model table check; Spool refuses absent IDs rather than letting Pi select a fuzzy/fallback model. CLI drift fails closed. Offline suppresses automatic catalog/package network activity, not the separately authorized model request. Credentials remain in Pi's normal trusted provider setup/inherited environment, never Spool config or arguments.

Historical input is JSON-encoded untrusted data sent only on stdin, not argv or logs. Stdout is bounded to 256 KiB and stderr to 32 KiB; nonzero exit (including token-limit/aborted output), overflow, cancellation, and configured timeout (default 120 seconds, maximum 600) fail without publication. Child stderr is captured but not echoed because it can contain secrets. Model output must be one strict UTF-8 JSON content object with required title, goal, constraints, decisions, progress, outstanding, and notes fields. Duplicate/unknown/missing fields, null content lists, invented sessions/workspace/repository/target/executable fields, fences, and malformed or oversized output fail without JSON repair or another hidden model call. Spool attaches trusted source, scope, model caveats, and confirmed workspace and validates version 1 through existing ParseBrief. Model assertions remain unverified.

## Review, edit, approve, save

Spool shows a safe rendered unverified draft and offers edit, approve these exact bytes, or cancel. Editing uses a deliberately configured trusted VISUAL, else EDITOR, parsed as quoted argv without a shell or expansion; it is never chosen from history. With no editor, approve/cancel remain available (configure an editor before selecting edit). Editor files are bounded private `0600` JSON in a `0700` directory outside histories. On return, symlink/nonregular/outside-container replacements and insecure files fail; valid atomic replacement inside the private container is allowed. Edits must preserve application-owned sessions, workspace/repository metadata, and drafting/scope notes; content can change after revalidation. Any changed bytes invalidate earlier approval and need explicit fresh approval. Cancel/invalid edits publish and launch nothing.

Publication revalidates the selected source, including original file identity/content, format, and cwd, then reuses immutable `handoff.Create`. Missing/replaced/unsupported source or unavailable/insecure state fails without publishing. Storage stays version 1, with visible model-drafting/scope/unverified notes rather than authenticated provenance or copied transcript records. The drafter is instructed not to reproduce transcripts or secrets; human review remains necessary and cannot guarantee truth or confidentiality. Existing authored create still stores supplied content literally and calls no model.

After publication Spool prints the handoff ID and offers save-only or separately confirmed fresh launch. Declining launch, EOF, or choosing save-only is success with the saved handoff intact. Post-publication cleanup/sync warnings remain successful publication, never a request to blindly retry. Failed launch preserves the handoff and exits operational failure. Manual recovery does not require any target harness:

```sh
spool handoff list
spool handoff show HANDOFF_ID
spool handoff show --format markdown HANDOFF_ID
```

All temporary draft/input/brief directories are removed on normal completion/cancel. They are not encrypted; crashes can leave private temporary files requiring manual cleanup. Same-user processes concurrently racing trusted directory ancestors are outside the filesystem trust model.

## Original native Pi resume

Initial native Pi resume supports only validated local Pi version-3 regular session files whose exact source/path/native identity is known and whose recorded cwd is an existing directory matching the user-confirmed workspace exactly. The menu hides resume for unsupported harnesses/kinds/missing workspaces; unsupported mappings explain why handoff is available instead. Partial/ambiguous native IDs and most-recent lookups are never used. Claude subagents and Codex archives remain readable but native Claude/Codex resume is deliberately unavailable.

After showing the exact executable, original path, and confirmed cwd, another `yes` runs `pi --session ABSOLUTE_SESSION_PATH` directly with no synthetic prompt, fork, resume selector, or permission-bypass flags. Immediately before spawn, Describe and confined regular-file checks revalidate identity, supported format, inode/content, and cwd. Replacement/change invalidates confirmation without starting a child. There remains a residual pathname race between final validation and Pi opening its session file; Spool neither claims to remove it nor silently copies/forks the source. The resumed harness may append to its own original history under normal permissions. Spool reads themselves remain read-only.

## Fresh Pi, Claude, and Codex receiving sessions

A separate launch confirmation names target executable, target harness, confirmed cwd, and approved immutable handoff ID. Changing any invalidates the plan. Direct argv is `pi -- FIXED_PROMPT`, `claude -- FIXED_PROMPT`, or `codex -- FIXED_PROMPT`, never continue/resume or approval-bypass flags. The short fixed prompt points to a private `0600` rendered approved brief file outside all configured roots and asks the receiver to verify workspace state. It does not place transcript/brief/credential contents in argv. Spool awaits the child with inherited terminal streams, keeps the file alive throughout, then cleans up. Normal target authentication, project trust, and permission checks apply; Spool never automatically grants access or approves tools. Failure to ingest the file safely leaves the saved handoff for manual use, not a fallback receiver or new permissions.

A fresh session is not an import of Pi/Claude/Codex internal state. No exact context or workspace portability promise is made. The receiving harness can create its own new history and take actions only under its normal trust/permission behavior.

## Exit codes

| Exit | Meaning |
| --- | --- |
| 0 | Completed action, emitted ref, successful native resume/receiver exit, or save-only/declined post-publication launch |
| 1 | Operational failure, empty/no-match catalog, failed pagination/model/editor/child |
| 2 | Invalid arguments/config or stdin/stdout not independently terminals |
| 3 | Selected source session no longer exists or changed native identity |
| 130 | Picker cancellation, Ctrl-C, declined model disclosure, or canceled unapproved draft review |
