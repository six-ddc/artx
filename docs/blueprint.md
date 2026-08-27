# artx Implementation Blueprint v1.0

> Targets the full M0–M2 implementation · 2026-08-24 · the sole requirements source is `docs/design.md`; this document covers only **how to implement it**
>
> This document and the skeleton code in the repo are two statements of the same contract. **The skeleton code is authoritative for signatures; this document is authoritative for semantics.**
> Implementers fill in function bodies and add unexported helpers and tests only — **do not change exported signatures, field names, or tags**.

---

## 0. Five design red lines (in force throughout; no implementation may violate them)

| # | Red line | Where it lands |
|---|---|---|
| R1 | The single source of truth for md rendering is Go-side goldmark; the frontend never re-renders md | `internal/render` produces `DocDetail.HTML`; the frontend only does `dangerouslySetInnerHTML` |
| R2 | The reviewer script injected into the sandbox iframe is pure vanilla; React never enters the sandbox | `web/src/reviewer/` is a separate Vite entry and does not import React |
| R3 | Comment files are append-only; only compact may rewrite them | `eventlog.Store.Append` only appends; `Compact`/`Truncate` are the only rewriters |
| R4 | The serve runtime is the single writer; when the CLI probes a live serve it routes to the API | `lockfile.Probe` + `client.Detect`; inside serve, `server.Writer` serializes on a single goroutine |
| R5 | 127.0.0.1 only by default; `--host` must be paired with `--token` | `server.New` returns `ErrTokenRequired` and refuses to start |

---

## 1. Repository file tree

```
artx/
├── go.mod  go.sum  Makefile  .gitignore
├── docs/
│   ├── design.md                    requirements (read-only)
│   └── blueprint.md                 this document
├── cmd/artx/
│   ├── main.go                      entry point + exit code mapping    [arch layer written]
│   ├── root.go                      command tree + openVault/dial/emit [W-core]
│   ├── init.go  new.go  path.go  list.go  open.go                      [W-core]
│   ├── comments.go  reply.go  addressed.go  resolve.go  reopen.go      [W-core]
│   ├── compact.go  doctor.go                                           [W-core]
│   └── serve.go                                                        [W-serve]
├── internal/
│   ├── api/api.go                   DTO contract, frozen                  [arch layer]
│   ├── mdsrc/mdsrc.go               md source position truth, implemented [arch layer]
│   ├── mdsrc/mdsrc_test.go          implemented and passing               [arch layer]
│   ├── version/version.go                                                 [arch layer]
│   ├── idgen/idgen.go                                                     [W-core]
│   ├── config/config.go                                                   [W-core]
│   ├── vault/vault.go                                                     [W-core]
│   ├── eventlog/event.go            Event schema, frozen                  [W-core]
│   ├── eventlog/store.go            read/write / fold / compact           [W-core]
│   ├── lockfile/lockfile.go         flock + serve probe                   [W-core]
│   ├── lockfile/flock_unix.go       implemented                           [arch layer]
│   ├── lockfile/flock_other.go      implemented                           [arch layer]
│   ├── gitx/gitx.go                                                       [W-core]
│   ├── client/client.go             CLI→serve HTTP client                 [W-core]
│   ├── anchor/anchor.go             Anchor struct frozen                  [W-anchor]
│   ├── remap/remap.go                                                     [W-anchor]
│   ├── htmlaid/htmlaid.go                                                 [W-anchor]
│   ├── watcher/watcher.go                                                 [W-anchor]
│   ├── render/render.go                                                   [W-serve]
│   └── server/
│       ├── server.go                routing / single writer / SSE / auth  [W-serve]
│       ├── embed.go                 go:embed all:dist                     [arch layer]
│       └── dist/index.html          placeholder page                      [arch layer]
└── web/                                                                   [W-web]
    ├── package.json  pnpm-lock.yaml  vite.config.ts  tsconfig.json
    ├── tailwind.config.ts  components.json  index.html
    └── src/
        ├── main.tsx  routes.tsx  styles.css
        ├── lib/types.ts            TS mirror of internal/api, frozen      [arch layer]
        ├── lib/protocol.ts         postMessage protocol, frozen           [arch layer]
        ├── lib/api.ts              fetch wrapper
        ├── lib/queries.ts          TanStack Query key + hooks
        ├── lib/sse.ts              EventSource → invalidate
        ├── lib/selection.ts        selection → SelectionInput
        ├── routes/index.tsx  routes/doc.tsx
        ├── components/…            see §7.2
        └── reviewer/reviewer.ts    vanilla, separate entry (R2)
```

**The file lists are disjoint**: each work package's files are tagged `[W-*]` in the tree above; no file is owned by two packages.

---

## 2. Package dependency graph

```
api ────────────────────────────────────┐ (leaf; imports no internal package)
mdsrc ──► (goldmark)                    │
anchor ──► api, mdsrc                   │
eventlog ──► api, anchor, lockfile, idgen
vault ──► api, config, eventlog, gitx, anchor, mdsrc, idgen
remap ──► api, anchor, eventlog
htmlaid ──► idgen, (x/net/html)
watcher ──► vault, eventlog, remap, htmlaid, gitx, api
render ──► mdsrc
server ──► vault, eventlog, render, watcher, api, config, lockfile
client ──► api, lockfile
cmd/artx ──► everything
```

Acyclic. **Before adding any cross-package dependency, confirm it introduces no cycle** — in particular `anchor → eventlog` is forbidden (it would form a cycle with `eventlog → anchor`).

---

## 3. Go package boundaries and exported APIs

The exported contracts are listed per package below. Doc comments are omitted; the full semantics live in the skeleton files.

### 3.1 `internal/api` — DTO contract (frozen; no work package may modify it)

Constants: `DocTypeMD/DocTypeHTML`, `StatusOpen/StatusAddressed/StatusResolved`, `AnchorText/AnchorElement`, `OrphanHint`, `ErrNotFound/ErrBadRequest/ErrUnauthorized/ErrForbidden/ErrConflict/ErrInternal`, `SSEComments/SSEDoc/SSEDocs/SSEPing`.

Types: `Doc`, `DocDetail`, `DocsResponse`, `ThreadAnchor`, `Reply`, `Addressed`, `Thread`, `CommentsResponse`, `SelectionInput`, `ElementInput`, `EventRequest`, `EventResponse`, `NewDocRequest/NewDocResponse`, `HealthResponse`, `CompactRequest/CompactStat/CompactResponse`, `ErrorResponse`, `SSEComment`, `SSEDocChange`.

**Key ruling**: `artx comments --json` and `GET /api/docs/{id}/comments` emit the **same** `[]api.Thread`. This is deliberate, not coincidence — when the CLI probes a live serve it forwards the request to the API, and both paths must produce byte-identical JSON; otherwise an agent's behavior would drift depending on whether serve happens to be running.

### 3.2 `internal/mdsrc` — single source of truth for md source positions (implemented by the architecture layer, frozen)

```go
type Segment struct{ Start, End int }
type Block struct {
    Kind string; Start, End, Level, Depth int
    Segments []Segment; Language string
}
type Document struct {
    Source []byte; BodyOffset int; Frontmatter []byte; Blocks []Block
}
type BlockMap struct{ Text string /* + unexported segs */ }

func NewMarkdown() goldmark.Markdown
func NewContext(bodyOffset int) parser.Context
func WithBodyOffset(pc parser.Context, off int)
func SplitFrontmatter(src []byte) (fm []byte, bodyOffset int)
func ParseFrontmatter(src []byte) (map[string]any, error)
func Parse(src []byte) (*Document, error)

func (d *Document) LineOf(off int) int
func (d *Document) Context(start, end, n int) string
func (d *Document) BlockAt(start, end int) *Block
func (d *Document) BlockCovering(off int) *Block
func (d *Document) BlockMap(b *Block) *BlockMap
func (m *BlockMap) ToFile(i int) int
func (m *BlockMap) Range(i, j int) (int, int)
```

All offsets are without exception **file-absolute byte offsets** (frontmatter included), half-open intervals.

### 3.3 `internal/anchor` — anchors (the `Anchor` struct is frozen)

```go
const ContextChars = 32
const ContextLines = 2
var ErrNoMatch error

type Anchor struct {
    Kind, Exact, Prefix, Suffix string
    Start, End int
    Rev, AID string
    Approx bool
}
type Match struct{ Start, End int; Score float64; Approx bool }

func FromSelection(doc *mdsrc.Document, sel api.SelectionInput) (Anchor, error)
func FromElement(el api.ElementInput) (Anchor, error)
func Locate(src []byte, a Anchor) (Match, error)
func Enrich(src []byte, doc *mdsrc.Document, threads []api.Thread)
func ToAPI(a Anchor) api.ThreadAnchor
func Quote(src []byte, start, end int) (exact, prefix, suffix string)
```

### 3.4 `internal/eventlog` — event stream

```go
const KindCreate/KindReply/KindEdit/KindAddressed/KindResolve/KindReopen/KindRemap/KindOrphan/KindArchive
const CommentsDir = ".artx/comments"
const DefaultCompactSizeBytes, DefaultCompactResolvedAge
var ErrCorruptTail, ErrThreadNotFound
var StatusKinds map[string]string

type Event struct{ /* full field table in §4 */ }
type ArchivedThread struct{…}; type ArchivedNote struct{…}
type ReadReport struct{ Events int; TailCorrupt bool; TailOffset int64; Warnings []string }
type FoldResult struct{ Threads []api.Thread; Warnings []string }
type CompactOptions struct{ Force bool; SizeBytes int64; ResolvedAge time.Duration; Now time.Time }
type Store struct{ /* unexported */ }

func NewEvent(kind string) Event
func Open(root string) *Store
func Marshal(events ...Event) ([]byte, error)
func ReadFrom(r io.Reader) ([]Event, *ReadReport, error)
func Fold(events []Event) *FoldResult

func (s *Store) Root() string
func (s *Store) Path(docID string) string
func (s *Store) ArchivePath(docID string) string
func (s *Store) DocIDs() ([]string, error)
func (s *Store) Read(docID string) ([]Event, *ReadReport, error)
func (s *Store) Append(docID string, events ...Event) error
func (s *Store) Threads(docID string) (*FoldResult, error)
func (s *Store) Truncate(docID string, keep int) error
func (s *Store) Compact(docID string, opts CompactOptions) (api.CompactStat, error)
func (s *Store) NeedsCompact(docID string, opts CompactOptions) (bool, error)
```

### 3.5 `internal/vault` — vault facade

```go
const ArtDir, AssetsDir, AgentsFile, IndexMD, IndexHTML
const FrontmatterAIDKey = "aid"; const MetaAIDName = "aid"
var ErrNotFound, ErrExists, ErrOutsideVault, ErrInsideRepo

type Vault struct{ Root, Name string; Cfg *config.Vault; Store *eventlog.Store; Git *gitx.Repo }
type Artifact struct{ ID, Slug, Type, Dir, Path, RelPath, Title string }

func Open(root, name string) (*Vault, error)
func Discover(explicit string) (*Vault, error)
type InitOptions struct{ Name string; Force bool }
func Init(ctx context.Context, dir string, opts InitOptions) (*Vault, error)
func AgentsTemplate() []byte

func (v *Vault) Scan() ([]Artifact, error)
func (v *Vault) Lookup(ref string) (*Artifact, error)
func (v *Vault) New(slug, typ, title string) (*Artifact, error)
func (v *Vault) ReadSource(a *Artifact) ([]byte, error)
func (v *Vault) ResolvePath(rel string) (string, error)
func (v *Vault) Docs(ctx context.Context, baseURL string) ([]api.Doc, error)
func (v *Vault) Doc(ctx context.Context, a *Artifact, baseURL string) (api.Doc, error)
func (v *Vault) Threads(ctx context.Context, a *Artifact) (*api.CommentsResponse, error)
func (v *Vault) AllThreads(ctx context.Context, status string) ([]api.Thread, error)
func (v *Vault) FindThread(ctx context.Context, threadRef string) (*Artifact, *api.Thread, error)
func (v *Vault) Author() string
```

### 3.6 Remaining packages

`config`: the `Global`/`Vault` structs + `LoadGlobal/SaveGlobal/GlobalFilePath/Register/LoadVault/SaveVault/Resolve/FindRoot`, and `(*Vault).Debounce()`.

`gitx`: `Repo` + `Open/Available/Init/HeadSHA/FileRev/ShowFile/Commit/LogFile`, the package-level `Toplevel`, the three-valued `Author` enum, the `CommitOptions` and `Commit` records, `EnsureGitattributes`, `GitattributesLine`.

`idgen`: `DocID/ElementID/ThreadID/ReplyID/EventID/IsThreadID/IsReplyID/ThreadOfComment/Random`.

`lockfile`: `ServeInfo`, `Lock`, `Acquire/TryAcquire/WithLock/AcquireServe/Probe`, `ErrLocked/ErrNoServe`.

`client`: `Client` + `New/FromServeInfo/Detect/Health/Docs/NewDoc/Comments/PostEvent/FindThread/Compact`.

`remap`: `Options/DefaultOptions/NewDMP/Result/Remap/RemapOne/Events`, `KindUnchanged/KindMoved/KindOrphan/KindRevived`.

`htmlaid`: `AIDAttr/ReviewerScriptPath/BlockTags`, `InjectResult`, `Inject/Parse/Render/FindByAID/ExtractDocAID/SetDocAID/Title/ElementText/InjectReviewer/ReplaceElementHTML`, `ReviewerOptions`.

`watcher`: `Notice/Options/Watcher` + `New/Run/Close/Process/ProcessAll/Ignore`.

`render`: `Result/Renderer` + `New/Render`, and the constant `Sanitize = false`.

`server`: `Options/Server` + `New/Run/Addr/BaseURL/Handler`; `Writer` + `NewWriter/Run/Append`; `Hub` + `NewHub/Broadcast/ServeHTTP/FromNotice`; `Auth/CSP/TokenCookie/PingInterval/WriteError/WriteJSON/Placeholder/DistFS`.

---

## 4. Comment event YAML schema specification

File: `<vault>/.artx/comments/<docid>.yaml`, a multi-document YAML stream, one `---` block per event.
Archive: `<vault>/.artx/comments/<docid>.archive.yaml`, containing only `archive` events.

### 4.1 Full field table

| Field | YAML key | Type | Notes |
|---|---|---|---|
| Event kind | `e` | string | see §4.2 |
| Event id | `eid` | string | `base36(unixMilli)-<4 base36 chars>`; **fold's dedup key** |
| Timestamp | `ts` | RFC3339 with timezone | fold's sort key |
| Thread | `thread` | string | `c` + 5 base36 chars |
| Reply id | `id` | string | `<thread>.<3 base36 chars>` |
| Edit target | `target` | string | thread id or reply id |
| Author | `author` | string | create/reply/edit |
| Actor | `by` | string | addressed/resolve/reopen |
| Body | `body` | string | |
| Anchor | `anchor` | map | create only, see §4.3 |
| New start | `start` | int | remap only |
| New end | `end` | int | remap only |
| git rev | `rev` | string | create/remap/orphan |
| Commit | `commit` | string | addressed only |
| Note | `note` | string | addressed/reopen |
| Vanished text | `last_exact` | string | orphan only |
| Archive snapshot | `archived` | map | archive only; appears only in `.archive.yaml` |

### 4.2 Required/optional fields per event kind

| `e` | Required | Optional | Producer |
|---|---|---|---|
| `create` | `eid ts thread author body anchor` | `rev` | browser / CLI |
| `reply` | `eid ts thread id author body` | — | browser / CLI (`artx reply`) |
| `edit` | `eid ts target body` | `author` | browser |
| `addressed` | `eid ts thread by` | `commit note` | CLI (`artx addressed`) |
| `resolve` | `eid ts thread by` | — | browser / CLI |
| `reopen` | `eid ts thread by` | `note` | browser / CLI |
| `remap` | `eid ts thread start end` | `rev` | watcher |
| `orphan` | `eid ts thread last_exact` | `rev` | watcher |
| `archive` | `eid ts thread archived` | — | compact (writes to `.archive.yaml`) |

### 4.3 The `anchor` sub-structure

| key | Type | Notes |
|---|---|---|
| `kind` | `text` \| `element` | |
| `exact` | string | the anchored **source file** text (not the rendered text) |
| `prefix` / `suffix` | string | 32 runes on each side of exact |
| `start` / `end` | int | file-absolute byte offsets, half-open interval |
| `rev` | string | the git short sha this offset corresponds to |
| `aid` | string | element anchors only |
| `approx` | bool | true = block-level fallback, not an exact quote hit |

### 4.4 fold semantics (**both implementations must agree rule for rule**)

1. **Dedup by `eid`**. `merge=union` can make the same event block appear twice.
2. **Stable sort**: `ts` ascending, ties broken by `eid` ascending. **Physical file order does not participate** — the order after a union merge is untrustworthy. Events with no `ts` sort first.
3. `create` opens a thread; when one `thread` has several `create` events, the first after sorting wins and the rest go into `Warnings`.
4. `reply` appends a comment, deduped by `id`.
5. `edit` overwrites `target`'s body and records `EditedAt`; when `target` equals the thread id it edits the root comment.
6. Status is decided by the **last entry after sorting** among `StatusKinds` (create→open, addressed→addressed, resolve→resolved, reopen→open).
7. `remap` takes the last entry, overwrites `Anchor.Start/End/Rev`, **and clears the Orphan flag** (a thread must be able to revive when the content is put back).
8. `orphan` sets `Anchor.Orphan=true` and `LastExact`, and sets `Thread.Hint` to `api.OrphanHint`.
9. Events referencing a non-existent thread, and unknown `e` values: go into `Warnings` and are dropped — **no error is returned**.
10. `Thread.UpdatedAt` = the maximum `ts` across all of that thread's events.
11. Output is sorted by `CreatedAt` ascending. `Doc/Slug/Path` and `Anchor.Line/Context` are left empty, to be filled in by `vault.Threads` and `anchor.Enrich`.

### 4.5 Corruption tolerance

`Store.Read`'s decoder stops at the first error, **returns every event parsed so far** and sets `ReadReport.TailCorrupt` — **it returns no error**. This is self-consistent with append-only: damage can only be at the tail of the file. `artx doctor --fix` calls `Truncate` to trim it.

### 4.6 compact

Triggers: `artx compact` manually, or serve detecting a log > 256KB / a thread resolved more than 30 days ago. The flock is held throughout:

1. Threads that are resolved and whose resolve time exceeds `ResolvedAge` → collapsed into a single `archive` event appended to `.archive.yaml`, and removed from the active file
2. The remaining threads' edit chains collapse into the create/reply body
3. remap chains collapse into the create anchor (keeping the last start/end/rev)
4. Write `<docid>.yaml.tmp`, then `rename` for an atomic replace
5. One separate git commit: `artx: compact <docid>`

---

## 5. HTTP API contract

> **The frontend agent implements from this section alone and does not read the Go code.** The TS definitions of the fields are in `web/src/lib/types.ts`.

Base URL: `http://127.0.0.1:7777` (can be changed by `--port` / vault config). All JSON responses use `Content-Type: application/json; charset=utf-8`.

### 5.1 Endpoint table

| Method | Path | Request body | Response body |
|---|---|---|---|
| GET | `/api/health` | — | `HealthResponse` |
| GET | `/api/docs` | — | `DocsResponse` |
| POST | `/api/docs` | `NewDocRequest` | `NewDocResponse` |
| GET | `/api/docs/{id}` | — | `DocDetail` |
| GET | `/api/docs/{id}/raw` | — | `text/plain` source file bytes |
| GET | `/api/docs/{id}/history` | — | `{"commits":[{sha,subject,author,date}]}` |
| GET | `/api/docs/{id}/comments` | — | `CommentsResponse` |
| POST | `/api/docs/{id}/events` | `EventRequest` | `EventResponse` |
| POST | `/api/compact` | `CompactRequest` | `CompactResponse` |
| GET | `/api/stream` | — | SSE |
| GET | `/raw/{id}/` | — | html artifact, reviewer script already injected |
| GET | `/raw/{id}/{path...}` | — | static assets inside the artifact directory |
| GET | `/_artx/{path...}` | — | embedded frontend assets (Vite `base: '/_artx/'`) |
| GET | `/`, `/a/{id}`, any other non-`/api` path | — | SPA shell `index.html` |

`GET /api/docs/{id}` supports the query parameter `?v=<sha>`: it renders the content at that git revision, and `rev0` in the response is that sha. Historical versions are **read-only**; POSTing events to a historical version returns 409 `conflict`.

### 5.2 Request/response structures

Field names and types are in `web/src/lib/types.ts` (one-to-one with `internal/api/api.go`). This section only adds the semantics the structures themselves do not carry:

**`POST /api/docs/{id}/events`** dispatches on `type`:

| `type` | Required fields | Notes |
|---|---|---|
| `create` | `body` + (`selection` or `element`) | an md document must supply `selection`, an html document must supply `element`; the server assigns the thread id and returns it in the response's `thread` |
| `reply` | `thread`, `body` | |
| `edit` | `target`, `body` | `target` is a thread id or a reply id |
| `addressed` | `thread` | `commit` and `note` are optional |
| `resolve` | `thread` | |
| `reopen` | `thread` | `note` is optional |

When `author` is omitted the server fills it in: local mode takes `$USER`; `--token` mode takes `artx-web <display name>`, where the display name comes from the request body's `author` or defaults to `reviewer` (design doc §13, resolution 2: the token is the identity, plus a self-reported display name is allowed).

**The `SelectionInput` contract (the most important one)**: the frontend does **not** compute source-file offsets itself. It only reports

- `block_start` / `block_end`: the attribute values of the nearest ancestor element carrying `data-sourcepos` that contains the selection
- `exact`: the selected **rendered** text
- `before` / `after`: up to 64 characters of rendered text before and after the selection, within the same block

The server then quote-matches within that block's **source** to compute the final anchor (algorithm in §7.4 and §8.2). Differences between the rendered text and the source (`**bold**` → `bold`) are absorbed by the server's fuzzy matching; when matching fails it degrades to anchoring the whole block and sets `anchor.approx = true`.

### 5.3 Error responses

Uniformly `ErrorResponse{error, message, detail?}`. The `error` values and their status codes:

| `error` | HTTP | CLI exit |
|---|---|---|
| `bad_request` | 400 | 1 |
| `unauthorized` | 401 | 1 |
| `forbidden` | 403 | 1 |
| `not_found` | 404 | **2** |
| `conflict` | 409 | 1 |
| `internal` | 500 | 1 |

### 5.4 SSE

`GET /api/stream`, `Content-Type: text/event-stream`. Format:

```
event: comments
data: {"doc":"a7f3k2","threads":["c7k2f9"],"rev":"9f8e7d6"}

event: doc
data: {"doc":"a7f3k2","kind":"remap","rev":"9f8e7d6","remaps":3,"orphans":1}

event: docs
data: {}

event: ping
data: {}
```

- `comments`: that document's comments changed → the frontend invalidates `['comments', doc]`
- `doc`: that document's content changed, `kind` ∈ `content|remap|aid|remove` → invalidate `['doc', doc]`; when `kind` is `remap`, also invalidate `['comments', doc]`
- `docs`: a document was added or removed → invalidate `['docs']`
- `ping`: heartbeat every 25s, keep-alive only, ignored by the frontend

**Backpressure ruling**: when a subscriber's channel is full, the server **drops that message for that subscriber** rather than blocking. A slow client must never stall the write path; the frontend catches up on the next message, or on the full refresh after reconnecting.

### 5.5 Authentication

Local mode (no `--token`): the middleware passes everything through.

`--token` mode, check order:

1. `Authorization: Bearer <token>`
2. query parameter `?token=<token>`
3. Cookie `artx_token`

**Why the cookie is mandatory**: neither `EventSource` nor `<iframe>` can set request headers. The protocol is: the first request to any page carrying `?token=` → the server sets an HttpOnly cookie → all subsequent SSE / iframe / static-asset requests pass on the cookie alone. Once the frontend has read `?token=` it should strip it from the address bar (`history.replaceState`).

**CSRF**: every non-GET request checks `Origin` — present and not equal to this server's origin means 403; **a missing `Origin` passes** (curl and the CLI do not send that header).

The shell page's response carries `Content-Security-Policy` (see `server.CSP`). html artifacts are always loaded in `<iframe sandbox="allow-scripts">`, **never with `allow-same-origin`**.

---

## 6. Key technical rulings

### 6.1 goldmark block-level sourcepos: write our own transformer, no off-the-shelf extension

**Ruling: `ast.Walk` + `node.Lines()` to build our own block table + `SetAttributeString("data-sourcepos", …)`, plus a `NodeRenderer` that overrides code blocks only. Already implemented by the architecture layer in `internal/mdsrc`.**

Basis (measured empirically on goldmark v1.8.5, not guessed):

1. A block node's `node.Lines()` gives the `[Start, Stop)` byte range of **each source line**, with **block prefixes such as `> ` and list indentation already stripped**. Measured: the paragraph `> 引用块第一行\n> 引用块第二行` (a two-line blockquote; the sample is kept in its original CJK because the quoted byte offsets are only correct for these exact bytes) yields two segments, `[7,26)` and `[28,46)`; the `> ` between them is inside no segment. So **only concatenating the segments gives the clean block source text**; slicing `src[start:stop]` directly drags block markers in — that is the entire reason `BlockMap` exists.
2. Container blocks (`List`/`ListItem`/`Blockquote`/`Table`/`TableHeader`/`TableRow`) have an **empty** `Lines()`; their range must be aggregated from the min/max of their descendants.
3. goldmark's **default HTML renderer does emit node attributes** — measured: `<h1>`/`<p>`/`<ul>`/`<li>`/`<blockquote>`/`<table>`/`<th>`/`<td>` all carry `data-sourcepos` correctly. **The only gap is `(Fenced)CodeBlock`**, whose render function ignores attributes. So only that one node type needs overriding (~40 lines), rather than rewriting the whole renderer (~300 lines).
4. `Heading`'s `Lines()` excludes the `## ` prefix, and `FencedCodeBlock`'s excludes the ``` fence lines. This is harmless for anchoring — quote matching happens inside the block content.
5. **If frontmatter is not stripped in advance it parses as `ThematicBreak` + `Heading`** (measured: `aid: a7f3\ntitle: demo` became an h2). So `SplitFrontmatter` must run first, only the body is fed to goldmark, and `BodyOffset` is passed to the transformer through `parser.Context` and added back onto every offset. **Externally, offsets are always file-absolute.**

Why no off-the-shelf extension: the goldmark ecosystem has no maintained sourcepos extension that provides **byte offsets** (`goldmark-meta` only handles frontmatter, and line-number-level solutions are not enough for locating a quote within a block). Writing 60 lines ourselves is in fact the shortest path.

`mdsrc.NewMarkdown()` is the project's **only** goldmark factory: both `render` rendering HTML and `anchor` computing anchors must go through it, or block segmentation diverges and the anchor system collapses on the spot.

### 6.2 YAML library: `goccy/go-yaml`

**Ruling: `github.com/goccy/go-yaml v1.19.2`.**

Reasons:
- `gopkg.in/yaml.v3`'s last release was 2022-05 and upstream is archived — unsuitable as a long-term dependency for a data format;
- goccy provides **multi-document streaming decoding** via `yaml.NewDecoder(r).Decode(&v)`, exactly what a `---`-separated event stream needs (verified empirically, including the case where the first block is preceded by `---`);
- unknown fields are ignored by default, so adding event fields in the future will not break parsing in older artx builds (verified empirically);
- non-ASCII strings are emitted verbatim without quoting, multi-line bodies automatically use the `|-` block scalar, and `git diff` stays readable — this is the core reason the design doc chose YAML over JSONL.

`Event` uses **a single flat struct + `omitempty`** rather than one type per event kind. Reason: the YAML stream is heterogeneous, and a flat struct means decoding never has to "peek at `e` and then decode a second time"; `omitempty` guarantees each emitted block contains only the fields that are meaningful for that event kind.

### 6.3 flock: `syscall.Flock` + build tags, no third-party library

**Ruling: `internal/lockfile/flock_unix.go` uses `syscall.Flock` (already implemented by the architecture layer); non-unix platforms return an explicit error.**

Reason: the distribution target is a single macOS/Linux binary (brew/curl). BSD flock has identical semantics on both platforms, and **the kernel releases it automatically when the process exits (including on kill or panic)** — the serve probe protocol depends on exactly this property. A third-party library would be no better here, only one more dependency.

### 6.4 serve probe protocol: the lockfile is the proof of liveness

**Ruling: `<vault>/.artx/serve.lock`, JSON format (machine-only, hence not YAML), mode 0600.**

```json
{"pid":12345,"host":"127.0.0.1","port":7777,"token":"...","root":"/Users/x/vaults/work",
 "version":"v0.1.0","watch":true,"started_at":"2026-08-24T10:00:00+08:00"}
```

At startup, serve's `AcquireServe` takes `LOCK_EX` on that file and **holds it until the process exits**. So the CLI's probe logic is:

1. Read `serve.lock`; absent → no serve
2. `TryAcquire(LOCK_EX|LOCK_NB)` on the same file: **success means the holder is dead** → release immediately, delete the stale file → no serve
3. Cannot get the lock → serve is alive; verify `info.Root == this vault`, then return it

"Can the lock be acquired" is itself the proof of liveness — **no PID liveness check is needed, and there is no PID-reuse race**. After obtaining `ServeInfo` the CLI makes one more `GET /api/health` call to confirm `root` matches before routing (guarding against the port being held by another vault's serve).

In `--token` mode the token is written into serve.lock, so the local CLI needs no configuration — the file is mode 0600 and `.artx/serve.lock` goes into `.gitignore`.

### 6.5 CLI → serve routing

Every read/write command follows the same skeleton:

```go
v, _ := openVault()
c, _ := dial(ctx, v)     // returns (nil, nil) when no serve is detected; that is not an error
if c != nil {
    // go through the HTTP API: serve is the single writer
} else {
    // direct write: append the event block while holding the flock
}
```

Command-to-route mapping:

| Command | With serve | Without serve |
|---|---|---|
| `list` / `path` | `GET /api/docs` | local `vault.Scan` |
| `new` | `POST /api/docs` | local `vault.New` |
| `comments` | `GET /api/docs/{id}/comments` | local `vault.AllThreads` |
| `reply`/`addressed`/`resolve`/`reopen` | `POST /api/docs/{id}/events` | `Store.Append` (holding the flock) |
| `compact` | `POST /api/compact` | `Store.Compact` (holding the flock) |
| `open` | use the probed port | tell the user to run `artx serve` first, **never auto-start it** |
| `init` / `doctor` | always local | local |

`open` does not auto-start a background serve: that would make "who is the single writer" unpredictable.

### 6.6 mermaid / katex: purely client-side, zero special handling on the Go side

**Ruling: no math or diagram extension is added on the Go side.**

- **mermaid**: goldmark already emits `<pre><code class="language-mermaid">` by default (confirmed empirically). The frontend lazy-loads mermaid on that selector and calls `run()`; the Go side **needs to do nothing**.
- **katex**: no math extension on the Go side; the frontend uses katex's `auto-render` to scan the render container, with `delimiters` set to `$$`/`$` and `ignoredTags` extended with `pre`/`code`/`script`/`style`/`textarea`, so `$` inside code is not swallowed.

This satisfies the resolution to "ship mermaid/katex in M1", adds no fork to the md→HTML pipeline, and **does not violate R1** — R1 forbids the frontend re-rendering markdown; post-processing the contents of a code block is not markdown rendering.

Both libraries are lazy-loaded with Vite's dynamic `import()` (mermaid is ~3MB), pulled only when `DocDetail.has_mermaid` / `has_math` is true. **No CDN** — the binary must run offline.

### 6.7 id scheme: random base36 throughout

Design doc §13, resolution 1 only specifies that doc ids are random. **This blueprint extends that to thread and reply ids**:

| id | Shape | Example |
|---|---|---|
| doc | 6 base36 chars | `a7f3k2` |
| element (`data-aid`) | 6 base36 chars | `b2c9x1` |
| thread | `c` + 5 base36 chars | `c7k2f9` |
| reply | `<thread>.` + 3 base36 chars | `c7k2f9.x8q` |
| event (`eid`) | `base36(unixMilli)-<4 base36 chars>` | `lz3k9a2-f8q1` — within one process, uniqueness inside the same millisecond is guaranteed **by construction** (a per-millisecond randomized starting point, advanced deterministically for each subsequent call in that millisecond), not merely by chance: an independent random draw per call collides often enough during a burst — a watcher pass appending many remap events — to silently drop events at the fold dedup step. Across processes or machines the random starting points still have to not overlap, which at those far lower per-millisecond rates is negligible |

**Reason (a deliberate deviation from the design doc's `c12` / `c12.1` examples)**: when two machines each append comments to the same document, incrementing sequence numbers **inevitably collide** once they converge through `merge=union`, and an id collision makes fold merge two distinct threads — that is silent data corruption. Random ids are what make "remote = git" actually hold. CLI arguments accept unique-prefix matching, so day-to-day typing is unaffected.

### 6.8 md → HTML performs no sanitization

`render.Sanitize = false`, and goldmark enables `WithUnsafe()` to allow raw HTML. The reasoning is recorded in the code so nobody relitigates it: md content comes from a local agent and has the same authority as a file the user wrote themselves; serve listens on 127.0.0.1 only by default; html artifacts are isolated in a sandbox iframe. CSP is the shell page's fallback.

---

## 7. Frontend architecture (W-web)

Stack: React 19 + React Compiler, Vite 8 (Rolldown/Oxc), TanStack Router (SPA mode) + TanStack Query, Tailwind v4 + shadcn/ui, TS strict.

`vite.config.ts` essentials:
- `base: '/_artx/'` (matching the backend's `/_artx/*` route)
- in dev, `server.proxy` forwards `/api` and `/raw` to `http://127.0.0.1:7777`
- two entries: the main app + `src/reviewer/reviewer.ts`, the latter emitted as an **IIFE, unhashed, with the fixed filename `reviewer.js`** (the backend injects it at the fixed path `/_artx/reviewer.js`)

### 7.1 Routing

| Path | Component | Query parameters |
|---|---|---|
| `/` | `DocsIndex` | — |
| `/a/$docId` | `DocView` | `v` (git sha, to view a historical version), `t` (the focused thread id) |

The backend falls back to `index.html` for every path other than `/api`, `/raw`, and `/_art`, so client-side routing takes over.

### 7.2 Component tree

```
RootLayout                      // mounts useEventStream()
├── Header                      // vault name, search, SSE connection dot
└── <Outlet>
    ├── DocsIndex
    │   └── DocCard[]           // title, type, open count, updated time
    └── DocView
        ├── DocToolbar          // title, version picker (?v=), raw link, review mode toggle
        ├── <main>
        │   ├── MdCanvas                    // type === 'md'
        │   │   ├── (dangerouslySetInnerHTML: DocDetail.html)   ← R1
        │   │   ├── HighlightLayer          // draws highlights for existing comments by anchor.start/end
        │   │   ├── SelectionPopover        // floats a "Comment" button over the selection
        │   │   └── MermaidMath             // lazy-loaded mermaid / katex post-processing
        │   └── HtmlCanvas                  // type === 'html'
        │       ├── <iframe sandbox="allow-scripts" src={raw_url}>
        │       ├── FrameBridge             // postMessage send/receive
        │       ├── HoverOutline            // draws the outline from HoverMsg.rect
        │       └── ElementPopover          // opens the comment box on PickMsg
        └── ThreadSidebar
            ├── ThreadFilter                // open / addressed / resolved / all
            └── ThreadCard[]
                ├── AnchorPreview           // exact + orphan hint
                ├── ReplyList
                ├── ReplyComposer
                └── StatusActions           // addressed / resolve / reopen
```

### 7.3 Data flow

**Query key design** (`web/src/lib/queries.ts`):

| key | Endpoint | Notes |
|---|---|---|
| `['health']` | `GET /api/health` | `staleTime: Infinity` |
| `['docs']` | `GET /api/docs` | |
| `['doc', docId, rev]` | `GET /api/docs/{id}?v=rev` | a `rev` of `undefined` means the current working-copy version |
| `['comments', docId]` | `GET /api/docs/{id}/comments` | |

**SSE → invalidate** (`web/src/lib/sse.ts`): `RootLayout` mounts one `useEventStream()`, which opens `EventSource('/api/stream')`:

```
event 'comments' → invalidate(['comments', data.doc])
event 'doc'      → invalidate(['doc', data.doc])
                   if data.kind === 'remap', also invalidate(['comments', data.doc])
event 'docs'     → invalidate(['docs'])
event 'ping'     → ignore
```

Every write uses `useMutation` and invalidates the matching key in `onSuccess`, with **no optimistic updates** — the server fills in the `thread` id, the anchor's exact offsets, and the `approx` flag, and an optimistic update cannot guess those. SSE is for "someone else / an agent changed it"; your own writes rely on the mutation callback.

The browser reconnects `EventSource` automatically; after reconnecting, the frontend should invalidate every key to catch up on the changes missed during the gap.

### 7.4 md selection → anchor computation algorithm (frontend part)

The frontend only **collects**; it does not compute offsets (§5.2). Steps:

1. `document.getSelection()` for the `Range`; if empty or collapsed, show no popover
2. from `range.commonAncestorContainer`, walk up with `closest('[data-sourcepos]')` to find the nearest block element carrying the attribute, `blockEl`
   - if not found (the selection spans several blocks) → take the block on the `range.startContainer` side, and **shrink the selection into that block**
3. parse `blockEl.dataset.sourcepos`, splitting on `:` into `block_start` / `block_end` (decimal integers)
4. `exact = range.toString()`
5. `before` = the **last** 64 characters of the text preceding the selection start within the block:
   `r = range.cloneRange(); r.selectNodeContents(blockEl); r.setEnd(range.startContainer, range.startOffset); before = r.toString().slice(-64)`
6. `after` = likewise, the **first** 64 characters of the text following the selection end within the block
7. assemble `SelectionInput` and POST it as `EventRequest{type:'create', body, selection}`
8. the server returns `EventResponse.thread`; the frontend invalidates `['comments', docId]`

**Rendering highlights back**: `ThreadAnchor.start/end` are **source file** offsets, with no corresponding node in the DOM. The display works like this: find the block element whose `data-sourcepos` covers that range, run a DOM text search for `anchor.exact` inside it (`Range` + `TreeWalker`) and draw the highlight; when not found (`approx` or `orphan`), give the whole block a pale border. **Do not** try to convert byte offsets into DOM positions in the frontend — UTF-8 bytes and DOM characters are not the same thing, and the rendered text is not isomorphic to the source.

### 7.5 The reviewer script's postMessage protocol

Types are defined in `web/src/lib/protocol.ts` (frozen). Every message carries the `art: 1` marker; messages missing that field are ignored without exception. The marker key stays `art`, not `artx` — it is a frozen wire field, and renaming it would break every reviewer script already injected into a sandboxed frame.

**iframe → shell**: `ready{href, aidCount}`, `hover{aid, rect, tag}`, `pick{aid, rect, tag, text, quote?}`, `size{height}`, `scroll{top}`, `edit{aid, html}` (M2)

**shell → iframe**: `mode{mode: 'browse'|'review'|'edit'}`, `highlight{aids}`, `scrollTo{aid}`

**Two pitfalls that must be written into code comments**:

1. The iframe uses `sandbox="allow-scripts"` **without** `allow-same-origin`, so its origin is the string `"null"`. The shell **must** validate messages with `event.source === iframeEl.contentWindow`, and **must never** write `event.origin === location.origin` (which is never true). The shell sends messages with `postMessage(msg, '*')`.
2. The reviewer script stays **vanilla, zero-dependency** (R2). `protocol.ts` contains only type declarations and leaves no runtime code after compilation, so the reviewer may `import type` from it but must not import any value.

Reviewer behavior: in `review` mode, on `mouseover` it walks up to the nearest element carrying `data-aid` and sends `hover`; on `click` it calls `preventDefault` and sends `pick` (with an element text summary and the selection quote). In `browse` mode it does not interfere with the page at all. It sends `size` via `ResizeObserver`.

---

## 8. Work package breakdown

Four packages run in parallel, with disjoint file lists.

### W-core — vault / config / eventlog / CLI commands

**Files**
```
internal/idgen/idgen.go
internal/config/config.go
internal/gitx/gitx.go
internal/lockfile/lockfile.go          (flock_unix.go / flock_other.go are already implemented by the architecture layer — do not touch)
internal/eventlog/event.go
internal/eventlog/store.go
internal/client/client.go
internal/vault/vault.go
cmd/artx/root.go
cmd/artx/{init,new,path,list,open,comments,reply,addressed,resolve,reopen,compact,doctor}.go
+ each package's _test.go
```

**Contracts depended on**: `internal/api` (DTOs), `internal/anchor` (the `Anchor` struct — implemented by W-anchor, but the struct is frozen and usable directly), `internal/mdsrc` (already implemented).

**Acceptance criteria**
- `artx init` → `artx new x --type md --json` → write content by hand → `artx list` / `artx path` all pass
- the `[]api.Thread` emitted by `artx comments --json` is structurally identical to the HTTP endpoint's
- Unit tests that must be written:
  - `eventlog`: **the full fold semantics table** — one case per §4.4 rule; the focus is the combination of "out-of-order + duplicate eid + unknown event kind + orphaned events", because that directly determines correctness after a git merge
  - `eventlog`: `Read` on a corrupt trailing block returns the events parsed so far with `TailCorrupt=true` and **returns no error**
  - `eventlog`: `Append` concurrency safety — 8 goroutines each appending 50 events; reading back yields exactly 400 events, every block parseable
  - `eventlog`: fold results before and after `Compact` are equivalent (except for archived resolved threads)
  - `lockfile`: `Probe` returns `ErrNoServe` and cleans up the file for a stale lockfile (holder already exited)
  - `idgen`: 100,000 generations with no duplicates; `ThreadOfComment` is correct for both thread and reply ids
  - `vault`: `ResolvePath` rejects `../` traversal and symlink escapes
  - `config`: `Resolve`'s four-level precedence

### W-anchor — anchor / remap / htmlaid / watcher

**Files**
```
internal/anchor/anchor.go
internal/remap/remap.go
internal/htmlaid/htmlaid.go
internal/watcher/watcher.go
+ each package's _test.go
```

**Contracts depended on**: `internal/mdsrc` (already implemented — **this is this package's most important input**), `internal/api`, `internal/eventlog` (the Event struct, implemented by W-core, struct already frozen), `internal/vault` (Artifact / Vault, implemented by W-core).

**Acceptance criteria**
- after the md changes, open threads' anchors shift correctly; deleting the anchored text turns the thread into an orphan with `last_exact` preserved
- injecting `data-aid` into an html artifact repeatedly is idempotent; the second `Inject` reports `Changed == false`
- Unit tests that must be written:
  - `anchor`: `FromSelection` computes the correct file-absolute offsets in all three cases — "selection contains `**bold**` markers", "selection inside a blockquote", "selection inside a list item"; when in-block quote matching fails it returns a whole-block anchor with `Approx=true`
  - `anchor`: one case each for `Locate`'s three fallback levels (exact offset hit → full-text search on prefix+exact+suffix → bitap)
  - `remap`: insert content **before** the anchor → offsets shift and `Kind=moved`; change characters **inside** it → bitap hit; **delete** the whole paragraph → `Kind=orphan`; delete it then put it back → `Kind=revived`
  - `remap`: `Remap` skips threads with `status=resolved`
  - `htmlaid`: `Inject` idempotency (run twice; the second reports `Changed=false` with an empty `Added`)
  - `htmlaid`: elements that already have `data-aid` **keep their id** after the document structure changes
  - `watcher`: `Ignore` returns true for `.artx/`, `.git/`, and editor temp files
  - `watcher`: self-triggering protection — the write-back triggered by `Process` does not cause a second round of processing

### W-serve — HTTP server / render / SSE / auth

**Files**
```
internal/render/render.go
internal/server/server.go
cmd/artx/serve.go
+ each package's _test.go
```
(`internal/server/embed.go` and `dist/index.html` are provided by the architecture layer — do not touch.)

**Contracts depended on**: `internal/api`, `internal/mdsrc` (already implemented), `internal/vault`, `internal/eventlog`, `internal/anchor`, `internal/watcher`.

**Acceptance criteria**
- `Handler()` can be fully tested with `httptest` without actually listening on a port
- Unit tests that must be written:
  - `render`: the rendered `data-sourcepos` matches `mdsrc.Parse`'s block table block for block (including offsets when frontmatter is present)
  - `render`: `has_mermaid` / `has_math` detection is correct
  - `server`: `New` returns `ErrTokenRequired` when `Host=0.0.0.0` and `Token=""`
  - `server`: one case for each of `Auth`'s three token sources; a non-GET request with a wrong `Origin` returns 403, **a request with no `Origin` passes**
  - `server`: one case for each of the six `type` values of `POST /api/docs/{id}/events`, including `create` missing `selection` returning 400
  - `server`: path traversal — `GET /raw/{id}/../../etc/passwd` returns 404, not the file contents
  - `server`: `Writer` serialization — after 100 concurrent `Append` calls every event is complete and parseable
  - `server`: a blocked SSE subscriber does not stall `Broadcast` (slow-consumer case)
  - `server`: non-`/api` paths fall back to `index.html`

### W-web — the entire frontend

**Files**
```
web/package.json  web/pnpm-lock.yaml  web/vite.config.ts  web/tsconfig.json
web/tailwind.config.ts  web/components.json  web/index.html
web/src/**  (lib/types.ts and lib/protocol.ts are provided by the architecture layer — frozen, do not change fields)
```

**Contracts depended on**: **only §5 (HTTP API) and §7 (frontend architecture) of this document, plus `web/src/lib/types.ts` + `web/src/lib/protocol.ts`. Do not read the Go code.**

**How to develop**: run the backend first with `go run ./cmd/artx serve` (even with handlers unimplemented, development can proceed against any mock server built from §5); `pnpm dev` reaches :7777 through the Vite proxy.

**Acceptance criteria**
- `pnpm build` produces `web/dist/`, containing `index.html`, the hashed assets under `_art/`, and the **fixed filename** `reviewer.js`
- index page → doc page → select-and-comment → thread appears in the sidebar → resolve, fully operable end to end
- the `reviewer.ts` output **contains no React** (after building, `grep -c react dist/reviewer.js` is 0)
- Tests that must be written (vitest):
  - `lib/selection.ts`: computes the correct `SelectionInput` from a constructed DOM + Range, including the "selection spanning blocks shrinks to the start block" case
  - `lib/protocol.ts`: `isArtMessage` returns false for a message missing the `art` field
  - `lib/sse.ts`: each SSE event maps to the correct invalidate key

---

## 9. Integration and verification

### 9.1 Makefile targets

| Target | Purpose |
|---|---|
| `make go-build` | Compile Go only, using the current `internal/server/dist` (possibly the placeholder page). **This is what the four work packages use day to day** |
| `make web` | `pnpm install && pnpm build`, then copy `web/dist` into `internal/server/dist` |
| `make build` | `web` + `go-build`; the release path |
| `make placeholder` | Restore the embed directory to the placeholder page |
| `make test` / `make vet` / `make fmt` | Self-evident |
| `make check` | CI entry point: `fmt vet test` + `git diff --exit-code` |

### 9.2 The embed pipeline and placeholder strategy

`internal/server/embed.go` uses `//go:embed all:dist`. The repo permanently carries a minimal `internal/server/dist/index.html` (marked `artx-dist-placeholder`), so **`go build ./...` still passes on a machine that has never run the frontend build** — this is the precondition for the four work packages actually running in parallel.

`.gitignore` rules: `internal/server/dist/*` is ignored entirely, with `!internal/server/dist/index.html` as the exception. `make web` overwrites the whole directory with the real build output.

`server.Placeholder()` detects whether the embedded page is the placeholder; when it is true at serve startup, a conspicuous notice must be printed, so developers do not spend ages debugging against a blank page.

### 9.3 Integration order

1. **W-core is acceptable on its own**: `init` / `new` / `path` / `list` / `comments` (local path) all pass, with event stream read/write and fold backed by tests
2. **W-serve joins W-core**: `serve` starts, `/api/health` and `/api/docs` work; md pages already render at this point (`mdsrc` is implemented — no waiting on W-anchor)
3. **W-web joins W-serve**: developed against §5, with no dependency on W-anchor at any point
4. **W-anchor merges in last**: `FromSelection` makes comment creation truly precise; the watcher makes remapping take effect. Until then, `create` in `POST events` may return a block-level fallback anchor (`approx=true`) — **this is a deliberately reserved degradation path**, so the first three packages are not blocked on the fourth

### 9.4 End-to-end verification script

```bash
make build
./bin/artx init /tmp/artx-demo && cd /tmp/artx-demo
../..//bin/artx new payment-refactor --type md --json     # note the id / path / url
# write a few paragraphs of md into path with an editor
./bin/artx serve &                                        # or in the foreground in another terminal
open http://127.0.0.1:7777/                              # index page
#  → open a doc page → select some text → comment → the thread appears on the right
./bin/artx comments --open --json                         # agent's view: must include path/line/start/end/quote/context
./bin/artx reply <thread> "Condensed; merged into section 2"
./bin/artx addressed <thread>                             # the browser sidebar should update live via SSE
# edit the md, move the commented paragraph up a few lines → the watcher should emit a remap, and the browser highlight follows
# delete the commented paragraph → the thread becomes an orphan, showing last_exact and the fixed hint
#  → click resolve in the browser
./bin/artx comments --all --json                          # status should be resolved
```

Additional `--host` scenario tests: `artx serve --host 0.0.0.0` must **refuse to start**; `artx serve --host 0.0.0.0 --token secret` must start, and requests without a token must return 401.

---

## 10. Three high-risk points in parallel implementation, and the mitigations

### Risk 1: in-block offset computation written twice in two places, with inconsistent results

If md selection computation (W-anchor) and `data-sourcepos` generation (W-serve) each call goldmark separately, block segmentation will inevitably diverge subtly, anchors will be systematically misaligned, and this class of bug only surfaces with particular md structures (blockquotes, nested lists) — very easy for integration tests to miss.

**Mitigation**: the architecture layer **fully implemented `internal/mdsrc` and got its tests passing** (not a stub), and froze it as the single shared entry point for both packages; `NewMarkdown()` is the project's only goldmark factory. W-serve's rendering acceptance criteria explicitly include the test "`data-sourcepos` matches `mdsrc.Parse`'s block table block for block". `BlockMap` is also made an explicit type, forcing implementers through "segment mapping" instead of casually slicing `src[start:end]` — the latter drags the `> ` prefix in inside blockquotes, and is the easiest mistake to make.

### Risk 2: `artx comments --json` and the HTTP API output drifting apart

The same data has two output paths (CLI direct read vs. going through serve); the slightest field discrepancy makes an agent's behavior change with "whether serve is running" — a bug that is nearly undetectable during development, because the developer's serve is always up.

**Mitigation**: `internal/api` is a leaf package with no logic, and `eventlog.Fold` **produces `[]api.Thread` directly** rather than producing an internal type and converting, eliminating the two-structure problem at the source. W-core's acceptance criteria state that both paths must match field for field.

### Risk 3: watcher self-triggering, and "who is the single writer" being broken

The watcher's aid injection and auto-commit both trigger fsnotify events again; at the same time, if W-core's CLI direct-write path and W-serve's single writer are not aligned, two processes end up appending to the same file simultaneously.

**Mitigation**:
- The contract nails "single writer" down to two mutually exclusive paths (the `dial` skeleton in §6.5), and `eventlog.Store.Append` **holds the flock itself** — so even if an implementer misses the single writer, the file layer stays safe, degrading into a performance problem rather than data corruption
- On the serve side, browser/CLI event writes are forced through `server.Writer.Append`; **[Implementation ruling 2026-08-25]** remap/orphan events are persisted by the watcher itself via `Store.Append` (the `Notice` struct has no field to carry an event, and Append's flock keeps it safe); `watcher.Options.Emit` serves only as the SSE notification carrier, and the server merely broadcasts a Notice without writing files
- `watcher.Ignore` is explicitly required to ignore the entire `.artx/` directory (changes to comment files must not trigger remapping)
- W-anchor's acceptance criteria list "self-triggering protection" and "Inject idempotency" as two separate tests

---

## 11. TODO: what this blueprint does not cover

The following are M2 value-adds; the contracts have placeholders but the semantics are not yet detailed — fill them in when implementation reaches that point:

- `artx watch --dispatch "claude -p …"`: automatically dispatch a headless agent for new comments. The skeleton has no command file; the work package that reaches this stage adds `cmd/artx/watch.go`
- Editing html elements directly in the browser: `htmlaid.ReplaceElementHTML` and `protocol.ts`'s `EditMsg` are already in place; a `POST /api/docs/{id}/element` endpoint is missing
- Fleshing out the multi-vault registry: `config` already has `Register`; the `artx vault add/list/use` subcommands are missing
