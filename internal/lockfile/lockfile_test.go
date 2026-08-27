package lockfile

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTryAcquireExclusive(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.lock")

	l1, err := TryAcquire(path, 0o600)
	if err != nil {
		t.Fatalf("first TryAcquire: %v", err)
	}
	defer l1.Unlock()

	if _, err := TryAcquire(path, 0o600); !errors.Is(err, ErrLocked) {
		t.Fatalf("second TryAcquire = %v, want ErrLocked", err)
	}

	if err := l1.Unlock(); err != nil {
		t.Fatalf("Unlock: %v", err)
	}
	l2, err := TryAcquire(path, 0o600)
	if err != nil {
		t.Fatalf("TryAcquire after unlock: %v", err)
	}
	l2.Unlock()
}

func TestAcquireTimeout(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.lock")

	l1, err := TryAcquire(path, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer l1.Unlock()

	start := time.Now()
	_, err = Acquire(path, 0o600, 100*time.Millisecond)
	if !errors.Is(err, ErrLocked) {
		t.Fatalf("Acquire with timeout = %v, want ErrLocked", err)
	}
	if elapsed := time.Since(start); elapsed < 90*time.Millisecond {
		t.Fatalf("Acquire returned too fast: %v", elapsed)
	}
}

func TestWithLock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.lock")

	err := WithLock(path, 0o600, time.Second, func(f *os.File) error {
		_, err := f.WriteString("hello")
		return err
	})
	if err != nil {
		t.Fatalf("WithLock: %v", err)
	}

	// Lock must be released by now.
	l, err := TryAcquire(path, 0o600)
	if err != nil {
		t.Fatalf("TryAcquire after WithLock: %v", err)
	}
	l.Unlock()

	b, err := os.ReadFile(path)
	if err != nil || string(b) != "hello" {
		t.Fatalf("file content = %q, %v; want %q", b, err, "hello")
	}
}

func TestProbeNoLockfile(t *testing.T) {
	root := t.TempDir()
	if _, err := Probe(root); !errors.Is(err, ErrNoServe) {
		t.Fatalf("Probe on empty vault = %v, want ErrNoServe", err)
	}
}

// TestProbeStaleLockfileCleaned is the required acceptance test: a
// serve.lock left behind by a process that no longer holds the flock (e.g.
// it crashed) must be reported as ErrNoServe and removed from disk.
func TestProbeStaleLockfileCleaned(t *testing.T) {
	root := t.TempDir()
	lockPath := filepath.Join(root, ServeLockName)
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		t.Fatal(err)
	}
	info := ServeInfo{PID: 999999, Host: "127.0.0.1", Port: 7777, Root: root}
	// Write the info file directly, without holding any flock on it —
	// simulating a process that died without releasing the lock (the OS
	// already released it on exit).
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.NewEncoder(f).Encode(info); err != nil {
		t.Fatal(err)
	}
	f.Close()

	got, err := Probe(root)
	if !errors.Is(err, ErrNoServe) {
		t.Fatalf("Probe(stale) = %+v, %v; want ErrNoServe", got, err)
	}
	if got != nil {
		t.Fatalf("Probe(stale) returned info %+v, want nil", got)
	}
	if _, statErr := os.Stat(lockPath); !os.IsNotExist(statErr) {
		t.Fatalf("stale lockfile was not removed: stat err = %v", statErr)
	}
}

func TestAcquireServeAndProbeLive(t *testing.T) {
	root := t.TempDir()
	info := ServeInfo{PID: os.Getpid(), Host: "127.0.0.1", Port: 7777, Root: root, Token: "sekrit"}

	lock, err := AcquireServe(root, info)
	if err != nil {
		t.Fatalf("AcquireServe: %v", err)
	}
	defer lock.Unlock()

	got, err := Probe(root)
	if err != nil {
		t.Fatalf("Probe(live): %v", err)
	}
	if got.Root != root || got.Port != 7777 || got.Token != "sekrit" {
		t.Fatalf("Probe(live) = %+v, want matching ServeInfo", got)
	}

	// A second AcquireServe attempt must fail while the first is held.
	if _, err := AcquireServe(root, info); !errors.Is(err, ErrLocked) {
		t.Fatalf("second AcquireServe = %v, want ErrLocked", err)
	}
}

func TestProbeRejectsWrongRoot(t *testing.T) {
	root := t.TempDir()
	other := t.TempDir()
	info := ServeInfo{PID: os.Getpid(), Host: "127.0.0.1", Port: 7777, Root: other}

	lock, err := AcquireServe(root, info)
	if err != nil {
		t.Fatalf("AcquireServe: %v", err)
	}
	defer lock.Unlock()

	if _, err := Probe(root); !errors.Is(err, ErrNoServe) {
		t.Fatalf("Probe with mismatched root = %v, want ErrNoServe", err)
	}
}

func TestBaseURL(t *testing.T) {
	cases := []struct {
		host string
		want string
	}{
		{"127.0.0.1", "http://127.0.0.1:7777"},
		{"0.0.0.0", "http://127.0.0.1:7777"},
		{"", "http://127.0.0.1:7777"},
		{"192.168.1.5", "http://192.168.1.5:7777"},
	}
	for _, c := range cases {
		info := ServeInfo{Host: c.host, Port: 7777}
		if got := info.BaseURL(); got != c.want {
			t.Errorf("BaseURL(host=%q) = %q, want %q", c.host, got, c.want)
		}
	}
}
