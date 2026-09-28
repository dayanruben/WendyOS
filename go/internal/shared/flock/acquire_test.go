package flock

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestAcquireTimesOutWhileAnotherHolderHasTheLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.lock")
	release, err := Acquire(path, time.Second)
	if err != nil {
		t.Fatalf("first Acquire: %v", err)
	}
	start := time.Now()
	if _, err := Acquire(path, 150*time.Millisecond); !errors.Is(err, ErrTimeout) {
		t.Fatalf("contended Acquire = %v, want ErrTimeout", err)
	}
	if waited := time.Since(start); waited < 150*time.Millisecond {
		t.Fatalf("contended Acquire gave up after %s, before its 150ms timeout", waited)
	}
	release()
	again, err := Acquire(path, time.Second)
	if err != nil {
		t.Fatalf("Acquire after release: %v", err)
	}
	again()
}

func TestAcquireWaitsForARelease(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.lock")
	release, err := Acquire(path, time.Second)
	if err != nil {
		t.Fatalf("first Acquire: %v", err)
	}
	go func() {
		time.Sleep(100 * time.Millisecond)
		release()
	}()
	next, err := Acquire(path, 5*time.Second)
	if err != nil {
		t.Fatalf("Acquire did not pick up a lock released while it waited: %v", err)
	}
	next()
}

func TestAcquireCreatesAPrivateLockFileAndReleaseIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.lock")
	release, err := Acquire(path, time.Second)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat lock file: %v", err)
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Errorf("lock file mode = %v, want 0600", got)
		}
	}
	release()
	release() // a second release must be harmless
}
