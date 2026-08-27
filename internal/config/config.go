// Package config manages the global vault registry and per-vault configuration.
//
// Owner: W-core.
package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/goccy/go-yaml"
)

// GlobalDir and GlobalFile together form the path relative to the user's
// config directory: ~/.config/art/config.yaml.
const (
	GlobalDir  = "art"
	GlobalFile = "config.yaml"
)

// VaultConfigPath is the vault config path relative to the vault root.
const VaultConfigPath = ".art/config.yaml"

// ErrNoVault indicates the current context could not determine a vault.
var ErrNoVault = errors.New("config: no vault found")

// ErrVaultNotRegistered indicates the registry has no vault by that name.
var ErrVaultNotRegistered = errors.New("config: vault not registered")

// Global is the content of ~/.config/art/config.yaml.
type Global struct {
	DefaultVault string            `yaml:"default_vault,omitempty"`
	Vaults       map[string]string `yaml:"vaults,omitempty"` // name -> absolute path (supports ~ expansion)
}

// Vault is the content of <vault>/.art/config.yaml.
type Vault struct {
	Name       string `yaml:"name,omitempty"`
	Port       int    `yaml:"port,omitempty"`        // default 7777
	Host       string `yaml:"host,omitempty"`        // default 127.0.0.1
	AutoCommit *bool  `yaml:"auto_commit,omitempty"` // default true
	Watch      *bool  `yaml:"watch,omitempty"`       // default true

	CompactSizeKB       int `yaml:"compact_size_kb,omitempty"`       // default 256
	CompactResolvedDays int `yaml:"compact_resolved_days,omitempty"` // default 30

	DebounceMS int `yaml:"debounce_ms,omitempty"` // watcher debounce, default 300

	// Author overrides the comment author name. When empty, single-machine
	// mode falls back to $USER (design doc §13, decision 2).
	Author string `yaml:"author,omitempty"`
}

// Default values.
const (
	DefaultPort       = 7777
	DefaultHost       = "127.0.0.1"
	DefaultDebounceMS = 300
)

const (
	defaultCompactSizeKB       = 256
	defaultCompactResolvedDays = 30
)

// GlobalFilePath returns the absolute path to the global registry:
// ~/.config/art/config.yaml (honoring XDG_CONFIG_HOME, falling back to
// ~/.config when unset, regardless of platform — design doc §4 pins this
// path deliberately and does not use macOS's ~/Library/Application Support).
func GlobalFilePath() (string, error) {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, GlobalDir, GlobalFile), nil
}

// LoadGlobal reads the global registry. A missing file returns a zero value
// rather than an error.
func LoadGlobal() (*Global, error) {
	path, err := GlobalFilePath()
	if err != nil {
		return nil, err
	}
	g := &Global{}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return g, nil
		}
		return nil, err
	}
	if err := yaml.Unmarshal(data, g); err != nil {
		return nil, err
	}
	return g, nil
}

// SaveGlobal writes the global registry back to disk, creating directories
// as needed.
func SaveGlobal(g *Global) error {
	path, err := GlobalFilePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := yaml.Marshal(g)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// Register writes name -> path into the registry; when this is the first
// registered vault, it also becomes the default_vault.
func Register(name, path string) error {
	g, err := LoadGlobal()
	if err != nil {
		return err
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	first := len(g.Vaults) == 0
	if g.Vaults == nil {
		g.Vaults = map[string]string{}
	}
	g.Vaults[name] = abs
	if first || g.DefaultVault == "" {
		g.DefaultVault = name
	}
	return SaveGlobal(g)
}

// LoadVault reads the vault config, filling in default values for unset
// fields.
func LoadVault(root string) (*Vault, error) {
	path := filepath.Join(root, VaultConfigPath)
	v := &Vault{}
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}
	} else if err := yaml.Unmarshal(data, v); err != nil {
		return nil, err
	}
	applyDefaults(v)
	return v, nil
}

func applyDefaults(v *Vault) {
	if v.Port == 0 {
		v.Port = DefaultPort
	}
	if v.Host == "" {
		v.Host = DefaultHost
	}
	if v.AutoCommit == nil {
		t := true
		v.AutoCommit = &t
	}
	if v.Watch == nil {
		t := true
		v.Watch = &t
	}
	if v.CompactSizeKB == 0 {
		v.CompactSizeKB = defaultCompactSizeKB
	}
	if v.CompactResolvedDays == 0 {
		v.CompactResolvedDays = defaultCompactResolvedDays
	}
	if v.DebounceMS == 0 {
		v.DebounceMS = DefaultDebounceMS
	}
}

// SaveVault writes the vault config back to disk.
func SaveVault(root string, v *Vault) error {
	path := filepath.Join(root, VaultConfigPath)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := yaml.Marshal(v)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// Debounce returns the watcher debounce duration.
func (v *Vault) Debounce() time.Duration {
	ms := v.DebounceMS
	if ms == 0 {
		ms = DefaultDebounceMS
	}
	return time.Duration(ms) * time.Millisecond
}

// Resolve decides which vault the current command applies to, by priority:
//  1. an explicit flag (--vault <name|path>, i.e. the explicit parameter)
//  2. the ART_VAULT environment variable
//  3. an upward search from cwd for a directory containing .art/
//  4. the registry's default_vault
//
// It returns the vault's absolute path and its name in the registry (falling
// back to the directory's base name if it isn't registered).
func Resolve(explicit, cwd string) (root, name string, err error) {
	g, err := LoadGlobal()
	if err != nil {
		return "", "", err
	}

	resolveRegistered := func(ref string) (string, string, bool) {
		if p, ok := g.Vaults[ref]; ok {
			return expandHome(p), ref, true
		}
		return "", "", false
	}

	nameForPath := func(p string) string {
		for n, vp := range g.Vaults {
			if expandHome(vp) == p {
				return n
			}
		}
		return filepath.Base(p)
	}

	candidate := explicit
	if candidate == "" {
		candidate = os.Getenv("ART_VAULT")
	}
	if candidate != "" {
		if root, name, ok := resolveRegistered(candidate); ok {
			return root, name, nil
		}
		abs, aerr := filepath.Abs(expandHome(candidate))
		if aerr != nil {
			return "", "", aerr
		}
		return abs, nameForPath(abs), nil
	}

	if found, ferr := FindRoot(cwd); ferr == nil {
		return found, nameForPath(found), nil
	}

	if g.DefaultVault != "" {
		if root, name, ok := resolveRegistered(g.DefaultVault); ok {
			return root, name, nil
		}
	}

	return "", "", ErrNoVault
}

// FindRoot searches upward from dir for a directory containing .art/.
func FindRoot(dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	cur := abs
	for {
		info, statErr := os.Stat(filepath.Join(cur, ".art"))
		if statErr == nil && info.IsDir() {
			return cur, nil
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return "", ErrNoVault
		}
		cur = parent
	}
}

func expandHome(p string) string {
	if p == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
		return p
	}
	if strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[2:])
		}
	}
	return p
}
