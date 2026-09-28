package flock

import (
	"errors"
	"os"
	"sync"
	"time"
)

// ErrTimeout reports that Acquire gave up waiting for another holder.
var ErrTimeout = errors.New("timed out waiting for file lock")

// acquirePoll is how often Acquire retries a contended lock.
const acquirePoll = 20 * time.Millisecond

// Acquire takes an exclusive lock on the file at path, creating it (0600) if
// needed, and waits up to timeout for another holder to let go. The returned
// release unlocks and closes the file; calling it more than once is harmless.
//
// The lock file is never deleted: unlinking it could let a second process lock
// a fresh inode while a waiter still holds the old one. Locks belong to the
// open file, so two Acquire calls in one process exclude each other too — and
// a holder that calls Acquire again on the same path waits on itself until
// timeout.
func Acquire(path string, timeout time.Duration) (release func(), err error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(timeout)
	for {
		locked, err := TryLock(f)
		if err != nil {
			_ = f.Close()
			return nil, err
		}
		if locked {
			return sync.OnceFunc(func() {
				_ = Unlock(f)
				_ = f.Close()
			}), nil
		}
		if !time.Now().Before(deadline) {
			_ = f.Close()
			return nil, ErrTimeout
		}
		time.Sleep(acquirePoll)
	}
}
