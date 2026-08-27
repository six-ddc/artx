SHELL := /bin/bash
BIN   := bin/art
PKG   := github.com/six-ddc/art
VERSION ?= dev
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
LDFLAGS := -X $(PKG)/internal/version.Version=$(VERSION) -X $(PKG)/internal/version.Commit=$(COMMIT)

DIST        := internal/server/dist
PLACEHOLDER := scripts/placeholder.html

.PHONY: all build web go-build test vet fmt clean dev placeholder check e2e smoke

all: build

## build: build the frontend, then compile the binary (release path); produces ./bin/art
build: web go-build

## web: build the frontend with pnpm and sync all of web/dist into the embed directory
web:
	cd web && pnpm install --frozen-lockfile && pnpm build
	rm -rf $(DIST)
	cp -R web/dist $(DIST)
	@echo "note: $(DIST) now holds the real frontend; run make placeholder to restore the placeholder"

## go-build: compile Go only, using whatever $(DIST) currently holds (possibly the placeholder)
go-build:
	mkdir -p bin
	go build -ldflags '$(LDFLAGS)' -o $(BIN) ./cmd/art

## placeholder: restore the embed directory to the placeholder page. Useful for
## working without a frontend toolchain, and for cleaning the tree after
## make web (the real build must never land in git).
placeholder:
	rm -rf $(DIST)
	mkdir -p $(DIST)
	cp $(PLACEHOLDER) $(DIST)/index.html

## dev: backend on :7777; the vite dev server proxies /api and /raw to it
dev:
	@echo "terminal 1: go run ./cmd/art serve"
	@echo "terminal 2: cd web && pnpm dev"

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -l -w ./cmd ./internal

## e2e: end-to-end verification; requires make build first
e2e:
	scripts/e2e.sh

## smoke: browser smoke test. Starts a real serve, builds a document with
## nested headings, lists and a reply-less thread, and mounts the component
## tree once in headless Chromium. Requires make build first.
smoke:
	scripts/smoke.sh

## check: CI entry point. gofmt runs read-only here (unlike make fmt, which
## rewrites), so formatting can be judged without git; inside a git work tree
## it additionally requires a clean tree.
check: vet test
	@out=$$(gofmt -l ./cmd ./internal); \
	if [[ -n "$$out" ]]; then echo "these files are not gofmt'd; run make fmt:"; echo "$$out"; exit 1; fi
	@if git rev-parse --is-inside-work-tree >/dev/null 2>&1; then \
	  git diff --exit-code || { echo "the working tree has uncommitted changes"; exit 1; }; \
	fi

clean:
	rm -rf bin web/dist
