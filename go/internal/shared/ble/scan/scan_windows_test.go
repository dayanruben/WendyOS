//go:build windows

package scan

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
)

// errAfterReader plays back data, then returns err on every subsequent Read
// instead of the io.EOF a real closed pipe would produce — standing in for a
// stdout pipe that fails mid-stream.
type errAfterReader struct {
	data []byte
	err  error
}

func (r *errAfterReader) Read(p []byte) (int, error) {
	if len(r.data) == 0 {
		return 0, r.err
	}
	n := copy(p, r.data)
	r.data = r.data[n:]
	return n, nil
}

func (r *errAfterReader) Close() error { return nil }

// newTestScanner builds a windowsScanner bypassing newScanner/exec.Cmd, so
// readLoop can be exercised directly against a fake pipe. cancel records
// whether it was invoked.
func newTestScanner() (s *windowsScanner, canceled *bool) {
	canceled = new(bool)
	s = &windowsScanner{
		devices: make(map[string]BLEDeviceInfo),
		cancel:  func() { *canceled = true },
	}
	return s, canceled
}

func TestReadLoopRecordsRealErrorAndCancels(t *testing.T) {
	s, canceled := newTestScanner()
	sentinel := errors.New("pipe boom")
	r := &errAfterReader{
		data: []byte(`{"address":"AABBCCDDEEFF"}` + "\n"),
		err:  sentinel,
	}

	s.readLoop(r)

	if !*canceled {
		t.Error("expected cancel to be called after a real scanner error")
	}
	if s.readErr == nil || !errors.Is(s.readErr, sentinel) {
		t.Errorf("expected readErr to wrap the sentinel error, got %v", s.readErr)
	}
	// The line read before the error still landed.
	if len(s.devices) != 1 {
		t.Errorf("expected 1 device collected before the error, got %d", len(s.devices))
	}
}

func TestReadLoopRecordsTooLongLineAndCancels(t *testing.T) {
	s, canceled := newTestScanner()
	// One "line" with no newline, well past the 256 KiB buffer ceiling.
	big := bytes.Repeat([]byte{'a'}, 300*1024)
	r := &errAfterReader{data: big, err: errors.New("unreachable")}

	s.readLoop(r)

	if !*canceled {
		t.Error("expected cancel to be called on bufio.ErrTooLong")
	}
	if s.readErr == nil {
		t.Error("expected readErr to be set on bufio.ErrTooLong")
	}
}

func TestReadLoopCleanEOFRecordsNoError(t *testing.T) {
	s, canceled := newTestScanner()
	r := &errAfterReader{
		data: []byte(`{"address":"AABBCCDDEEFF"}` + "\n"),
		err:  io.EOF,
	}

	s.readLoop(r)

	if *canceled {
		t.Error("expected cancel not to be called on a clean EOF")
	}
	if s.readErr != nil {
		t.Errorf("expected no readErr on a clean EOF, got %v", s.readErr)
	}
	if len(s.devices) != 1 {
		t.Errorf("expected 1 device collected, got %d", len(s.devices))
	}
}

// TestSnapshotPrefersStaleDevicesOverError locks in the deliberate policy at
// Snapshot: once at least one device has been collected, a recorded readErr
// stays internal rather than ending the stream, so a watcher that dies after
// finding something keeps reporting it.
func TestSnapshotPrefersStaleDevicesOverError(t *testing.T) {
	s, _ := newTestScanner()
	s.devices["AA:BB:CC:DD:EE:FF"] = BLEDeviceInfo{Address: "AA:BB:CC:DD:EE:FF"}
	s.readErr = errors.New("watcher died")

	devices, err := s.Snapshot(context.Background())
	if err != nil {
		t.Errorf("expected no error while stale devices exist, got %v", err)
	}
	if len(devices) != 1 {
		t.Errorf("expected the stale device to still be reported, got %d devices", len(devices))
	}
}

func TestSnapshotSurfacesErrorWhenEmpty(t *testing.T) {
	s, _ := newTestScanner()
	sentinel := errors.New("watcher died before finding anything")
	s.readErr = sentinel

	devices, err := s.Snapshot(context.Background())
	if !errors.Is(err, sentinel) {
		t.Errorf("expected Snapshot to surface the error on an empty device set, got %v", err)
	}
	if devices != nil {
		t.Errorf("expected no devices alongside the error, got %v", devices)
	}
}
