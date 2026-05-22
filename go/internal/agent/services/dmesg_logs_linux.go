//go:build linux

package services

import (
	"bufio"
	"context"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"

	otelpb "github.com/wendylabsinc/wendy/proto/gen/otelpb"
)

// CollectDmesgLogs reads kernel messages from /dev/kmsg and publishes them as
// OTel log records at debug/trace severity. It replays buffered kernel messages
// first, then follows new ones. Blocks until ctx is cancelled.
func CollectDmesgLogs(ctx context.Context, broadcaster *TelemetryBroadcaster) {
	f, err := os.Open("/dev/kmsg")
	if err != nil {
		return
	}
	// Close the file when ctx is done to unblock the blocking read.
	go func() {
		<-ctx.Done()
		f.Close()
	}()
	defer f.Close()

	resource := dmesgResource()
	bootEpoch := kmsgBootEpochNanoseconds()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if len(line) == 0 || line[0] == ' ' {
			// Continuation lines carry key=value device metadata — skip them.
			continue
		}

		level, message, tsUS, ok := parseKmsgLine(line)
		if !ok {
			continue
		}

		var timeNano uint64
		if bootEpoch > 0 && tsUS > 0 {
			timeNano = uint64(bootEpoch + tsUS*1000)
		} else {
			timeNano = uint64(time.Now().UnixNano())
		}

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

// kmsgBootEpochNanoseconds returns the Unix nanosecond timestamp of the kernel
// boot instant, computed from CLOCK_BOOTTIME. Returns 0 on failure.
func kmsgBootEpochNanoseconds() int64 {
	var ts unix.Timespec
	if err := unix.ClockGettime(unix.CLOCK_BOOTTIME, &ts); err != nil {
		return 0
	}
	bootNowNs := ts.Sec*int64(time.Second) + ts.Nsec
	return time.Now().UnixNano() - bootNowNs
}

// parseKmsgLine parses a /dev/kmsg record of the form:
//
//	priority,sequence,timestamp_us,-;message
//
// Returns the syslog level (0–7), message text, timestamp in microseconds
// since boot, and whether parsing succeeded.
func parseKmsgLine(line string) (level int, message string, timestampUS int64, ok bool) {
	semi := strings.IndexByte(line, ';')
	if semi < 0 {
		return 0, "", 0, false
	}
	message = line[semi+1:]

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
// within the debug/trace band. Kernel debug messages map to trace; higher
// kernel severity maps upward within the debug sub-levels so that relative
// ordering is preserved while keeping all dmesg output below INFO.
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
