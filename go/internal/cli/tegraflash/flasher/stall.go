package flasher

import "time"

// stallTimeout is how long Run tolerates neither push bytes nor flash-log
// output changing before it declares bootburn wedged and kills it. The whole
// flash takes ~15 minutes; the longest legitimate joint-silent window is one
// device-side op with the peer shim lock-blocked (a multi-GiB blkdiscard, md5
// verify, or the end-of-flash resize2fs — single-digit minutes on NVMe), so 10
// minutes leaves ~3x margin while still bounding a genuine deadlock (e.g.
// bootburn's pusher blocked forever on its writer queue after the writer died).
const stallTimeout = 10 * time.Minute

// stallDetector detects a wedged bootburn: neither the shim's push byte
// counter nor the flash log has changed for a full window. Both signals go
// quiet together only when no host-side flash work is happening at all.
type stallDetector struct {
	window   time.Duration
	last     time.Time // last time either signal changed
	prevPush int64
	prevLog  int64
}

func newStallDetector(window time.Duration, now time.Time) *stallDetector {
	return &stallDetector{window: window, last: now}
}

// observe feeds the current cumulative push byte count and flash-log size,
// returning true when neither has changed for the full window. Any change —
// including a counter reset — counts as progress.
func (s *stallDetector) observe(now time.Time, pushBytes, logBytes int64) bool {
	if pushBytes != s.prevPush || logBytes != s.prevLog {
		s.prevPush, s.prevLog = pushBytes, logBytes
		s.last = now
		return false
	}
	return now.Sub(s.last) >= s.window
}
