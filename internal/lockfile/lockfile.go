// Package lockfile provides two things:
//
//  1. a file-level advisory lock (flock), used to serialize the CLI's direct
//     writes to the event log;
//  2. the serve-detection protocol: the CLI uses this to determine whether a
//     serve is already running for this vault, and if so, routes writes to
//     its HTTP API instead (design doc §6.4: serve is the sole writer while
//     it's running).
//
// Owner: W-core.
//
// The key property of the detection protocol: once serve starts, it **holds**
// an exclusive LOCK_EX on serve.lock until the process exits. So whether we
// can acquire LOCK_EX|LOCK_NB on that file is itself proof of serve's
// liveness — no PID-based liveness check is needed, and there's no PID-reuse
// race, because the kernel releases the flock automatically when the process
// dies.
package lockfile

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// ServeLockName is the serve-detection file's path relative to the vault
// root.
const ServeLockName = ".artx/serve.lock"

// ErrLocked indicates a non-blocking lock acquisition failed because someone
// else already holds the lock.
var ErrLocked = errors.New("lockfile: already locked")

// ErrNoServe indicates this vault currently has no running serve.
var ErrNoServe = errors.New("lockfile: no running serve")

// ServeInfo is the JSON-encoded content of serve.lock (JSON rather than YAML
// since it's machine-only). The file's permissions must be 0600, since it
// carries a token when --token is used.
type ServeInfo struct {
	PID       int       `json:"pid"`
	Host      string    `json:"host"` // listen address, e.g. 127.0.0.1
	Port      int       `json:"port"`
	Token     string    `json:"token,omitempty"` // set when --token is used, letting the local CLI call in without extra config
	Root      string    `json:"root"`            // vault's absolute path, used by the CLI to confirm it's the same vault
	Version   string    `json:"version"`
	Watch     bool      `json:"watch"`
	StartedAt time.Time `json:"started_at"`
}

// BaseURL returns http://host:port, substituting 127.0.0.1 when host is
// 0.0.0.0.
func (s *ServeInfo) BaseURL() string {
	host := s.Host
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return fmt.Sprintf("http://%s:%d", host, s.Port)
}

// Lock is a handle on an already-held flock.
type Lock struct {
	f *os.File
}

// File returns the underlying file handle, for reading and writing the same
// file while the lock is held.
func (l *Lock) File() *os.File { return l.f }

// Unlock releases the lock and closes the file. Safe to call repeatedly.
func (l *Lock) Unlock() error {
	if l == nil || l.f == nil {
		return nil
	}
	_ = funlock(l.f)
	err := l.f.Close()
	l.f = nil
	return err
}

// Acquire blocks while acquiring an exclusive flock on path, returning
// ErrLocked on timeout. The file is created with mode if it doesn't exist.
// timeout <= 0 means wait indefinitely.
func Acquire(path string, mode os.FileMode, timeout time.Duration) (*Lock, error) {
	if timeout <= 0 {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, mode)
		if err != nil {
			return nil, err
		}
		if err := flockEx(f, false); err != nil {
			_ = f.Close()
			return nil, err
		}
		return &Lock{f: f}, nil
	}

	deadline := time.Now().Add(timeout)
	for {
		lock, err := TryAcquire(path, mode)
		if err == nil {
			return lock, nil
		}
		if !errors.Is(err, ErrLocked) {
			return nil, err
		}
		if time.Now().After(deadline) {
			return nil, ErrLocked
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TryAcquire acquires an exclusive flock without blocking, returning
// ErrLocked on failure.
func TryAcquire(path string, mode os.FileMode) (*Lock, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, mode)
	if err != nil {
		return nil, err
	}
	if err := flockEx(f, true); err != nil {
		_ = f.Close()
		return nil, err
	}
	return &Lock{f: f}, nil
}

// WithLock runs fn while holding the lock, guaranteeing release before it
// returns.
func WithLock(path string, mode os.FileMode, timeout time.Duration, fn func(f *os.File) error) error {
	lock, err := Acquire(path, mode, timeout)
	if err != nil {
		return err
	}
	defer lock.Unlock()
	return fn(lock.File())
}

// AcquireServe is called by artx serve at startup: it claims
// <root>/.artx/serve.lock and writes info into it. It returns ErrLocked when
// a serve is already running, and the caller should error out. The returned
// Lock must be held until the process exits.
func AcquireServe(root string, info ServeInfo) (*Lock, error) {
	path := filepath.Join(root, ServeLockName)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	lock, err := TryAcquire(path, 0o600)
	if err != nil {
		return nil, err
	}
	f := lock.File()
	if err := f.Truncate(0); err != nil {
		lock.Unlock()
		return nil, err
	}
	if _, err := f.Seek(0, 0); err != nil {
		lock.Unlock()
		return nil, err
	}
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(info); err != nil {
		lock.Unlock()
		return nil, err
	}
	if err := f.Sync(); err != nil {
		lock.Unlock()
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		lock.Unlock()
		return nil, err
	}
	return lock, nil
}

// Probe detects whether a serve is running for this vault.
//
// Steps:
//  1. Read <root>/.artx/serve.lock; if it doesn't exist -> ErrNoServe.
//  2. TryAcquire the same file: success means the previous holder is dead ->
//     release immediately, remove the stale file, and return ErrNoServe.
//  3. Failing to get the lock means serve is alive -> validate that
//     info.Root matches root, then return it.
//
// After obtaining a ServeInfo, callers should confirm it with a GET
// /api/health call before routing writes to it.
func Probe(root string) (*ServeInfo, error) {
	path := filepath.Join(root, ServeLockName)

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNoServe
		}
		return nil, err
	}

	lock, err := TryAcquire(path, 0o600)
	if err == nil {
		// We got the lock: whoever wrote it is gone. Clean up.
		lock.Unlock()
		_ = os.Remove(path)
		return nil, ErrNoServe
	}
	if !errors.Is(err, ErrLocked) {
		return nil, err
	}

	var info ServeInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return nil, ErrNoServe
	}
	if info.Root != root {
		return nil, ErrNoServe
	}
	return &info, nil
}
