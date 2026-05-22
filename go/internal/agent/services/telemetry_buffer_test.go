package services

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"google.golang.org/protobuf/proto"

	otelpb "github.com/wendylabsinc/wendy/proto/gen/otelpb"
)

// writeFrameTo is a test helper that writes a single length-prefixed frame.
func writeFrameTo(t *testing.T, path string, msg proto.Message) {
	t.Helper()
	data, err := proto.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()
	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], uint32(len(data)))
	if _, err := f.Write(hdr[:]); err != nil {
		t.Fatalf("write header: %v", err)
	}
	if _, err := f.Write(data); err != nil {
		t.Fatalf("write data: %v", err)
	}
}

func TestSegmentFilename(t *testing.T) {
	got := segmentFilename(SignalLogs, 1)
	if got != "logs-000001.bin" {
		t.Errorf("want logs-000001.bin, got %s", got)
	}
	got = segmentFilename(SignalMetrics, 42)
	if got != "metrics-000042.bin" {
		t.Errorf("want metrics-000042.bin, got %s", got)
	}
}

func TestListSegments(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"logs-000002.bin", "logs-000001.bin", "metrics-000001.bin"} {
		os.WriteFile(filepath.Join(dir, name), []byte{}, 0644)
	}
	segs, err := listSegments(dir, SignalLogs)
	if err != nil {
		t.Fatal(err)
	}
	if len(segs) != 2 {
		t.Fatalf("want 2 log segments, got %d", len(segs))
	}
	if segs[0] != "logs-000001.bin" || segs[1] != "logs-000002.bin" {
		t.Errorf("wrong order: %v", segs)
	}
}

func TestReadFramesFrom_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "logs-000001.bin")

	entry := &otelpb.ExportLogsServiceRequest{
		ResourceLogs: []*otelpb.ResourceLogs{
			{ScopeLogs: []*otelpb.ScopeLogs{{
				LogRecords: []*otelpb.LogRecord{{
					Body: &otelpb.AnyValue{Value: &otelpb.AnyValue_StringValue{StringValue: "hello"}},
				}},
			}}},
		},
	}
	writeFrameTo(t, path, entry)
	writeFrameTo(t, path, entry)

	msgs, endOffset, err := readFramesFrom(path, 0, SignalLogs, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 {
		t.Fatalf("want 2 messages, got %d", len(msgs))
	}
	if endOffset == 0 {
		t.Error("endOffset should be > 0")
	}
}

func TestReadFramesFrom_PartialFrame(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "logs-000001.bin")

	entry := &otelpb.ExportLogsServiceRequest{}
	writeFrameTo(t, path, entry)

	// Append a partial header (only 2 bytes — incomplete frame).
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
	f.Write([]byte{0x00, 0x00})
	f.Close()

	msgs, _, err := readFramesFrom(path, 0, SignalLogs, 10)
	if err != nil {
		t.Fatal(err)
	}
	// Partial frame must be skipped; only the first complete entry returned.
	if len(msgs) != 1 {
		t.Errorf("want 1 message (partial frame skipped), got %d", len(msgs))
	}
}

func TestReadFramesFrom_MaxN(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "logs-000001.bin")
	entry := &otelpb.ExportLogsServiceRequest{}
	for i := 0; i < 5; i++ {
		writeFrameTo(t, path, entry)
	}
	msgs, _, err := readFramesFrom(path, 0, SignalLogs, 3)
	if err != nil {
		t.Fatalf("readFramesFrom: %v", err)
	}
	if len(msgs) != 3 {
		t.Errorf("want 3 messages (maxN respected), got %d", len(msgs))
	}
}
