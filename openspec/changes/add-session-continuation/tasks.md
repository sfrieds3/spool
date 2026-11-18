# Tasks

## 1. Configuration and shared continuation boundaries

- [x] 1.1 Extend strict config types/parsing with optional drafting provider/model/thinking/input-budget/timeout settings and defaults as specified in design; verify missing config, omitted sources, explicit sources, invalid values, unknown/duplicate fields, and unchanged legacy behavior in config tests, and document configuration without credentials.
- [x] 1.2 Add shared continuation orchestration interfaces and typed states for scope preparation, drafting, review approval, publication, and launch plans; verify transitions reject implicit launch/publication and stale approvals without invoking real executables, and document trust boundaries in architecture docs.

## 2. Safe full-catalog picker

- [x] 2.1 Add pick dispatch/flags/help, Darwin/Linux terminal probes, stderr-only UI/failures, and inherited-PATH fzf discovery; verify invalid args/harness/config, help without dependencies, /dev/null and independently redirected streams, missing fzf, documented exit mappings, and unchanged list/read JSON tests.
- [x] 2.2 Compose shared List pagination through every next_offset with deduplication/cancellation and safe stderr diagnostics; verify real generic fixtures with more than 500 sessions plus fake empty/short nonterminal pages, nonadvancing offsets, partial/all-source/later-page failures, and document memory/order/non-snapshot limitations.
- [x] 2.3 Implement sanitized five-field rows, hidden/unsearchable ref, cleared fzf defaults, bounded exact-row selection validation, and same-executable reference-only preview; verify hostile labels/control/bidi injection, altered/multiple/oversized selections, duplicate labels, and actual preview command execution with hostile static paths using fake fzf.
- [x] 2.4 Add the action menu, bounded inspect-and-return flow, and --ref escape hatch; verify browsing/inspection/ref-only cause no model/agent launch and revalidate refs, and document the new Enter/action and stdout/stderr contracts.

## 3. Explicit source scope and constrained Pi drafting

- [x] 3.1 Prepare a bounded single-session input via shared Read with exact included physical lines, diagnostics, omission/non-EOF coverage, target, and user goal; verify budget accounting, never splitting records, incomplete/nonadvancing tails, malformed/oversized records, and non-snapshot caveats with generic fixtures, and document the disclosure screen.
- [x] 3.2 Require source/scope/provider/model/cost/privacy confirmation before spawning Pi; verify declined confirmation invokes no child/provider and changing target/goal/scope invalidates it, and document that configured drafting is not consent and remote providers may receive secrets.
- [x] 3.3 Add the injectable direct-exec Pi print-mode adapter with explicit model selection, no tools/resources/context/MCP/session persistence, private isolated cwd, stdin input, bounded stdout/stderr, timeout/cancellation, and cleanup; verify exact argv/environment/input with a fake Pi including nonzero/token-limit/timeout/overflow cases and no source workspace or transcript data in argv/logs.
- [x] 3.4 Strictly validate model-only content fields, reject invented source/workspace/target fields, attach trusted source/scope notes, and validate assembled version-1 briefs with existing ParseBrief; verify malformed/duplicate/unknown/oversized/fenced JSON and injection fixtures publish nothing, and document output/failure bounds.

## 4. Review and immutable publication

- [x] 4.1 Implement safe rendered draft review with explicit edit/approve/cancel, trusted VISUAL/EDITOR argv handling without shell expansion, private bounded non-symlink draft files, and revalidation of edited content/application-owned metadata; verify fake-editor edits, rejected edits, source injection, cancellation, file replacement, cleanup, and exact-byte approval invalidation.
- [x] 4.2 Reuse handoff Create for approved publication only, preserving existing version-1 schema and immutable storage while adding visible generation/scope/unverified notes; verify source deletion/replacement/unsupported-format races, state failures, safe rendering, no transcript embedding, save-only success, and byte-stable legacy create/list/show behavior.
- [x] 4.3 Update handoff/security/architecture docs to scope existing inert-operation promises and describe approved drafting/publication separately; verify doc-contract tests and generic fixture snapshots show browsing/drafting/review do not mutate histories or publish unapproved handoffs.

## 5. Native resume and fresh-session launch

- [x] 5.1 Implement a conservative native-resume capability resolver for validated local Pi files only, with exact source-file identity, supported format/kind, existing matching recorded cwd, and unavailable reasons for other mappings; verify ambiguous IDs, missing cwd, Claude subagents, Codex archives, source replacement/identity/cwd changes during confirmation with immediate pre-spawn revalidation and no child start, and generic supported Pi cases, and document the initial support matrix.
- [x] 5.2 Implement the separately confirmed Pi native-resume runner with explicit --session path and no synthetic prompt, fork, or approval-bypass flags; verify fake Pi receives exact argv/cwd and cancel/unavailable dependencies start nothing, and document that resumed harness activity may append to its own history.
- [x] 5.3 Add separately confirmed new-session Pi/Claude/Codex adapters using fixed prompts pointing to a private approved rendered brief file, direct argv, confirmed cwd, inherited terminal streams, and normal harness permissions; verify fake executables read the exact approved brief, no transcript/credential appears in argv, no resume/bypass flags appear, no wrong target/cwd launches, child exit/cancel cleanup works, interactive child terminal output is permitted while Spool UI stays on stderr, and failures preserve the published handoff.
- [x] 5.4 Update README and continuation docs with find/resume/handoff/review/save/launch examples, optional dependencies, provider privacy, original-versus-fresh session semantics, manual recovery, and no exact context/workspace portability promise; verify help and doc-contract tests match all shipped paths without real private histories or model calls.

## 6. Integration gates

- [x] 6.1 Run mise run fmt, mise run test, mise run vet, mise run build, race tests, and Darwin/Linux amd64/arm64 builds; verify tests require only generic fixtures and fake fzf/Pi/editor/target processes, existing commands remain usable without integrations, and no MCP/remote changes are required on this baseline.
- [x] 6.2 Run strict OpenSpec validation plus applicable ripwire change-check/quality-bar checks and map each scenario to focused coverage; verify cancellation, source/terminal injection, disclosure/approval separation, and all failure-state recovery paths have no unresolved findings.

## Verification results

- Passed `mise run fmt`, `mise run test`, `mise run vet`, `mise run build`, `go test -race ./...` through Mise, and Darwin/Linux amd64/arm64 cross-builds. Strict change/all OpenSpec validation and archive automation tests also passed.
- Catalog, framing, and source races: `TestCatalogMoreThan500GenericSessions`, `TestCatalogSparsePagesAndFailures`, `TestPickRejectsUntrustedSelectionsAndFzfFailure`, `TestPickRevalidatesSelectedRefAfterFzf`, and `TestPublicationRevalidatesExactSource` cover pagination, unoffered rows, damaged histories, and source replacement.
- Scope, consent, and publication: `TestScopeExactCoverageAndBudget`, `TestDenseDiagnosticsFitPinnedVersionOneProvenance`, `TestStateBoundariesAndExactByteApproval`, `TestPickDisclosureReviewSaveAndCancellation`, and `TestStrictModelOutput` cover bounded inputs, pre-inference schema feasibility, independent approvals, strict model output, and inert browsing/save-only behavior.
- Process/terminal safety and cleanup: `TestPiAdapterExactContractAndIsolation`, `TestPiAdapterFailsClosed`, `TestPiExactModelPreflightAndCancel`, `TestPrivateDirectoryExclusion`, `TestPromptCancellationWithoutCompetingReaders`, `TestActualPreviewExecutionWithHostileStaticPaths`, `TestPreviewEscapesRealControlsWithoutChangingRead`, and `TestFakeEditorEditsAndRejectsUnsafeFiles` cover isolation, exact model selection, controls, cancellation (including editor exit 130), editor attacks, and cleanup.
- Resume and receiver recovery: `TestNativePiResumeContractAndRaces`, `TestNativeResumeUsesExactFileWithDuplicateNativeIDs`, `TestFreshHarnessLaunchesPrivateExactApprovedBrief`, `TestFailedLaunchPreservesPublishedHandoff`, and `TestReceiverCancellationCleansPrivateFileAndPreservesHandoff` cover exact native identity, separate launches, approved file contents, cancellation, and saved-handoff recovery.
- An additional macOS controlling-PTY smoke test with installed fzf and copied generic fixtures passed selection, inspect, cancel (130), cleared fzf defaults, and no state creation. Automated tests use fake drafting/editor/receiver processes; no private history or real drafting-provider request was used.
- Ripwire contract checks found no incompatible config callers, and quality-delta reported zero gating regressions after consolidating duplicated test helpers. Structural security triage (not a taint proof) found no unsafe-construct/weak-crypto hits; reviewed process sinks use direct argv and explicit confirmations. The sole reported untested seam was an existing ambiguous `Marshal` edge, not a new continuation seam. Independent final review findings on preview controls and editor cancellation were fixed and reverified.
- The configured decision-model verifier was unavailable; the predeclared changed-scope and separate-consent properties were checked manually against the diff and guarded CLI/service transitions instead.

## Workflow follow-up

- Implementation was explicitly authorized and all tasks above are verified. Do not also apply the superseded unpublished picker exploration.
- Use Conventional Commits with a final OpenSpec-Change: add-session-continuation trailer. A PR must include Closes https://github.com/sfrieds3/spool/issues/9 and its final OpenSpec trailer block.
- Close the related local backlog item with the user-requested tdo done command and a concrete change summary only after the verified implementation is complete.
- Leave completed-change archival to validated main-branch CI; do not archive incomplete planning work.
