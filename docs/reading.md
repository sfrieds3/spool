# Listing and reading history

## Pagination

Both commands accept `--offset` (default `0`) and `--limit` (default `50`, maximum `500`). Out-of-range values return `invalid_request`; Spool never silently changes them. Every successful response includes:

- `offset` and `limit`: the request.
- `next_offset`: the offset for the next page, or `null`.
- `eof`: `true` when the page reached the end of the input and `next_offset` is `null`.
- `partial`: `true` when any diagnostic was reported.
- `diagnostics` and `omitted_diagnostics`: at most 64 diagnostics, plus a count of the rest.

Each serialized response is at most 8 MiB.

### List

List offsets count catalog rows: candidate files and skipped filesystem entries, ordered by source ID and then bytewise by source-relative path. A candidate that cannot be described (unsupported format, missing identity, oversized metadata, symbolic link, special file) is reported as a diagnostic at its row instead of a session. Listing reads at most the first 1 MiB of each file for metadata. When a file is larger, `metadata_complete` is `false` and a later title may be missing. A page ends early, with `next_offset`, when the next session would exceed the response limit.

### Read

Read offsets count physical lines already consumed, including skipped malformed or oversized lines. Records carry their 1-based physical `line`, so the record on line `n` is returned for any `offset < n`. Pass `next_offset` unchanged to continue.

- A line over 4 MiB is omitted with an `oversized_line` diagnostic.
- A record that cannot fit in a response by itself is omitted with `record_too_large`.
- A record that would push the current page over 8 MiB is deferred: the page ends before it and `next_offset` points at it.
- A complete line that is not a JSON object is skipped with `malformed_line`. A malformed required header or identity fails the read with `invalid_history`.
- A final line without a newline is withheld with `incomplete_tail`, `eof: false`, and a `next_offset` that retries it. Retrying before the writer finishes returns the same offset with no records, so callers must not loop on it automatically.
- At a normal end of file, `eof` is `true` and `next_offset` is `null`.

## Native file order

Read responses label content `untrusted_history` and the view `native_file_order`. Records are returned in the order they appear in the file, including headers, branch records, summaries, compaction entries, tool calls/results, and unknown types. The `native` field holds the original JSON object byte-for-byte (outer whitespace trimmed), so unknown fields and number precision are preserved. `type`, `subtype`, `id`, `parent_id`, and `timestamp` are extracted when the native record has them and are `null` otherwise. Spool does not select an active branch, apply compaction, or claim to reconstruct the context another harness used.

Historical paths, including `cwd` and `original_path`, are preserved as recorded on the source machine. They do not imply that the path exists now.

## Unsupported formats

Files outside the [supported formats](formats.md) are reported as list diagnostics and fail `spool read` with `unsupported_format`. A file whose history depends on records outside it is never returned as if its contents were the complete conversation.

## Not a snapshot

Each call rescans the source. Separate calls do not form a snapshot: appended records are naturally readable at later offsets, but rewritten, removed, or added files can shift list rows and read line offsets. After a known change, restart from offset `0`. Metadata such as titles and modification times may change between calls.

## Example

Using the synthetic fixtures with a generated configuration:

```sh
root="$PWD/testdata/sessions"
sed "s#@ROOT@#$root#" testdata/fixtures-config.json.in > /tmp/spool-fixtures.json
./bin/spool list --config /tmp/spool-fixtures.json --harness pi
ref=$(./bin/spool list --config /tmp/spool-fixtures.json --harness pi | sed -E 's/.*"ref":"([^"]+)".*/\1/')
./bin/spool read --config /tmp/spool-fixtures.json --limit 4 "$ref"
./bin/spool read --config /tmp/spool-fixtures.json --offset 4 "$ref"
```

The first read returns lines 1–4 with `"next_offset":4,"eof":false,"partial":false`. The second returns lines 5–10 with `"next_offset":null,"eof":true,"partial":false`.
