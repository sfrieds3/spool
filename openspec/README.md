# OpenSpec workflow

This repository's `openspec/` is the authoritative planning root; work from the repository root without a `--store` flag. Proposal artifacts are planning, not approval to implement. CI installs the pinned CLI version from `.github/workflows/ci.yml` with Node 24; use a compatible Node version (at least 22.12) locally.

## References and squash merges

Add the appropriate trailer to every commit, for example `git commit -m 'feat: add local session access' --trailer 'OpenSpec-Change: add-local-session-access'` (use `none` for unrelated work). Put a true Git trailer block at the **end of the pull request body**. CI parses the proposed squash message formed from the PR title, a blank line, and the PR body with `git interpret-trailers --parse`. Each value is one lowercase hyphenated slug naming an active or archived change. Use repeated lines for multiple changes, or `none` alone for unrelated work:

```text
OpenSpec-Change: add-local-session-access
OpenSpec-Change: add-ssh-hosts
```

```text
OpenSpec-Change: none
```

These are references, not claims that the change is complete. CI checks the PR title/body rather than individual commits. Squash-merge the PR and preserve its trailers in the **actual** squash message; the merge dialog can override the checked text, so the person merging must verify it. If using a merge commit or rebase instead, put the trailers on commits that actually land on main; PR body text alone is not preserved by those methods. Merge commits without trailers are ignored, while referenced inner commits are scanned. Direct pushes bypass PR checks and must carry their own trailers. The only missing-trailer PR exemption is the same-repository release-please PR authored by `github-actions[bot]`; other bots are not exempt. Archive commits pushed by CI use `none`.

## Archive automation

After the main-push Go checks, cross-platform builds, and OpenSpec checks succeed, CI scans every commit in the push range and historical main commits for trailers. Historical scanning recovers references from failed or superseded runs; keep archived change directories so old references remain resolvable. Unknown slugs fail the archive job. A referenced active change is skipped until its planning artifacts are done, its apply state is not blocked, and all of its nonzero tasks are complete. Planning `isComplete` alone is not implementation completion. Ready changes must pass strict validation and the noninteractive `openspec archive --yes`, without skipping spec updates or validation; reported archive warnings fail the job. A failure publishes nothing. Already archived changes are idempotent.

When anything was archived, CI commits only `openspec/**` as `github-actions[bot]` with `OpenSpec-Change: none` and pushes it directly to `main`. There is no archive PR to review; review archives in the main history and fix mistakes with a normal follow-up change. Stale pushes are skipped, and a push rejected because main advanced is recovered by the next successful run. The push uses `GITHUB_TOKEN`, so it does not trigger another CI run. If branch protection is later enabled on `main`, allow GitHub Actions to bypass it or this push will be rejected.

## Local checks

```sh
python3 -m unittest discover -s scripts/tests
openspec validate --all --strict --no-interactive
openspec validate --archived --strict --no-interactive
```

`python3 scripts/openspec_ci.py check-pr EVENT_JSON` checks an event payload without writing changes. `archive-push BEFORE AFTER` mutates the checkout: use it only in a disposable clean clone, never in a working tree with personal edits.
