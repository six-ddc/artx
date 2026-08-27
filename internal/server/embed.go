package server

import (
	"bytes"
	"embed"
	"errors"
	"io/fs"
)

// dist holds the built web/ frontend.
//
// Placeholder strategy: the repo keeps a minimal
// internal/server/dist/index.html checked in, so `go build ./...` still
// succeeds on a machine that **hasn't run the frontend build**, keeping the
// Go-side work packages from blocking on each other. `make web` overwrites
// it with the real build output from web/dist (see the Makefile); releases
// go through `make build`.
//
// The all: prefix ensures files starting with _ or . (which Vite's hashed
// assets may) are embedded too.
//
//go:embed all:dist
var dist embed.FS

// Placeholder reports whether the currently embedded frontend is the
// placeholder page. If true when serve starts, a prominent warning should
// be printed so developers don't spend time debugging a blank page.
func Placeholder() bool {
	b, err := dist.ReadFile("dist/index.html")
	if err != nil {
		return true
	}
	return bytes.Contains(b, []byte(PlaceholderMarker))
}

// DistFS returns a filesystem rooted at dist.
func DistFS() (fs.FS, error) { return fs.Sub(dist, "dist") }

// PlaceholderMarker is the marker string embedded in the placeholder page;
// Placeholder() checks for its presence.
const PlaceholderMarker = "art-dist-placeholder"

var errTokenRequired = errors.New("server: --host requires --token")
