//go:build darwin || linux

package shim

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/wendylabsinc/wendy/go/internal/cli/tegraflash/flasher"
)

func unlock(t *testing.T, f *os.File) {
	t.Helper()
	if err := unix.Flock(int(f.Fd()), unix.LOCK_UN); err != nil {
		t.Fatalf("unlock: %v", err)
	}
	f.Close()
}

// TestFlockPathSerializes verifies that a second flockPath (modeling a second
// shim process — flock treats each open file description independently) signals
// onWait, blocks while the first holds the lock, and acquires once released.
func TestFlockPathSerializes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usb.lock")

	first, err := flockPath(path, func() { t.Error("first flockPath should not wait") })
	if err != nil {
		t.Fatalf("first flockPath: %v", err)
	}

	waited := make(chan struct{})
	acquired := make(chan *os.File)
	errs := make(chan error, 1)
	go func() {
		f, err := flockPath(path, func() { close(waited) })
		if err != nil {
			errs <- err
			return
		}
		acquired <- f
	}()

	select {
	case <-waited:
	case err := <-errs:
		t.Fatalf("second flockPath: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("second flockPath never signaled it was waiting")
	}
	select {
	case <-acquired:
		t.Fatal("second flockPath acquired while the first still held the lock")
	case <-time.After(100 * time.Millisecond):
	}

	unlock(t, first)
	select {
	case f := <-acquired:
		unlock(t, f)
	case err := <-errs:
		t.Fatalf("second flockPath after release: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("second flockPath did not acquire after the first released")
	}
}

func TestFlockPathCreatesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does-not-exist-yet.lock")
	f, err := flockPath(path, nil)
	if err != nil {
		t.Fatalf("flockPath: %v", err)
	}
	defer unlock(t, f)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("lock file not created: %v", err)
	}
}

// TestAcquireUSBLockNoEnv verifies the env-less no-op contract: callers outside
// flasher.Run (no WENDY_ADB_LOCK) keep the old unserialized behavior.
func TestAcquireUSBLockNoEnv(t *testing.T) {
	t.Setenv(flasher.EnvADBLock, "")
	release := acquireUSBLock()
	if release == nil {
		t.Fatal("release func is nil")
	}
	release() // must be callable
}

// TestAcquireUSBLockRelease verifies release actually unlocks: a fresh flock on
// the same path succeeds immediately afterwards.
func TestAcquireUSBLockRelease(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usb.lock")
	t.Setenv(flasher.EnvADBLock, path)

	release := acquireUSBLock()
	release()

	f, err := flockPath(path, func() { t.Error("lock still held after release") })
	if err != nil {
		t.Fatalf("flockPath after release: %v", err)
	}
	unlock(t, f)
}
