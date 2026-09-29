package commands

import (
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

// fastStallChecks shrinks the watchdog's polling interval for the test.
func fastStallChecks(t *testing.T) {
	t.Helper()
	old := stallCheckInterval
	stallCheckInterval = 5 * time.Millisecond
	t.Cleanup(func() { stallCheckInterval = old })
}

func fixedLimit(d time.Duration) stallLimitFunc {
	return func(written, total int64) time.Duration { return d }
}

func TestWatchFlash_ReturnsWriteResult(t *testing.T) {
	fastStallChecks(t)
	var forwarded atomic.Int64
	write := func(progress func(int64)) error {
		for i := int64(1); i <= 4; i++ {
			progress(i * 100)
		}
		return nil
	}
	if err := watchFlash(write, 400, fixedLimit(time.Second), func(n int64) { forwarded.Store(n) }); err != nil {
		t.Fatalf("watchFlash: %v", err)
	}
	if got := forwarded.Load(); got != 400 {
		t.Fatalf("progress not forwarded: last = %d, want 400", got)
	}

	want := errors.New("boom")
	err := watchFlash(func(func(int64)) error { return want }, 400, fixedLimit(time.Second), nil)
	if !errors.Is(err, want) {
		t.Fatalf("watchFlash error = %v, want %v", err, want)
	}
}

// A writer that never reports progress and never returns is the macOS reflash
// hang: watchFlash must give up on its own instead of waiting on the writer.
func TestWatchFlash_StallsWithoutProgress(t *testing.T) {
	fastStallChecks(t)
	block := make(chan struct{})
	t.Cleanup(func() { close(block) })

	start := time.Now()
	err := watchFlash(func(func(int64)) error { <-block; return nil }, 400, fixedLimit(50*time.Millisecond), nil)
	if !errors.Is(err, errFlashStalled) {
		t.Fatalf("watchFlash error = %v, want errFlashStalled", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("stall took %s to report", elapsed)
	}
}

// Steady progress keeps the watchdog quiet even when the whole write takes
// longer than the stall limit.
func TestWatchFlash_ProgressResetsTheClock(t *testing.T) {
	fastStallChecks(t)
	write := func(progress func(int64)) error {
		for i := int64(1); i <= 10; i++ {
			time.Sleep(20 * time.Millisecond)
			progress(i)
		}
		return nil
	}
	if err := watchFlash(write, 10, fixedLimit(80*time.Millisecond), nil); err != nil {
		t.Fatalf("watchFlash: %v", err)
	}
}

// A zero limit disables the check at that point, e.g. while the final flush
// runs after the last byte was handed to the device.
func TestWatchFlash_ZeroLimitDisablesCheck(t *testing.T) {
	fastStallChecks(t)
	limit := func(written, total int64) time.Duration {
		if written >= total {
			return 0
		}
		return 30 * time.Millisecond
	}
	write := func(progress func(int64)) error {
		progress(10)
		time.Sleep(150 * time.Millisecond) // "flushing"
		return nil
	}
	if err := watchFlash(write, 10, limit, nil); err != nil {
		t.Fatalf("watchFlash: %v", err)
	}
}

func TestStalledFlashIsDeviceFailure(t *testing.T) {
	err := framePrimaryFlashError("seekable block-map write", 0, 100, fmt.Errorf("writing: %w", errFlashStalled))
	if !isDeviceFlashFailure(err) {
		t.Fatalf("a stalled write must skip the full-image fallback: %v", err)
	}
}

func TestFlashStallLimit(t *testing.T) {
	tests := []struct {
		name           string
		written, total int64
		want           time.Duration
	}{
		{"before the first byte", 0, 100, firstByteStallLimit},
		{"mid-write", 50, 100, 120 * time.Second},
		{"everything handed over", 100, 100, 0},
		{"unknown total, nothing written", 0, 0, firstByteStallLimit},
		{"unknown total, mid-write", 50, 0, 0},
	}
	for _, tt := range tests {
		if got := flashStallLimit(tt.written, tt.total); got != tt.want {
			t.Errorf("%s: flashStallLimit(%d, %d) = %s, want %s", tt.name, tt.written, tt.total, got, tt.want)
		}
	}
}
