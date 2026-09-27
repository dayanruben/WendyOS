package chunkupload

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

// ErrStalled is the cancellation cause Watch sets when WriteChunks streams are
// open but none has made progress for the stall timeout. Uncompressed chunk
// payloads wedged a USB-NCM link on WendyOS 0.18.2 (#1765); a caller that
// sees this cause reconnects and retries with gzip.
var ErrStalled = errors.New("chunk upload stalled: no stream made progress")

// Activity records WriteChunks progress across every layer of one push, so a
// single watchdog can tell a slow link (some stream still moves) from a wedged
// one (streams are open and nothing moves). The zero value is ready to use,
// and a nil *Activity records nothing.
type Activity struct {
	open atomic.Int64 // WriteChunks streams currently open
	last atomic.Int64 // UnixNano of the latest stream open, Send or CloseAndRecv
}

// opened records a new stream. The timestamp is stored before the count, so
// the watchdog never sees an open stream with a stale timestamp.
func (a *Activity) opened() {
	if a == nil {
		return
	}
	a.last.Store(time.Now().UnixNano())
	a.open.Add(1)
}

func (a *Activity) closed() {
	if a == nil {
		return
	}
	a.open.Add(-1)
}

func (a *Activity) progressed() {
	if a == nil {
		return
	}
	a.last.Store(time.Now().UnixNano())
}

// stalled reports whether a stream is open and nothing has progressed for
// timeout as of now.
func (a *Activity) stalled(now time.Time, timeout time.Duration) bool {
	return a.open.Load() > 0 && now.Sub(time.Unix(0, a.last.Load())) >= timeout
}

// Watch calls cancel(ErrStalled) once a has been stalled for timeout, checking
// every timeout/10. It stops after cancelling, or when stop is called; stop is
// safe to call more than once.
func Watch(a *Activity, timeout time.Duration, cancel context.CancelCauseFunc) (stop func()) {
	done := make(chan struct{})
	go func() {
		tick := time.NewTicker(max(timeout/10, time.Millisecond))
		defer tick.Stop()
		for {
			select {
			case <-done:
				return
			case now := <-tick.C:
				if a.stalled(now, timeout) {
					cancel(ErrStalled)
					return
				}
			}
		}
	}()
	var once sync.Once
	return func() { once.Do(func() { close(done) }) }
}
