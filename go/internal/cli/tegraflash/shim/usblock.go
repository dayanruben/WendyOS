//go:build darwin || linux

package shim

import (
	"errors"
	"fmt"
	"os"
	"time"

	"golang.org/x/sys/unix"

	"github.com/wendylabsinc/wendy/go/internal/cli/tegraflash/flasher"
)

// flockPath opens (creating if needed) path and takes an exclusive advisory
// flock on it, blocking until the lock is free. onWait is called once if the
// lock was not immediately available. The wait is unbounded by design: the
// holder is another shim whose USB op is bounded by the adb package's
// ioTimeout, and the parent flasher's stall watchdog bounds total silence.
func flockPath(path string, onWait func()) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return nil, err
	}
	err = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
	if err == nil {
		return f, nil
	}
	if !errors.Is(err, unix.EWOULDBLOCK) {
		f.Close()
		return nil, err
	}
	if onWait != nil {
		onWait()
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

// acquireUSBLock serializes this shim's USB claim + I/O against concurrent shim
// processes via the flock file the parent flasher names in EnvADBLock: bootburn
// runs its chunk pusher and partition writer concurrently, and unserialized
// claims of the same interface can SIGSEGV inside libusb on macOS. Returns a
// release func. When the env var is unset (a caller other than flasher.Run) it
// is a no-op, preserving the old unserialized behavior.
func acquireUSBLock() (release func()) {
	path := os.Getenv(flasher.EnvADBLock)
	if path == "" {
		return func() {}
	}
	var note *time.Timer
	f, err := flockPath(path, func() {
		// Contention is constant by design (every op waits on its peer), so
		// only surface waits long enough to matter in the flash log.
		note = time.AfterFunc(3*time.Second, func() {
			fmt.Fprintln(os.Stderr, "wendy adb: waiting for USB device lock...")
		})
	})
	if note != nil {
		note.Stop()
	}
	if err != nil {
		// The env var is set but the lock is unusable: proceeding would
		// silently reintroduce the claim race, so fail like any shim error.
		fmt.Fprintf(os.Stderr, "wendy adb: USB lock %s: %v\n", path, err)
		os.Exit(1)
	}
	return func() {
		_ = unix.Flock(int(f.Fd()), unix.LOCK_UN)
		_ = f.Close()
	}
}
