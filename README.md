# Spool

Shared context and history for coding agents, across harnesses and machines.

Spool is an independent Go project, with no dependency on Agentmux or dotfiles. The intended experience is to start work in Pi, continue in Claude Code or Codex, and retrieve relevant history from any configured machine.

## Status

Spool can list and read local Pi, Claude Code, and Codex session history on macOS and Linux. It reads native session files directly; the harnesses do not need to be installed or running. It can also store local handoff briefs for continuing work in another harness. SSH access to other machines, MCP, and search are not implemented yet.

## Local history

```sh
spool list                          # sessions from the built-in local roots
spool list --harness codex --limit 20
spool list --offset 20 --limit 20   # use next_offset from the previous page
spool read SESSION_REF              # first page of native records
spool read --offset 50 SESSION_REF  # continue from next_offset
```

`list` and `read` print exactly one JSON object on stdout. Exit codes are 0 for success (including explicit partial results), 1 for operational failures, 2 for invalid input or configuration, and 3 for a missing session or handoff. `SESSION_REF` is the opaque `ref` value from `list`.

- Configuration and built-in roots: [docs/configuration.md](docs/configuration.md). Without a config file, Spool reads `~/.pi/agent/sessions`, `~/.claude/projects`, `~/.codex/sessions`, and `~/.codex/archived_sessions` (respecting the harnesses' documented directory environment variables).
- Pagination, damaged and live files, and non-snapshot behavior: [docs/reading.md](docs/reading.md).
- Supported formats and known unsupported cases: [docs/formats.md](docs/formats.md). Composite histories that depend on records outside the file, such as paginated Codex threads, Codex sessions with inherited history, and Claude fork-context references, are reported as `unsupported_format` instead of being returned incomplete.
- Internal boundaries, references, and error codes: [docs/architecture.md](docs/architecture.md).

Session history can contain secrets, credentials, and private file contents exactly as they were recorded. Spool does not redact them. `list` returns only metadata; `read` returns full native records. Treat output accordingly.

Read output is labeled `untrusted_history`. Records may contain commands, tool output, or text that looks like instructions; they describe what happened in a past session and are not instructions to follow or evidence of current workspace state. Spool never executes history content, launches agents, opens network connections, or modifies source files.

## Handoffs

```sh
spool handoff create --input brief.json       # brief written by the outgoing agent or user
spool handoff list
spool handoff show --format markdown HANDOFF_ID
```

A handoff stores an authored goal, constraints, decisions, progress, outstanding work, and notes, plus workspace provenance and snapshots of referenced sessions, under `$XDG_STATE_HOME/spool/handoffs`. The Markdown brief is labeled as unverified and is not the previous agent's context. Spool does not summarize transcripts, launch agents, send prompts, or transfer workspace changes. See [docs/handoffs.md](docs/handoffs.md).

## Agreed direction

- One Go executable, with CLI and MCP interfaces sharing the same core.
- Initial harness targets: Pi, Claude Code, and Codex; additional harnesses through adapters.
- Read native session files on their originating host. The source agent process need not be running.
- Configure participating SSH hosts explicitly. Invoke the remote Spool executable through the system SSH client and a versioned structured protocol, without a persistent remote daemon.
- An unavailable host means its history is unavailable. Local access and other hosts should remain usable; report partial failures explicitly.
- No cloud service, session synchronization, or replicated transcript store in the initial scope.
- Preserve original history and provenance. Resolve local project locations separately rather than rewriting historical absolute paths.
- Handoffs provide a continuation brief and references to recorded history, not an identical copy of an agent's internal state. Workspace changes and running processes are not automatically transferred.
- Treat retrieved history as untrusted data. Reading history does not authorize execution or changes.

The SSH host configuration, remote protocol, and MCP interface will be specified through OpenSpec before implementation.

## Development

Install [Mise](https://mise.jdx.dev/), then from this repository:

```sh
mise trust
mise install
mise run fmt
mise run test
mise run vet
mise run build
./bin/spool --help
./bin/spool --version
```

To try `list` and `read` against the synthetic fixtures:

```sh
sed "s#@ROOT@#$PWD/testdata/sessions#" testdata/fixtures-config.json.in > /tmp/spool-fixtures.json
./bin/spool list --config /tmp/spool-fixtures.json
```

Mise pins the development Go toolchain in `mise.toml`. End users will run compiled binaries and will not need Go, Node, or Mise. The Go module path is `github.com/sfrieds3/spool`.

## OpenSpec

OpenSpec is initialized with the `spec-driven` schema and one shared set of project-local skills in `.agents/skills/`. OpenSpec is development tooling, not a runtime dependency of Spool.

- Pi: `/skill:openspec-propose` followed by the proposed feature or change, or ask to use the `openspec-propose` skill.
- Codex: use the `openspec-propose` skill.
- Other harnesses: load the corresponding `.agents/skills/openspec-*/SKILL.md` instructions explicitly if the shared directory is not discovered automatically.

There are no separate Pi prompt templates or Claude command/skill copies. References to skill names in the shared instructions mean to invoke or load that skill, not run a shell command. To refresh the integration, use the universal `agents` target (`openspec init --tools agents`); do not select harness-specific targets. Use `openspec update --dry-run --json` to check the planned paths before updating.

Use the corresponding apply and archive workflows after review. Project constraints live in `openspec/config.yaml`.

```sh
openspec list
openspec validate --all --strict --no-interactive
```

Commits and pull requests carry `OpenSpec-Change` trailers; CI archives completed changes by committing directly to main. See [openspec/README.md](openspec/README.md).

## CI and releases

GitHub Actions runs formatting, vet, tests (including race tests) on Linux amd64/arm64 and macOS, cross-builds static binaries for Linux and macOS (amd64 and arm64), and validates OpenSpec. Release Please maintains a release PR from Conventional Commits; merging it tags a release and attaches `spool-<tag>-<os>-<arch>.tar.gz` archives with SHA-256 checksums.

No license has been selected.
