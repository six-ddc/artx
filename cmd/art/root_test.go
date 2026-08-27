package main

import (
	"testing"

	"github.com/six-ddc/art/internal/api"
	"github.com/six-ddc/art/internal/vault"
)

func TestFindDocInList(t *testing.T) {
	docs := []api.Doc{
		{ID: "abc123", Slug: "doc-a"},
		{ID: "abc999", Slug: "doc-b"},
	}

	if d, err := findDocInList(docs, "doc-a"); err != nil || d.ID != "abc123" {
		t.Fatalf("findDocInList(slug) = %+v, %v", d, err)
	}
	if d, err := findDocInList(docs, "abc999"); err != nil || d.Slug != "doc-b" {
		t.Fatalf("findDocInList(id) = %+v, %v", d, err)
	}
	if d, err := findDocInList(docs, "abc1"); err != nil || d.ID != "abc123" {
		t.Fatalf("findDocInList(unique prefix) = %+v, %v", d, err)
	}
	if _, err := findDocInList(docs, "abc"); err != vault.ErrNotFound {
		t.Fatalf("findDocInList(ambiguous prefix) = %v, want ErrNotFound", err)
	}
	if _, err := findDocInList(docs, "nope"); err != vault.ErrNotFound {
		t.Fatalf("findDocInList(missing) = %v, want ErrNotFound", err)
	}
}

func TestFilterByStatus(t *testing.T) {
	threads := []api.Thread{
		{Thread: "c1", Status: api.StatusOpen},
		{Thread: "c2", Status: api.StatusResolved},
	}
	if got := filterByStatus(threads, ""); len(got) != 2 {
		t.Fatalf("filterByStatus(\"\") = %d, want 2 (no filtering)", len(got))
	}
	open := filterByStatus(threads, api.StatusOpen)
	if len(open) != 1 || open[0].Thread != "c1" {
		t.Fatalf("filterByStatus(open) = %+v", open)
	}
}

func TestLocalBaseURLDefaultsAnd0000(t *testing.T) {
	v := &vault.Vault{}
	if got := localBaseURL(v); got != "http://127.0.0.1:7777" {
		t.Errorf("localBaseURL(nil cfg) = %q, want default", got)
	}
}
