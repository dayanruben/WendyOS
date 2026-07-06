//go:build darwin || linux

package shim

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/unix"

	"github.com/wendylabsinc/wendy/go/internal/cli/tegraflash/flasher"
)

// usbLockPath returns the flock file that serializes USB access across shim
// processes: the one the parent flasher names in EnvADBLock, or a well-known
// per-boot fallback. The fallback matters because setupADBShim permanently
// links the cached flashpack bundle's adb to wendy, so shims can be spawned
// outside flasher.Run's environment (e.g. re-running NVIDIA's scripts against
// the cached bundle by hand) — those must serialize too, or the libusb claim
// race returns.
func usbLockPath() string {
	if p := os.Getenv(flasher.EnvADBLock); p != "" {
		return p
	}
	return filepath.Join(os.TempDir(), "wendy-adb-usb.lock")
}

// openLockFile opens (creating if needed) the USB lock file.
func openLockFile() (*os.File, error) {
	return os.OpenFile(usbLockPath(), os.O_RDWR|os.O_CREATE, 0o644)
}

// flockFile applies the flock operation how to f, retrying EINTR: a signal
// (e.g. the Go runtime's async preemption) can interrupt a blocked flock, and
// giving up on that would abort the flash for nothing.
func flockFile(f *os.File, how int) error {
	for {
		err := unix.Flock(int(f.Fd()), how)
		if !errors.Is(err, unix.EINTR) {
			return err
		}
	}
}

// releaseFunc returns the release closure for a held lock file.
func releaseFunc(f *os.File) func() {
	return func() {
		_ = unix.Flock(int(f.Fd()), unix.LOCK_UN)
		_ = f.Close()
	}
}

// usbLockFatal reports a lock failure and exits the shim: proceeding without
// the lock would silently reintroduce the USB claim race.
func usbLockFatal(err error) {
	fmt.Fprintf(os.Stderr, "wendy adb: USB lock %s: %v\n", usbLockPath(), err)
	os.Exit(1)
}

// acquireUSBLock serializes this shim's USB claim + I/O against concurrent
// shim processes: bootburn runs its chunk pusher and partition writer
// concurrently, and unserialized claims of the same interface can SIGSEGV
// inside libusb on macOS. It blocks until the lock is free — the holder is
// another shim whose USB transfers are bounded by the adb package's ioTimeout,
// and the parent flasher's stall watchdog bounds total silence. Returns a
// release func.
func acquireUSBLock() (release func()) {
	f, err := openLockFile()
	if err != nil {
		usbLockFatal(err)
	}
	// Contention is constant by design (every op waits on its peer), so only
	// surface waits long enough to matter in the flash log.
	note := time.AfterFunc(3*time.Second, func() {
		fmt.Fprintln(os.Stderr, "wendy adb: waiting for USB device lock...")
	})
	err = flockFile(f, unix.LOCK_EX)
	note.Stop()
	if err != nil {
		f.Close()
		usbLockFatal(err)
	}
	return releaseFunc(f)
}

// tryAcquireUSBLock is the non-blocking variant for wait-for-device: it never
// waits on a peer. When the lock is free it acquires it and returns a release
// func; when a sibling shim holds it, it reports busy instead.
func tryAcquireUSBLock() (release func(), busy bool) {
	f, err := openLockFile()
	if err != nil {
		usbLockFatal(err)
	}
	switch err := flockFile(f, unix.LOCK_EX|unix.LOCK_NB); {
	case err == nil:
		return releaseFunc(f), false
	case errors.Is(err, unix.EWOULDBLOCK):
		f.Close()
		return nil, true
	default:
		f.Close()
		usbLockFatal(err)
		panic("unreachable")
	}
}
