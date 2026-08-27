package config

import (
	"os"
	"path/filepath"
	"testing"
)

// isolate points XDG_CONFIG_HOME at a fresh temp dir so tests never touch
// the real user's ~/.config/artx/config.yaml.
func isolate(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("ARTX_VAULT", "")
	return dir
}

func mkVault(t *testing.T, dir string) string {
	t.Helper()
	root := filepath.Join(dir, "vault")
	if err := os.MkdirAll(filepath.Join(root, ".artx"), 0o755); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestGlobalRoundTrip(t *testing.T) {
	dir := isolate(t)

	g, err := LoadGlobal()
	if err != nil {
		t.Fatalf("LoadGlobal (missing file): %v", err)
	}
	if g.DefaultVault != "" || len(g.Vaults) != 0 {
		t.Fatalf("LoadGlobal (missing file) = %+v, want zero value", g)
	}

	g.DefaultVault = "work"
	g.Vaults = map[string]string{"work": "/x/work"}
	if err := SaveGlobal(g); err != nil {
		t.Fatalf("SaveGlobal: %v", err)
	}

	path, err := GlobalFilePath()
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(filepath.Dir(path)) != dir {
		t.Fatalf("GlobalFilePath = %q, want under %q", path, dir)
	}

	got, err := LoadGlobal()
	if err != nil {
		t.Fatalf("LoadGlobal: %v", err)
	}
	if got.DefaultVault != "work" || got.Vaults["work"] != "/x/work" {
		t.Fatalf("LoadGlobal = %+v, want round-tripped content", got)
	}
}

func TestRegisterSetsDefaultOnFirst(t *testing.T) {
	isolate(t)

	if err := Register("work", "/x/work"); err != nil {
		t.Fatalf("Register: %v", err)
	}
	g, err := LoadGlobal()
	if err != nil {
		t.Fatal(err)
	}
	if g.DefaultVault != "work" {
		t.Fatalf("DefaultVault = %q, want %q (first registration)", g.DefaultVault, "work")
	}

	if err := Register("personal", "/x/personal"); err != nil {
		t.Fatalf("Register (2nd): %v", err)
	}
	g, err = LoadGlobal()
	if err != nil {
		t.Fatal(err)
	}
	if g.DefaultVault != "work" {
		t.Fatalf("DefaultVault = %q, want unchanged %q after 2nd registration", g.DefaultVault, "work")
	}
	if g.Vaults["personal"] != "/x/personal" {
		t.Fatalf("Vaults[personal] = %q, want /x/personal", g.Vaults["personal"])
	}
}

func TestVaultDefaults(t *testing.T) {
	root := t.TempDir()
	v, err := LoadVault(root)
	if err != nil {
		t.Fatalf("LoadVault (no config file): %v", err)
	}
	if v.Port != DefaultPort {
		t.Errorf("Port = %d, want %d", v.Port, DefaultPort)
	}
	if v.Host != DefaultHost {
		t.Errorf("Host = %q, want %q", v.Host, DefaultHost)
	}
	if v.AutoCommit == nil || !*v.AutoCommit {
		t.Errorf("AutoCommit = %v, want true", v.AutoCommit)
	}
	if v.Watch == nil || !*v.Watch {
		t.Errorf("Watch = %v, want true", v.Watch)
	}
	if v.Debounce() != DefaultDebounceMS*1_000_000 {
		t.Errorf("Debounce() = %v, want %dms", v.Debounce(), DefaultDebounceMS)
	}
}

func TestVaultRoundTripOverridesDefaults(t *testing.T) {
	root := t.TempDir()
	f := false
	v := &Vault{Name: "work", Port: 9999, AutoCommit: &f}
	if err := SaveVault(root, v); err != nil {
		t.Fatalf("SaveVault: %v", err)
	}

	got, err := LoadVault(root)
	if err != nil {
		t.Fatalf("LoadVault: %v", err)
	}
	if got.Port != 9999 {
		t.Errorf("Port = %d, want 9999 (explicit value preserved)", got.Port)
	}
	if got.AutoCommit == nil || *got.AutoCommit {
		t.Errorf("AutoCommit = %v, want false (explicit false preserved, not defaulted)", got.AutoCommit)
	}
	if got.Host != DefaultHost {
		t.Errorf("Host = %q, want default %q for unset field", got.Host, DefaultHost)
	}
}

func TestFindRoot(t *testing.T) {
	dir := t.TempDir()
	root := mkVault(t, dir)
	sub := filepath.Join(root, "artifact", "nested")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := FindRoot(sub)
	if err != nil || got != root {
		t.Fatalf("FindRoot(sub) = %q, %v; want %q", got, err, root)
	}

	if _, err := FindRoot(t.TempDir()); err != ErrNoVault {
		t.Fatalf("FindRoot(unrelated dir) = %v, want ErrNoVault", err)
	}
}

// TestResolveFourLevelPriority is the required acceptance test for
// config.Resolve's four-level priority: explicit flag > ARTX_VAULT env >
// upward .artx/ search from cwd > registry default_vault.
func TestResolveFourLevelPriority(t *testing.T) {
	base := isolate(t)
	rootA := mkVault(t, filepath.Join(base, "a"))
	rootB := mkVault(t, filepath.Join(base, "b"))

	if err := Register("a", rootA); err != nil {
		t.Fatal(err)
	}
	if err := Register("b", rootB); err != nil {
		t.Fatal(err)
	}
	// Register makes "a" the default (first registration).

	// Level 4: no explicit, no env, cwd outside any vault -> registry default.
	root, name, err := Resolve("", t.TempDir())
	if err != nil {
		t.Fatalf("Resolve (level 4): %v", err)
	}
	if root != rootA || name != "a" {
		t.Fatalf("Resolve (level 4) = (%q,%q), want (%q,%q)", root, name, rootA, "a")
	}

	// Level 3: cwd inside vault B beats the registry default (still "a").
	cwdInB := filepath.Join(rootB, "some-artifact")
	if err := os.MkdirAll(cwdInB, 0o755); err != nil {
		t.Fatal(err)
	}
	root, name, err = Resolve("", cwdInB)
	if err != nil {
		t.Fatalf("Resolve (level 3): %v", err)
	}
	if root != rootB || name != "b" {
		t.Fatalf("Resolve (level 3) = (%q,%q), want (%q,%q)", root, name, rootB, "b")
	}

	// Level 2: ARTX_VAULT=a beats cwd being inside vault B.
	t.Setenv("ARTX_VAULT", "a")
	root, name, err = Resolve("", cwdInB)
	if err != nil {
		t.Fatalf("Resolve (level 2): %v", err)
	}
	if root != rootA || name != "a" {
		t.Fatalf("Resolve (level 2) = (%q,%q), want (%q,%q)", root, name, rootA, "a")
	}

	// Level 1: explicit --vault=b beats ARTX_VAULT=a.
	root, name, err = Resolve("b", cwdInB)
	if err != nil {
		t.Fatalf("Resolve (level 1): %v", err)
	}
	if root != rootB || name != "b" {
		t.Fatalf("Resolve (level 1) = (%q,%q), want (%q,%q)", root, name, rootB, "b")
	}
	t.Setenv("ARTX_VAULT", "")

	// Explicit can also be a raw filesystem path, not just a registered name.
	root, name, err = Resolve(rootB, t.TempDir())
	if err != nil {
		t.Fatalf("Resolve (explicit path): %v", err)
	}
	if root != rootB {
		t.Fatalf("Resolve (explicit path) root = %q, want %q", root, rootB)
	}
}

func TestResolveNoVault(t *testing.T) {
	isolate(t)
	if _, _, err := Resolve("", t.TempDir()); err != ErrNoVault {
		t.Fatalf("Resolve with nothing registered = %v, want ErrNoVault", err)
	}
}
