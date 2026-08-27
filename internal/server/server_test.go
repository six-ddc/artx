package server

import (
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/six-ddc/art/internal/api"
	"github.com/six-ddc/art/internal/vault"
)

func TestNewRequiresTokenForNonLoopbackHost(t *testing.T) {
	_, err := New(Options{Vault: &vault.Vault{Root: "/tmp/x", Name: "x"}, Host: "0.0.0.0", Token: ""})
	if !errors.Is(err, ErrTokenRequired) {
		t.Fatalf("Host=0.0.0.0 with Token=\"\" should return ErrTokenRequired, got %v", err)
	}
}

func TestNewAllowsNonLoopbackHostWithToken(t *testing.T) {
	s, err := New(Options{Vault: &vault.Vault{Root: "/tmp/x", Name: "x"}, Host: "0.0.0.0", Token: "secret"})
	if err != nil {
		t.Fatalf("should be able to build a Server with a token: %v", err)
	}
	if s == nil {
		t.Fatal("Server should not be nil")
	}
}

func TestNewDefaultsToLoopbackNoTokenNeeded(t *testing.T) {
	if _, err := New(Options{Vault: &vault.Vault{Root: "/tmp/x", Name: "x"}}); err != nil {
		t.Fatalf("default 127.0.0.1 should not need a token: %v", err)
	}
	if _, err := New(Options{Vault: &vault.Vault{Root: "/tmp/x", Name: "x"}, Host: "127.0.0.1"}); err != nil {
		t.Fatalf("explicit 127.0.0.1 should not need a token: %v", err)
	}
	if _, err := New(Options{Vault: &vault.Vault{Root: "/tmp/x", Name: "x"}, Host: "localhost"}); err != nil {
		t.Fatalf("localhost should not need a token: %v", err)
	}
}

func TestWriteErrorAndWriteJSON(t *testing.T) {
	w := httptest.NewRecorder()
	WriteError(w, 404, api.ErrNotFound, "nope")
	if w.Code != 404 {
		t.Fatalf("status = %d, want 404", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json; charset=utf-8" {
		t.Fatalf("Content-Type = %q", ct)
	}
	if !strings.Contains(w.Body.String(), `"error":"not_found"`) {
		t.Fatalf("body missing error code: %s", w.Body.String())
	}
}
