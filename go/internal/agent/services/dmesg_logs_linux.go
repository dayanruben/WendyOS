//go:build linux

package services

import (
	"bufio"
	"context"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
	"golang.org/x/sys/unix"

	otelpb "github.com/wendylabsinc/wendy/proto/gen/otelpb"
)

const (
	// dmesgMaxMsgsPerSec caps non-critical messages forwarded per second.
	// KERN_EMERG/ALERT/CRIT messages are always forwarded regardless of this limit.
	dmesgMaxMsgsPerSec = 500

	// dmesgMaxMessageLen caps the byte length of a single kernel message body.
	dmesgMaxMessageLen = 4096

	// minValidTimestampNs rejects computed timestamps earlier than year 2000.
	minValidTimestampNs = 946684800 * int64(time.Second)

	// maxFutureSkewNs rejects timestamps more than 24 h in the future.
	maxFutureSkewNs = int64(24 * time.Hour)
)

// piiPatterns matches common PII patterns in kernel messages (MAC addresses,
// IPv4/IPv6 addresses) for optional redaction when WENDY_DMESG_REDACT=true.
var piiPatterns = regexp.MustCompile(
	`(?i)(?:` +
		// MAC address (colon or hyphen separated)
		`[0-9a-f]{2}(?::[0-9a-f]{2}){5}` +
		`|[0-9a-f]{2}(?:-[0-9a-f]{2}){5}` +
		// IPv4
		`|\b\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}\b` +
		// IPv6 (simplified: groups of hex separated by colons, at least 2 colons)
		`|[0-9a-f]{1,4}(?::[0-9a-f]{0,4}){2,7}` +
		`)`,
)

// CollectDmesgLogs reads kernel messages from /dev/kmsg and publishes them as
// OTel log records at debug/trace severity. It replays buffered kernel messages
// first, then follows new ones. Blocks until ctx is cancelled.
//
// Set WENDY_DMESG_REDACT=true to enable best-effort redaction of MAC addresses
// and IP addresses before forwarding. Note that kernel messages may also contain
// USB serial numbers, process names, PIDs, and filesystem paths that are not
// redacted; operators should review their data-minimisation requirements.
//
// NOTE: All kernel severity levels are intentionally mapped into the OTel
// debug/trace band. KERN_EMERG/ALERT/CRIT additionally emit a zap.Warn so
// they are visible in the agent's own log stream. See kernelLevelToOTEL.
func CollectDmesgLogs(ctx context.Context, logger *zap.Logger, broadcaster *TelemetryBroadcaster) {
	redact, _ := strconv.ParseBool(os.Getenv("WENDY_DMESG_REDACT"))

	f, err := os.Open("/dev/kmsg")
	if err != nil {
		logger.Warn("dmesg collection unavailable", zap.Error(err))
		return
	}

	// Verify /dev/kmsg is actually a character device to guard against a
	// container bind-mount replacing it with a regular file or FIFO.
	info, statErr := f.Stat()
	if statErr != nil || info.Mode()&os.ModeCharDevice == 0 {
		logger.Warn("dmesg: /dev/kmsg is not a character device; skipping collection",
			zap.String("mode", func() string {
				if statErr != nil {
					return statErr.Error()
				}
				return info.Mode().String()
			}()))
		_ = f.Close()
		return
	}

	logger.Info("kernel dmesg collection started",
		zap.String("source", "/dev/kmsg"),
		zap.Bool("redact", redact),
	)
	defer logger.Info("kernel dmesg collection stopped")

	// sync.Once ensures only one close fires even though both the ctx-cancel
	// goroutine and the defer call closeFile().
	var closeOnce sync.Once
	closeFile := func() { closeOnce.Do(func() { _ = f.Close() }) }
	go func() {
		<-ctx.Done()
		closeFile()
	}()
	defer closeFile()

	resource := dmesgResource()
	bootEpoch := kmsgBootEpochNanoseconds()

	// Sliding-window rate limiter for non-critical messages only.
	// KERN_EMERG (0), KERN_ALERT (1), KERN_CRIT (2) bypass this entirely.
	var (
		windowStart = time.Now()
		windowCount int
		windowDrop  int
	)
	rateAllow := func() bool {
		now := time.Now()
		if now.Sub(windowStart) >= time.Second {
			if windowDrop > 0 {
				logger.Warn("dmesg rate limit: messages dropped in last second",
					zap.Int("dropped", windowDrop),
					zap.Int("forwarded", windowCount),
				)
			}
			windowStart = now
			windowCount = 0
			windowDrop = 0
		}
		if windowCount >= dmesgMaxMsgsPerSec {
			windowDrop++
			return false
		}
		windowCount++
		return true
	}

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if len(line) == 0 || line[0] == ' ' {
			continue
		}

		level, message, tsUS, ok := parseKmsgLine(line)
		if !ok {
			continue
		}
		if len(message) > dmesgMaxMessageLen {
			message = message[:dmesgMaxMessageLen]
		}
		if redact {
			message = piiPatterns.ReplaceAllString(message, "<redacted>")
		}

		isCritical := level <= 2 // KERN_EMERG, KERN_ALERT, KERN_CRIT

		// Critical messages bypass the rate limiter so they are never silently
		// dropped. The zap.Warn fires after the rate check so the agent log
		// stays visible at the default INFO+ level.
		if !isCritical && !rateAllow() {
			continue
		}
		if isCritical {
			logger.Warn("critical kernel message",
				zap.Int("kernel_level", level),
				zap.String("message", message),
			)
		}

		timeNano := kmsgTimestampToWall(tsUS, bootEpoch)
		severity, severityText := kernelLevelToOTEL(level)
		broadcaster.PublishLogs(&otelpb.ExportLogsServiceRequest{
			ResourceLogs: []*otelpb.ResourceLogs{
				{
					Resource: resource,
					ScopeLogs: []*otelpb.ScopeLogs{
						{
							Scope: &otelpb.InstrumentationScope{Name: "wendy.dmesg"},
							LogRecords: []*otelpb.LogRecord{
								{
									TimeUnixNano:         timeNano,
									ObservedTimeUnixNano: uint64(time.Now().UnixNano()),
									SeverityNumber:       severity,
									SeverityText:         severityText,
									Body: &otelpb.AnyValue{
										Value: &otelpb.AnyValue_StringValue{StringValue: message},
									},
								},
							},
						},
					},
				},
			},
		})
	}
}

// dmesgResource returns the OTel resource for kernel log records.
func dmesgResource() *otelpb.Resource {
	attrs := []*otelpb.KeyValue{
		stringKV("service.name", "kernel"),
		stringKV("service.namespace", "wendy"),
	}
	if h := resolveHostname(); h != "" {
		attrs = append(attrs, stringKV("service.instance.id", h))
	}
	return &otelpb.Resource{Attributes: attrs}
}

// kmsgBootEpochNanoseconds returns the wall-clock Unix nanosecond timestamp
// corresponding to the kernel boot instant, computed from CLOCK_BOOTTIME.
// Returns 0 if unavailable.
func kmsgBootEpochNanoseconds() int64 {
	var ts unix.Timespec
	if err := unix.ClockGettime(unix.CLOCK_BOOTTIME, &ts); err != nil {
		return 0
	}
	bootNowNs := ts.Sec*int64(time.Second) + ts.Nsec
	return time.Now().UnixNano() - bootNowNs
}

// kmsgTimestampToWall converts a kernel timestamp (microseconds since boot) to
// a wall-clock Unix nanosecond value. Falls back to time.Now() if outside a
// plausible range to guard against NTP steps or boot-epoch skew.
func kmsgTimestampToWall(tsUS int64, bootEpoch int64) uint64 {
	now := time.Now().UnixNano()
	if bootEpoch > 0 && tsUS > 0 {
		computed := bootEpoch + tsUS*1000
		if computed >= minValidTimestampNs && computed <= now+maxFutureSkewNs {
			return uint64(computed)
		}
	}
	return uint64(now)
}

// parseKmsgLine parses a /dev/kmsg record of the form:
//
//	priority,sequence,timestamp_us,-;message
//
// Returns the syslog level (0–7), sanitised message text, timestamp in
// microseconds since boot, and whether parsing succeeded. ASCII and Unicode
// control characters (except tab) are stripped to prevent log injection.
func parseKmsgLine(line string) (level int, message string, timestampUS int64, ok bool) {
	semi := strings.IndexByte(line, ';')
	if semi < 0 {
		return 0, "", 0, false
	}

	// Strip ASCII control chars and Unicode format/control characters.
	message = strings.Map(func(r rune) rune {
		if r == '\t' {
			return r
		}
		// Drop ASCII control chars and Unicode Cc/Cf categories.
		if r < 0x20 || (r >= 0x7f && r <= 0x9f) || r == 0x200b || r == 0xfeff {
			return -1
		}
		return r
	}, line[semi+1:])

	parts := strings.SplitN(line[:semi], ",", 4)
	if len(parts) < 3 {
		return 0, "", 0, false
	}

	priority, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, "", 0, false
	}

	ts, _ := strconv.ParseInt(parts[2], 10, 64)
	return priority & 7, message, ts, true
}

// kernelLevelToOTEL maps a kernel syslog level (0–7) to an OTel severity
// within the debug/trace band, preserving relative ordering while keeping all
// dmesg output below INFO.
//
// KERN_EMERG (0), KERN_ALERT (1), and KERN_CRIT (2) are capped at DEBUG4 by
// design — these events are for diagnostic purposes and should not surface in
// default INFO+ alert rules. The scan loop in CollectDmesgLogs additionally
// emits a zap.Warn for these levels so they appear in the agent log stream,
// and they are exempt from rate limiting so they are never silently dropped.
func kernelLevelToOTEL(level int) (otelpb.SeverityNumber, string) {
	switch level {
	case 7: // KERN_DEBUG
		return otelpb.SeverityNumber_SEVERITY_NUMBER_TRACE, "TRACE"
	case 6: // KERN_INFO
		return otelpb.SeverityNumber_SEVERITY_NUMBER_TRACE4, "TRACE"
	case 5: // KERN_NOTICE
		return otelpb.SeverityNumber_SEVERITY_NUMBER_DEBUG, "DEBUG"
	case 4: // KERN_WARNING
		return otelpb.SeverityNumber_SEVERITY_NUMBER_DEBUG2, "DEBUG"
	case 3: // KERN_ERR
		return otelpb.SeverityNumber_SEVERITY_NUMBER_DEBUG3, "DEBUG"
	default: // KERN_CRIT (2), KERN_ALERT (1), KERN_EMERG (0)
		return otelpb.SeverityNumber_SEVERITY_NUMBER_DEBUG4, "DEBUG"
	}
}
