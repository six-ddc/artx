# artx — Agent artifact repository and comment-loop CLI design

> Version v0.4 · 2026-08-24 · Product name: `artx` — as in artifact, with the `x` disambiguating it from the several existing tools already called `art`

## 1. Positioning

**In one sentence**: a local artifact publishing and review loop tool for Claude Code / Codex users — the agent publishes design documents (md) and demos (html) into a filesystem-backed vault, humans read them on a hosted page and comment by selecting or clicking, the agent reads the comments, addresses them and marks them, with the CLI as the only surface for operating any of it.

**Target users**: developers who habitually co-create with AI agents and live in the terminal. Not GUI novices.

**Non-goals (explicitly out of scope)**:

- Not a note-taking app (humans do not create documents here; creation always comes from the agent)
- Not a sync engine (remote = git; comments are files too, and travel with git)
- Not a rich-text editor (humans edit via any external editor, or lightweight in-browser editing)
- Not an agent runtime (no embedded model calls; dispatch only launches headless sessions)

**Design principles**: the filesystem is the database, git is versioning and sync, the browser is the human's UI, the agent brings its own editor. `artx` only does the glue plus the one layer of unique value — comments. **Bypassing is harmless**: any tool reading or writing vault files directly cannot corrupt the data; correctness comes from the watcher as a fallback, not from enforcement on the write path.

## 2. Technology choices

| Layer | Choice | Rationale |
|---|---|---|
| CLI | Go + cobra | Single static binary, brew/curl install, zero friction for the target audience |
| md rendering | goldmark | CommonMark + GFM extensions, AST carries byte offsets (block-level sourcepos available) |
| File watching | fsnotify | Cross-platform |
| diff/remap | sergi/go-diff (Go port of diff-match-patch) | Same algorithm family as Hypothesis; fuzzy quote matching + offset remapping in one shot |
| HTML parsing | golang.org/x/net/html | data-aid injection pipeline |
| git | exec system git | Simpler and more reliable than go-git; the target user is guaranteed to have git |
| Frontend | React 19 + React Compiler, Vite 8 (Rolldown/Oxc), TanStack Router (SPA mode) + TanStack Query, Tailwind v4 + shadcn/ui, TS strict | Fully modern yet purely static export; `pnpm build` produces dist/, packed into the binary by `//go:embed all:dist`. No Next/RSC — the server is Go |
| Realtime push | SSE | One-way is enough (comment updates, file-change notifications), simpler than WebSocket |

Precedents for Go + go:embed: Gitea, miniflux. All frontend assets are embedded in the binary; `artx serve` has zero external dependencies.

## 3. Core concepts

- **vault**: a git repository directory, the publishing destination for artifacts. The agent works in any cwd and finds the vault through a global registry. A vault must be born on fresh ground: `artx init` refuses to create one inside an existing git worktree (so artx's machine commits never interleave with a project's history) or in a non-empty directory (so a data directory is never silently annexed) — `--force` overrides both.
- **artifact**: a directory inside the vault containing `index.md` or `index.html` plus optional assets. Its identity is an immutable **doc id** (6-character base36; md stores it in frontmatter as `aid:`, html as `<meta name="aid">`), and the path is merely an address.
- **comment thread (thread)**: a discussion anchored to a position in a document, with the state machine `open → addressed → resolved` (reopenable).
- **event log**: one YAML event stream file per document; every change to comments is an appended event, and the current state = fold(all events).

## 4. Directory layout

```
vault/
├── .artx/
│   ├── config.yaml              # vault config (port, compaction threshold, etc.)
│   ├── comments/
│   │   ├── a7f3.yaml            # active event log (by doc id)
│   │   └── a7f3.archive.yaml    # resolved threads archived at compaction
│   └── assets/
│       └── a7f3/                # images and other assets for this document
├── payment-refactor/
│   └── index.md                 # frontmatter: aid: a7f3
├── pricing-demo/
│   └── index.html               # <meta name="aid" content="b2c9">
├── .gitattributes               # .artx/comments/*.yaml merge=union
└── .gitignore                   # .artx/serve.lock (it carries the --token value)
```

Global registry `~/.config/artx/config.yaml`:

```yaml
default_vault: work
vaults:
  work: ~/vaults/work
  personal: ~/vaults/personal
```

## 5. CLI command surface

Every agent-facing command supports `--json`, with semantic exit codes (0 success / 1 error / 2 not found).

```
artx init [dir]                     # create vault: directory skeleton + git init + .gitattributes/.gitignore + AGENTS.md template; requires a new/empty dir outside any git repo (--force overrides)
artx new <slug> --type md|html      # allocate an id, create the skeleton file, print {id, path, url}
artx path <slug|id>                 # resolve the absolute path
artx list [--json]                  # all artifacts + open comment counts
artx open [slug]                    # open in the browser (index page by default)

artx serve [--host 0.0.0.0] [--token T] [--port 7777] [--no-watch]

artx comments [--open|--all] [--doc slug] --json   # includes path, line number, offset, quote, surrounding context
artx reply <thread> <text>          # append a reply event
artx addressed <thread> [--commit sha]
artx resolve <thread> / artx reopen <thread>
artx compact [--doc slug]           # manual compaction (serve also does it automatically at the threshold)
```

**Example `artx new --json` output** (the agent takes the path and reads/writes it with its own native Read/Edit tools; the CLI provides no content I/O):

```json
{"id":"a7f3","path":"/Users/cappu/vaults/work/payment-refactor/index.md","url":"http://localhost:7777/a/a7f3"}
```

**Example `artx comments --open --json` output**:

```json
[{"thread":"c12","doc":"a7f3","path":".../payment-refactor/index.md",
  "status":"open","author":"cappu","body":"This paragraph is too verbose; merge it into the previous section",
  "anchor":{"exact":"Choosing a payment gateway requires weighing several factors","line":42,"start":1042,"end":1087,
            "prefix":"Therefore, ","suffix":", including","context":"(2 lines of original text on each side)"},
  "replies":[],"created_rev":"a1b2c3d"}]
```

## 6. Comment data model: append-only YAML event stream

### 6.1 Event types and format

`.artx/comments/<docid>.yaml`, a multi-document YAML stream: one event = one `---`-separated document, append-only (a write = appending one complete block at the end of the file, O(1) and near-atomic). The Go side reads it streaming with `yaml.Decoder` and folds event by event.

```yaml
---
e: create
ts: 2026-08-24T10:12:03+08:00
thread: c12
author: cappu
body: This paragraph is too verbose
anchor:
  kind: text            # text | element (used by html; fields are aid + optional quote)
  exact: Choosing a payment gateway requires weighing several factors
  prefix: 'Therefore, '
  suffix: ', including'
  start: 1042
  end: 1087
  rev: a1b2c3d
---
e: reply
ts: 2026-08-24T21:30:00+08:00
thread: c12
id: c12.1
author: agent:claude
body: Trimmed and merged into section 2
---
e: edit                 # editing a comment = appending an event carrying the full new body; the fold overwrites
ts: 2026-08-24T21:31:00+08:00
target: c12
body: This paragraph is too verbose; just delete it
---
e: addressed
thread: c12
by: agent:claude
commit: 9f8e7d6
---
e: resolve
thread: c12
by: cappu
---
e: remap                # emitted by the watcher; an anchor's current position = the create anchor with the last remap applied
thread: c12
start: 998
end: 1043
rev: 9f8e7d6
---
e: orphan
thread: c12
last_exact: Choosing a payment gateway requires weighing several factors
rev: 9f8e7d6
```

Remaining event: `reopen`. Status fold rule: scan in file order; the last status event determines the current state (`open → addressed → resolved`, reopenable).

### 6.2 Why a YAML event stream

A human can open it and understand it directly (this is the core reason for YAML over JSONL — the sidecar is itself a first-class vault file, and it stays readable in a git diff); the append semantics are equivalent to JSONL; once `.gitattributes` sets `merge=union` on that directory, event blocks appended at the end of the file by either side are both preserved on merge, and since every block begins with `---`, parsing is order-independent — **this is what makes "remote = git" hold**: comments written from the browser on the server and replies from the local agent converge automatically via pull/push. Convention: a writer only ever appends a complete document block at the end of the file, and never rewrites existing lines (the right to rewrite belongs to compact alone).

### 6.3 Compaction

Triggers: `artx compact` manually, or serve detecting a log > 256KB / resolved threads older than 30 days. Actions:

1. A resolved thread is folded whole into a single summary event and moved into `<docid>.archive.yaml`
2. The edit chain collapses into the final body (the create event is rewritten in place)
3. The remap chain collapses into the create event's anchor
4. Compaction is itself a standalone git commit (`artx: compact a7f3`), so the full history is still recoverable from git

### 6.4 Concurrent writes and locking

The write strategy for comment files differs from that for document content: documents are "bypassing is harmless", comments **must go through the write funnel** (AGENTS.md states it outright: `.artx/comments/` may only be operated on via the CLI / API). Every writer (CLI, browser, watcher) is artx's own code, so an advisory lock suffices:

- **serve running → serve is the single writer**: the CLI probes for serve via lockfile/port and routes writes to its API; inside serve a single writer goroutine serializes all events (including the watcher's remaps), so there is no concurrency at the file layer.
- **serve not running → the CLI appends directly, holding `flock`**: it locks the target yaml, completes a single-block append, and releases. It does not rely on assumptions about O_APPEND atomicity.
- **No locking across machines**: resolved by git merge=union convergence.

Defense in depth: event id = timestamp + random suffix (no id collisions under concurrency); the parser skips corrupt blocks and warns; `artx doctor` trims a trailing partial block — append-only guarantees damage can only occur at the end of the file.

## 7. Anchors and remapping

- **md**: W3C-style dual selectors — a TextQuoteSelector (exact + ~32 characters of prefix/suffix each) as the primary, and a TextPositionSelector (byte offset) as an acceleration hint. The rendering side uses goldmark block-level sourcepos to convert a browser selection back into source-file offsets; the precise position within a block comes from matching the quote inside the block's source text (sidestepping the pitfalls of goldmark's inline offsets).
- **html**: data-aid anchors the element directly; a selection inside an element attaches a quote.
- **Remapping**: the watcher runs it on every file change — take the previous git version and the new content, run diff-match-patch, shift all open threads' offsets through the diff; if quote verification passes, append a `remap`, on failure do a bitap fuzzy search (threshold 0.5), and failing that append an `orphan`.
- **orphan semantics**: last_exact is preserved to show "what this used to point at"; the hint shown to the agent is fixed as "The anchored text no longer exists — the feedback was likely addressed. Confirm with resolve, or re-anchor."

## 8. serve architecture

```
┌─ artx serve ───────────────────────────────────────────────────────┐
│  HTTP (net/http)                                                  │
│  ├─ GET  /                    index page (embedded)               │
│  ├─ GET  /a/{id}[?v=sha]      document page / historical version  │
│  ├─ GET  /api/docs            list                                │
│  ├─ GET  /api/docs/{id}/comments   folded threads                 │
│  ├─ POST /api/docs/{id}/events     browser-written events         │
│  ├─ GET  /api/stream          SSE (comments / file changes)       │
│  └─ /assets/*  /static/*      embedded frontend + vault assets    │
│  Watcher (fsnotify, 300ms debounce)                               │
│  └─ diff → remap → aid injection → auto-commit                    │
└───────────────────────────────────────────────────────────────────┘
```

**md document page**: rendered by goldmark, with block-level elements carrying `data-sourcepos="start:end"`; the comment overlay takes the selection via the Selection API → nearest sourcepos block → quote positioning within the block → POST a create event. Thread list in the right sidebar, refreshed live over SSE.

**html document page**: the artifact is loaded into `<iframe sandbox="allow-scripts">` (no same-origin), serve injects a reviewer script that talks to the parent page over postMessage. In review mode, hovering draws a box (getBoundingClientRect) and clicking picks up the data-aid; the parent page receives the aid and pops up the comment box. **Direct editing (M2)**: the selected element becomes contenteditable, and changes are written back into the source file by aid through the API, as a commit with `author=human`.

**data-aid injection** (inside the watcher, idempotent): parse the html → block-level/semantic elements (div/section/h*/p/button/table/img/…) get a 6-character id if they lack an aid, and are left alone if they have one → rewrite the file only when something was added. span level is not injected; fine granularity comes from the quote. AGENTS.md declares that "data-aid is a system attribute; preserve it when modifying a document".

**Frontend project shape**: a `web/` subproject inside the repo; the Makefile chains `pnpm build → go build`; dist/ is not in git and is built by goreleaser/CI at release time (distribution goes through brew/release binaries, giving up on bare `go install`). During development `vite dev` proxies `/api` to :7777. Go falls back to index.html for non-`/api` paths to support client-side routing. SSE events drive TanStack Query invalidation to give live comment refresh.

**Two rendering red lines**: ① the single source of truth for md rendering is Go-side goldmark (React only consumes the HTML carrying data-sourcepos and attaches overlays); the frontend must never re-render md, or the sourcepos anchor system collapses; ② the reviewer script injected into the sandboxed iframe of an html artifact stays vanilla with zero dependencies — React lives only in the shell application and never enters the sandbox.

**auto-commit**: once the watcher debounce settles, the artifact's own directory and `.artx/comments/` are staged and committed as `artx: update <slug>`. The scope is deliberately narrower than `git add -A`: staging the whole tree while processing one document sweeps in whatever another document happens to be mid-edit, committing a state the watcher never processed. Authorship distinguishes three classes — agent/human/artx (writes through the comment API use `artx-web <reviewer>`).

## 9. Security

- Listens on 127.0.0.1 only by default. `--host` must be paired with `--token` (Bearer / cookie), otherwise startup is refused
- html artifacts always go in a sandboxed iframe, no same-origin; the shell page sets a CSP
- serve only reads and writes files inside the vault directory; path traversal is validated

## 10. Agent integration (AGENTS.md template, generated by `artx init`)

```markdown
# This project's deliverables are published through artx

- When producing a design/demo: `artx new <slug> --type md|html --json` to get the path,
  write the content at that path with your own file tools; your reply must include the returned url.
- At the start of a session run `artx comments --open --json` and work through them one by one:
  modify the document → `artx reply <thread> "<explanation>"` → `artx addressed <thread> --commit <sha>`.
  Do not resolve on your own; resolve belongs to the human.
- Threads in the orphan state: confirm whether the original feedback has been satisfied, reply with
  an explanation, and ask the human to confirm.
- data-aid / the aid frontmatter are system identifiers; preserve them verbatim when editing.
```

The SKILL.md version for Claude Code is the same, just wrapped in a trigger description. No MCP — the CLI is the interface; if it turns out to be genuinely needed, wrap the CLI into an MCP server later with 20 lines of code.

## 11. Milestones

**M0 (week 1, a usable loop)**: init / new / path / list / open; serve: index page + md rendering + selection commenting + thread sidebar; comments / reply / addressed / resolve CLI; YAML event stream read/write and fold; manual git.
Acceptance criteria: agent publishes md → human comments in the browser → agent reads, addresses and replies → human resolves, end to end.

**M1 (weeks 2–3, invariants automated)**: watcher (diff remapping / orphan / auto-commit); html pipeline (aid injection + iframe picker + element comments); SSE; AGENTS.md generation; `?v=sha` historical version viewing.

**M2 (week 4+, value-add)**: compact; `--host + --token`; `artx watch --dispatch "claude -p …"` (new comments automatically dispatch a headless agent, an asynchronous loop); in-browser direct editing of html elements; a fleshed-out multi-vault registry.

## 12. Risks and countermeasures

| Risk | Countermeasure |
|---|---|
| goldmark inline-level offsets are unreliable | Depend on block-level sourcepos only, and match by quote within the block (already built into the anchor design) |
| The agent omits the url / never checks comments | A strong convention in AGENTS.md + `artx list` as a fallback; dispatch mode fixes it at the root |
| JS-heavy html demos misbehave in a sandboxed iframe | M1 provides a `--raw` direct-open escape hatch (giving up the comment layer) |
| Log bloat | Automated compact threshold; archive separation |
| Name collision | Settled: the plain name `art` was taken on npm, crates.io and PyPI (where it also installs a competing `art` binary), so `artx` was chosen instead — free on Homebrew, npm, PyPI, crates.io and apt, with no same-named binary anywhere |

## 13. Open questions (decided during implementation)

1. Whether the doc id is random base36 or a prefix of the hash of the first content version — **Decision: random.**
2. Commenter identity: `$USER` by default on a single machine; in the `--host` case, is the token the identity, or is self-declared naming allowed — **Decision: `$USER` on a single machine; in the `--host` case the token is the identity, plus a self-declared display name is allowed.**
3. Whether md-embedded mermaid/katex rendering lands in M1 — **Decision: yes.**
