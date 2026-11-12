#!/usr/bin/env python3
"""Validate squash trailers and archive completed, previously referenced changes."""
import argparse
import json
import re
import subprocess
import sys
from pathlib import Path

SLUG = re.compile(r"[a-z0-9]+(?:-[a-z0-9]+)*\Z")
SHA = re.compile(r"(?:[0-9a-f]{40}|[0-9a-f]{64})\Z")
CHANGES = Path("openspec/changes")


def run(*args, text=None):
    result = subprocess.run(args, input=text, text=True, stdout=subprocess.PIPE,
                            stderr=subprocess.PIPE, stdin=subprocess.DEVNULL if text is None else None,
                            timeout=120, check=False)
    if result.returncode:
        raise RuntimeError(f"{' '.join(args)} failed: {result.stderr.strip()} {result.stdout.strip()}")
    return result.stdout


def trailers(message, required=False):
    parsed = run("git", "interpret-trailers", "--parse", text=message)
    values = []
    for line in parsed.splitlines():
        key, sep, value = line.partition(":")
        if sep and key.lower() == "openspec-change":
            value = value.strip()
            if not SLUG.fullmatch(value):
                raise ValueError(f"Invalid OpenSpec-Change trailer: {value!r}")
            values.append(value)
    if required and not values:
        raise ValueError("Add a final OpenSpec-Change: <slug> or OpenSpec-Change: none trailer")
    if "none" in values and len(values) != 1:
        raise ValueError("OpenSpec-Change: none cannot be combined with other trailers")
    return set(values) - {"none"}


def location(slug):
    if not SLUG.fullmatch(slug) or slug == "none":
        raise ValueError(f"Invalid change slug: {slug!r}")
    active = CHANGES / slug
    archive = CHANGES / "archive"
    archived = list(archive.glob(f"????-??-??-{slug}"))
    # OpenSpec leaves an already date-prefixed slug unchanged when archiving.
    if re.match(r"\d{4}-\d{2}-\d{2}-", slug):
        archived.append(archive / slug)
    archived = [p for p in archived if p.is_dir() and (p.name == slug or re.fullmatch(r"\d{4}-\d{2}-\d{2}-" + re.escape(slug), p.name))]
    if active.is_dir() and not active.is_symlink():
        return "active"
    if archived and all(not p.is_symlink() for p in archived):
        return "archived"
    raise ValueError(f"Unknown OpenSpec change: {slug}")


def check_archive_base(pr, repository):
    head = pr["head"]
    sha = head["sha"]
    if not SHA.fullmatch(sha):
        raise ValueError("PR head must be a full hexadecimal object ID")
    main = run("git", "rev-parse", "refs/remotes/origin/main").strip()
    commits = run("git", "rev-list", f"{main}..{sha}").splitlines()
    markers = []
    for commit in commits:
        message = run("git", "show", "-s", "--format=%B", commit)
        message_lines = message.splitlines()
        indices = [i for i, line in enumerate(message_lines)
                   if re.match(r"^\s*openspec-archive-base\s*:", line, re.IGNORECASE)]
        if indices:
            lines = [message_lines[i] for i in indices]
            continued = any(i + 1 < len(message_lines) and message_lines[i + 1][:1].isspace() for i in indices)
            if (len(lines) != 1 or continued or not lines[0].startswith("OpenSpec-Archive-Base: ") or
                    not SHA.fullmatch(lines[0][len("OpenSpec-Archive-Base: "):])):
                raise ValueError("Invalid OpenSpec-Archive-Base marker; regenerate the archive PR")
            markers.append((commit, lines[0].split(": ", 1)[1]))
    automation = (head["ref"] == "automation/openspec-archive" and head["repo"] is not None and
                  head["repo"]["full_name"] == repository["full_name"])
    if automation and not markers:
        raise ValueError("Archive automation PR lacks its base marker; regenerate it")
    if markers:
        if len(markers) != 1:
            raise ValueError("Multiple archive base markers; regenerate the archive PR")
        commit, base = markers[0]
        parents = run("git", "show", "-s", "--format=%P", commit).split()
        if base != main or parents != [base] or commit != sha:
            raise ValueError("Archive base is stale or ancestry changed; regenerate the archive PR, do not rebase it")


def pr_check(event):
    pr = event["pull_request"]
    check_archive_base(pr, event["repository"])
    # release-please uses GITHUB_TOKEN in this repository; do not exempt other bots.
    # Its branch is release-please--branches--main, optionally with a component suffix.
    ref = pr["head"]["ref"]
    release = (pr["user"]["login"] == "github-actions[bot]" and
               (ref == "release-please--branches--main" or
                ref.startswith("release-please--branches--main--components--")) and
               pr["head"]["repo"] is not None and
               pr["head"]["repo"]["full_name"] == event["repository"]["full_name"])
    refs = trailers(pr["title"] + "\n\n" + (pr.get("body") or ""), required=not release)
    for slug in sorted(refs):
        print(f"{slug}: {location(slug)}")


def commit_refs(revision):
    refs = set()
    for sha in run("git", "rev-list", "--reverse", revision).splitlines():
        refs.update(trailers(run("git", "show", "-s", "--format=%B", sha)))
    return refs


def json_cli(*args):
    return json.loads(run("openspec", *args, "--json"))


def archive_one(slug):
    where = location(slug)
    if where == "archived":
        print(f"{slug}: already archived")
        return
    status = json_cli("status", "--change", slug)
    apply = json_cli("instructions", "apply", "--change", slug)
    progress = apply.get("progress") or {}
    tasks = apply.get("tasks") or []
    artifacts = status.get("artifacts") or []
    ready = (status.get("isComplete") is True and artifacts and
             all(a.get("status") == "done" for a in artifacts) and
             apply.get("state") not in ("blocked", None) and
             isinstance(progress.get("total"), int) and progress["total"] > 0 and
             progress.get("complete") == progress["total"] and
             progress.get("remaining") == 0 and len(tasks) == progress["total"] and
             all(t.get("done") is True for t in tasks))
    if not ready:
        print(f"{slug}: not ready (planning, tasks, or apply state incomplete); skipped")
        return
    run("openspec", "validate", slug, "--type", "change", "--strict", "--no-interactive")
    # --yes is required even for the ordinary spec-update confirmation in 1.16.
    # The independent readiness and strict validation gates above are mandatory.
    result = json_cli("archive", slug, "--yes")
    if (not result.get("archive") or result["archive"].get("warnings") or
            any(item.get("severity") in ("warning", "error") for item in result.get("status", []))):
        raise RuntimeError(f"Archive reported warnings or no result for {slug}: {result}")
    print(f"{slug}: archived")


def archive_push(before, after):
    if not SHA.fullmatch(before) or not SHA.fullmatch(after):
        raise ValueError("Push before/after must be full hexadecimal object IDs")
    run("git", "merge-base", "--is-ancestor", after, "HEAD")
    # Include the exact push range, plus history to recover references when GitHub
    # replaces a pending concurrency run or an earlier workflow fails.
    revision = after if set(before) == {"0"} else f"{before}..{after}"
    refs = commit_refs(revision) | commit_refs("HEAD")
    for slug in sorted(refs):
        location(slug)  # Fail before any mutation on unknown references.
    for slug in sorted(refs):
        archive_one(slug)
    run("openspec", "validate", "--archived", "--strict", "--no-interactive")


def main():
    parser = argparse.ArgumentParser()
    sub = parser.add_subparsers(dest="command", required=True)
    pr = sub.add_parser("check-pr")
    pr.add_argument("event_file")
    archive = sub.add_parser("archive-push")
    archive.add_argument("before")
    archive.add_argument("after")
    args = parser.parse_args()
    if args.command == "check-pr":
        pr_check(json.loads(Path(args.event_file).read_text()))
    else:
        archive_push(args.before, args.after)


if __name__ == "__main__":
    try:
        main()
    except (ValueError, RuntimeError, OSError, subprocess.TimeoutExpired, json.JSONDecodeError, KeyError) as exc:
        print(f"OpenSpec CI: {exc}", file=sys.stderr)
        sys.exit(1)
