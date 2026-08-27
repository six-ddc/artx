# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.1.0] - 2026-08-25

First release. Covers the complete publish → review → address loop for both markdown and HTML artifacts.

### Added

**Vault and CLI** — the M0 loop.

- `artx init` creates a vault: directory skeleton, `git init`, `.gitattributes` with `merge=union` on comment files, and an `AGENTS.md` describing the protocol to agents.
- `artx new <slug> --type md|html` allocates an immutable 6-character doc id, writes the skeleton file, and prints `{id, path, url}`. Identity lives in markdown frontmatter (`aid:`) or an HTML `<meta name="aid">`, so directories can be renamed without orphaning comments.
- `artx list`, `artx path`, `artx open` for locating and opening artifacts.
- `artx comments`, `artx reply`, `artx addressed`, `artx resolve`, `artx reopen` for the comment lifecycle. Every command supports `--json`; exit codes are `0` success, `1` error, `2` not found.
- Comments are stored as an append-only YAML event stream per document at `.artx/comments/<docid>.yaml`. A thread's state is the fold of its events; folding deduplicates by event id and sorts by timestamp, so `merge=union` convergence after a `git pull` is order-independent.
- `artx serve` renders markdown Go-side with goldmark, tagging every block with `data-sourcepos`, and hosts a React UI for reading and commenting by text selection.
- Anchors use a W3C-style `TextQuoteSelector` (exact text plus ~32 characters of context on each side) with a byte-offset `TextPositionSelector` as an acceleration hint.
- Single-writer discipline: a running `artx serve` is the only process writing comment files, and the CLI probes for it via a lockfile and routes writes to its HTTP API. Without a serve, the CLI appends directly while holding an advisory `flock`.

**Watcher, HTML pipeline, and live updates** — M1.

- A file watcher diffs each changed document against its previous git revision, shifts every open anchor through the diff, and verifies the quote. Failed verification falls back to bitap fuzzy search; genuine disappearance marks the thread `orphan` and preserves the text it used to point at.
- Automatic git commits after the watcher settles, with agent / human / artx distinguished as commit authors.
- HTML artifacts render inside an `<iframe sandbox="allow-scripts">` with an injected vanilla reviewer script. Hovering outlines block elements, clicking anchors a comment to that element's `data-aid`.
- Idempotent `data-aid` injection into HTML block elements; existing ids are never reassigned.
- Server-Sent Events at `/api/stream` push comment, document, and index changes to open browsers.
- Historical revisions are viewable with `?v=<sha>`, read-only.
- Mermaid diagrams and KaTeX math are rendered client-side, lazily, and are bundled rather than loaded from a CDN so the binary works offline.

**Compaction, remote access, and dispatch** — M2.

- `artx compact` archives resolved threads into `<docid>.archive.yaml`, collapses edit chains into final bodies and remap chains into anchors, and commits separately. `artx serve` triggers it automatically past a size or age threshold.
- `artx serve --host` for non-local binding, gated on `--token`. Token authentication accepts a Bearer header, a query parameter, or an HttpOnly cookie.
- `artx watch --dispatch "<cmd>"` runs a command whenever a new open comment appears, closing the loop without a human starting the agent.
- `artx doctor [--fix]` checks a vault and repairs what it can, including trimming a corrupt event-log tail.
- `artx vault add|list|use` for the global multi-vault registry.

[Unreleased]: https://github.com/six-ddc/artx/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/six-ddc/artx/releases/tag/v0.1.0
