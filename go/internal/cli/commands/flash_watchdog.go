package commands

import (
	"errors"
	"sync"
	"time"
)

// errFlashStalled marks a write that stopped making progress. It counts as a
// device failure: retrying the full image would block on the same device.
var errFlashStalled = errors.New("the write stopped making progress")

// flashStallHint is appended when a write stalls or the disk cannot be
// unmounted in time.
const flashStallHint = "The disk stopped responding. Unplug the card, plug it back in, and run the install again."

// stallCheckInterval is how often watchFlash looks at the progress clock.
var stallCheckInterval = time.Second

// stallLimitFunc reports how long a write may go without progress once it has
// written `written` of `total` bytes. Zero disables the check at that point.
type stallLimitFunc func(written, total int64) time.Duration

// flashStallLimit is the stall policy for `wendy os install`. Once every byte
// is handed over the check stops: a buffered device's final flush can run for
// minutes without progress. With no known total that point is invisible, so
// only a write that never starts counts. Mid-write allows for SD cards
// pausing to erase.
func flashStallLimit(written, total int64) time.Duration {
	switch {
	case written == 0:
		return firstByteStallLimit
	case total <= 0 || written >= total:
		return 0
	default:
		return 120 * time.Second
	}
}

// watchFlash runs write, forwarding its progress to progressFn, and returns
// errFlashStalled once progress has been silent longer than limit allows. It
// does not wait for a stalled write: a writer blocked in kernel I/O cannot be
// interrupted, so waiting for it would hang the CLI all over again.
func watchFlash(write func(progress func(int64)) error, total int64, limit stallLimitFunc, progressFn func(int64)) error {
	var (
		mu      sync.Mutex
		written int64
		last    = time.Now()
	)
	progress := func(n int64) {
		mu.Lock()
		written, last = n, time.Now()
		mu.Unlock()
		if progressFn != nil {
			progressFn(n)
		}
	}

	done := make(chan error, 1)
	go func() { done <- write(progress) }()

	ticker := time.NewTicker(stallCheckInterval)
	defer ticker.Stop()
	for {
		select {
		case err := <-done:
			return err
		case <-ticker.C:
			mu.Lock()
			n, idle := written, time.Since(last)
			mu.Unlock()
			if max := limit(n, total); max > 0 && idle > max {
				return errFlashStalled
			}
		}
	}
}
