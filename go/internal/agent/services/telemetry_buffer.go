package services

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"google.golang.org/protobuf/proto"

	otelpb "github.com/wendylabsinc/wendy/proto/gen/otelpb"
)

// SignalType identifies a telemetry signal kind.
type SignalType string

const (
	SignalLogs    SignalType = "logs"
	SignalMetrics SignalType = "metrics"
	SignalTraces  SignalType = "traces"
)

const maxSegmentFrameBytes = 10 * 1024 * 1024

// segmentFilename returns the filename for a segment file, e.g. "logs-000001.bin".
func segmentFilename(signal SignalType, seqNum int) string {
	return fmt.Sprintf("%s-%06d.bin", signal, seqNum)
}

// listSegments returns segment filenames (basename only) for signal, sorted
// ascending by sequence number. It does not prepend the directory.
func listSegments(dir string, signal SignalType) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	prefix := string(signal) + "-"
	var names []string
	for _, e := range entries {
		n := e.Name()
		if !e.IsDir() && strings.HasPrefix(n, prefix) && strings.HasSuffix(n, ".bin") {
			names = append(names, n)
		}
	}
	sort.Strings(names) // zero-padded seq → lexicographic = numeric order
	return names, nil
}

// newProtoForSignal allocates a fresh proto.Message of the correct type for signal.
func newProtoForSignal(signal SignalType) proto.Message {
	switch signal {
	case SignalLogs:
		return &otelpb.ExportLogsServiceRequest{}
	case SignalMetrics:
		return &otelpb.ExportMetricsServiceRequest{}
	case SignalTraces:
		return &otelpb.ExportTraceServiceRequest{}
	default:
		return nil
	}
}

// readFramesFrom reads up to maxN length-prefixed protobuf frames from path
// starting at startOffset. Returns the decoded messages, the byte offset after
// the last successfully read frame, and any I/O error (os.ErrNotExist included).
// A partial frame or unmarshal error stops reading cleanly; no partial data is returned.
func readFramesFrom(path string, startOffset int64, signal SignalType, maxN int) ([]proto.Message, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, startOffset, err
	}
	defer f.Close()

	if startOffset > 0 {
		if _, err := f.Seek(startOffset, io.SeekStart); err != nil {
			return nil, startOffset, err
		}
	}

	offset := startOffset
	var msgs []proto.Message

	for len(msgs) < maxN {
		// Allocate the destination proto first so we catch unknown signals before I/O.
		msg := newProtoForSignal(signal)
		if msg == nil {
			break
		}
		var hdr [4]byte
		if _, err := io.ReadFull(f, hdr[:]); err != nil {
			break
		}
		length := binary.BigEndian.Uint32(hdr[:])
		if length > maxSegmentFrameBytes {
			break
		}
		data := make([]byte, length)
		if _, err := io.ReadFull(f, data); err != nil {
			break
		}
		if err := proto.Unmarshal(data, msg); err != nil {
			break // corrupted frame — stop reading this file cleanly
		}
		msgs = append(msgs, msg)
		offset += int64(4 + length)
	}

	return msgs, offset, nil
}
