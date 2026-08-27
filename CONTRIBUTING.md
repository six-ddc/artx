# Contributing

Thanks for looking. `artx` is a small Go binary with an embedded React UI, and the build reflects that: the frontend compiles to static assets that `go:embed` bakes into the binary, so a released `artx` has no runtime dependencies.

## Development environment

| Tool | Version | Needed for |
|---|---|---|
| Go | 1.24+ | everything |
| Node | 22+ | the web UI |
| pnpm | any recent | the web UI |
| git | any | vaults are git repos; `artx` shells out to the system `git` |

You do not need Node to work on the Go side. The repo keeps a placeholder page committed at `internal/server/dist/index.html`, so `go build ./...` succeeds on a machine that has never run a frontend build. `artx serve` prints a warning when it is serving that placeholder, so you are not left debugging a blank page.

```bash
git clone https://github.com/six-ddc/artx
cd artx
make go-build        # Go only, against the placeholder UI
./bin/artx init /tmp/demo && cd /tmp/demo && artx serve
```

Try `artx` in a scratch directory, never in this repository. `artx init` turns
whatever directory it is given into a vault and commits the scaffolding —
`AGENTS.md`, `.gitattributes`, `.artx/config.yaml` — into the git repo it lands
in. Run it at the root of a checkout and it commits vault scaffolding into the
project history.

For frontend work, run the backend and the Vite dev server side by side. Vite proxies `/api` and `/raw` to `:7777`.

```bash
# terminal 1
go run ./cmd/artx serve
# terminal 2
cd web && pnpm dev
```

## Make targets

| Target | What it does |
|---|---|
| `make go-build` | Compile the Go binary against whatever is in `internal/server/dist` — possibly the placeholder. The everyday target. |
| `make web` | `pnpm install --frozen-lockfile && pnpm build`, then copy `web/dist` into `internal/server/dist`. |
| `make build` | `web` then `go-build`. The release path. |
| `make placeholder` | Restore `internal/server/dist` to the placeholder page. Run this after `make web` to leave the working tree clean — build output must never be committed. |
| `make test` | `go test ./...` |
| `make vet` | `go vet ./...` |
| `make fmt` | `gofmt -w` over `./cmd` and `./internal` |
| `make check` | CI entry point: `vet`, `test`, a read-only `gofmt -l` check, and `git diff --exit-code`. |
| `make e2e` | Full publish → comment → remap → orphan → resolve loop against a real `artx serve`, driving the HTTP API with `curl`. Needs `make build` first. |
| `make smoke` | Mount the real component tree in headless Chromium against a real server. Catches render crashes that unit tests, `tsc`, and `vite build` all miss. Needs `make build` first. |

Frontend-only checks live in `web/`: `pnpm test` (vitest), `pnpm typecheck` (`tsc --noEmit`).

## Architecture

Read these before a non-trivial change:

- [`docs/design.md`](docs/design.md) — what the product is, what it deliberately is not, and why each technology was chosen. Sections 6 and 7 (the comment event stream, and anchoring plus remapping) carry most of the conceptual weight.
- [`docs/blueprint.md`](docs/blueprint.md) — how it is implemented. Section 0 lists five design red lines that no change may violate; section 2 has the package dependency graph; sections 4 and 5 are the YAML event schema and HTTP API contract.

The short version of the layering: `internal/api` is a logic-free leaf holding the DTOs. `internal/mdsrc` owns markdown source positions and is the single goldmark factory for the whole project. `internal/eventlog` reads, folds, and compacts the comment stream. `internal/vault` is the facade over a vault directory. `internal/anchor`, `remap`, `htmlaid`, and `watcher` maintain anchor correctness as documents change. `internal/server` and `internal/render` serve it; `cmd/artx` is the CLI. The graph is acyclic — check that a new cross-package import keeps it that way, and in particular never import `eventlog` from `anchor`.

## Frozen contracts

Two sets of field definitions are a contract across languages and across process boundaries. Changing a name, a type, or a JSON tag in either one silently breaks the other side:

- `internal/api/api.go` — the DTOs shared by the Go internals, the CLI's `--json` output, and the HTTP API.
- `web/src/lib/types.ts` and `web/src/lib/protocol.ts` — the TypeScript mirror of those DTOs, and the `postMessage` protocol between the shell page and the reviewer script inside the sandboxed iframe.

`artx comments --json` and `GET /api/docs/{id}/comments` must emit byte-identical JSON for the same data. They are two code paths over one structure precisely so that an agent's behavior does not drift depending on whether `artx serve` happens to be running — a bug that is nearly invisible in development, because a developer's serve is always up. If you touch either path, verify both.

If a contract change is genuinely necessary, change `api.go`, `types.ts`, and `protocol.ts` in the same commit, and update the corresponding tables in `docs/blueprint.md`.

## Tests

New code comes with tests, and `make check` has to pass before a PR is ready.

`docs/blueprint.md` section 8 lists, per work package, the specific cases that must be covered — event folding rules, corrupt-tail tolerance, concurrent appends, anchor remapping across insertions and deletions, `data-aid` injection idempotence, path traversal, SSE backpressure. Treat that list as the floor, not the ceiling.

Two areas deserve extra care because their bugs are quiet:

- **Anchoring and remapping.** Failures surface only on particular markdown structures (blockquotes, nested lists) and only after a document is edited. Add a case for the structure you touched.
- **Event folding.** Fold order determines what happens after a `git pull` merges two machines' comments with `merge=union`. Cover out-of-order events, duplicate event ids, unknown event types, and events referencing threads that do not exist.

## Commits and pull requests

Conventional Commits, in the imperative mood:

```
<type>(<scope>): <subject>

feat(anchor): fall back to bitap when quote verification fails
fix(server): reject path traversal in /raw before opening the file
docs(blueprint): document the watcher self-triggering guard
```

Types: `feat`, `fix`, `docs`, `test`, `refactor`, `perf`, `build`, `ci`, `chore`. Scope is the package or area (`anchor`, `eventlog`, `server`, `web`, `cli`). Keep the subject under ~72 characters and skip the trailing period. Explain *why* in the body when the reason is not obvious from the diff; the diff already says what.

For pull requests: one logical change per PR, `make check` passing, and a description that says what changed and how you verified it. If you changed anything under `web/`, run `make build && make smoke` too — the unit tests never mount the component tree.

## Reporting bugs

Include your OS, `artx --version`, and the smallest artifact that reproduces the problem. For anchoring bugs, the document content matters more than anything else — attach the markdown, not a screenshot of it.
