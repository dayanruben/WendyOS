package services

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"
)

func testDialer() (*MeshDialer, *dialerProbes) {
	p := &dialerProbes{}
	d := NewMeshDialer(zap.NewNop(), "broker.example:443", 7, 100, "", "", "")
	d.lanLookup = func(ctx context.Context, assetID int32) (string, bool) {
		p.lookups++
		return p.lanAddr, p.lanAddr != ""
	}
	d.dialLAN = func(ctx context.Context, hostport string, port uint16) (net.Conn, error) {
		p.lanDials++
		if p.lanErr != nil {
			return nil, p.lanErr
		}
		a, _ := net.Pipe()
		return a, nil
	}
	d.dialBroker = func(ctx context.Context, deviceID int32, port uint16) (net.Conn, error) {
		p.brokerDials++
		if p.brokerErr != nil {
			return nil, p.brokerErr
		}
		a, _ := net.Pipe()
		return a, nil
	}
	return d, p
}

type dialerProbes struct {
	lanAddr                        string
	lanErr, brokerErr              error
	lookups, lanDials, brokerDials int
}

func TestDialDeviceUsesLANWhenAvailable(t *testing.T) {
	d, p := testDialer()
	p.lanAddr = "192.168.1.50:50052"
	conn, err := d.DialDevice(context.Background(), 215, 8080)
	if err != nil {
		t.Fatal(err)
	}
	conn.Close()
	if p.lanDials != 1 || p.brokerDials != 0 {
		t.Fatalf("lan=%d broker=%d, want 1/0", p.lanDials, p.brokerDials)
	}
}

func TestDialDeviceFallsBackToBroker(t *testing.T) {
	d, p := testDialer()
	p.lanAddr = "192.168.1.50:50052"
	p.lanErr = errors.New("connection refused")
	conn, err := d.DialDevice(context.Background(), 215, 8080)
	if err != nil {
		t.Fatal(err)
	}
	conn.Close()
	if p.lanDials != 1 || p.brokerDials != 1 {
		t.Fatalf("lan=%d broker=%d, want 1/1", p.lanDials, p.brokerDials)
	}
}

func TestDialDeviceBrokerOnlyWhenNoLANPeer(t *testing.T) {
	d, p := testDialer()
	p.lanAddr = "" // not found on LAN
	conn, err := d.DialDevice(context.Background(), 215, 8080)
	if err != nil {
		t.Fatal(err)
	}
	conn.Close()
	if p.lanDials != 0 || p.brokerDials != 1 {
		t.Fatalf("lan=%d broker=%d, want 0/1", p.lanDials, p.brokerDials)
	}
}

func TestDialDeviceCachesLANOutcome(t *testing.T) {
	d, p := testDialer()
	p.lanAddr = "192.168.1.50:50052"
	now := time.Now()
	d.now = func() time.Time { return now }

	for i := 0; i < 3; i++ {
		conn, err := d.DialDevice(context.Background(), 215, 8080)
		if err != nil {
			t.Fatal(err)
		}
		conn.Close()
	}
	if p.lookups != 1 {
		t.Fatalf("lookups = %d, want 1 (cached)", p.lookups)
	}

	now = now.Add(2 * lanCacheTTL) // expire
	conn, err := d.DialDevice(context.Background(), 215, 8080)
	if err != nil {
		t.Fatal(err)
	}
	conn.Close()
	if p.lookups != 2 {
		t.Fatalf("lookups after TTL = %d, want 2", p.lookups)
	}
}

func TestDialDeviceCachesNegativeOutcome(t *testing.T) {
	d, p := testDialer()
	p.lanAddr = ""
	for i := 0; i < 3; i++ {
		conn, err := d.DialDevice(context.Background(), 215, 8080)
		if err != nil {
			t.Fatal(err)
		}
		conn.Close()
	}
	if p.lookups != 1 {
		t.Fatalf("lookups = %d, want 1 (negative result cached)", p.lookups)
	}
}

// --- streamNetConn CloseWrite contract (Task 4 review requirement) ---

// fakeTunnelStream is a tunnelStream test double that records sent frames and
// lets the test control what recv() yields.
type fakeTunnelStream struct {
	mu         sync.Mutex
	sent       []fakeFrame
	closeSends int

	recvCh chan fakeFrame // test pushes frames for recv() to yield
	closed chan struct{}  // closed to make recv() return an error (EOF-like)
}

type fakeFrame struct {
	payload   []byte
	halfClose bool
}

func newFakeTunnelStream() *fakeTunnelStream {
	return &fakeTunnelStream{
		recvCh: make(chan fakeFrame, 16),
		closed: make(chan struct{}),
	}
}

func (f *fakeTunnelStream) send(p []byte, hc bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := append([]byte(nil), p...)
	f.sent = append(f.sent, fakeFrame{payload: cp, halfClose: hc})
	return nil
}

func (f *fakeTunnelStream) recv() ([]byte, bool, error) {
	select {
	case fr := <-f.recvCh:
		return fr.payload, fr.halfClose, nil
	case <-f.closed:
		return nil, false, io.EOF
	}
}

func (f *fakeTunnelStream) closeSend() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closeSends++
	return nil
}

func (f *fakeTunnelStream) sentFrames() []fakeFrame {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]fakeFrame, len(f.sent))
	copy(out, f.sent)
	return out
}

func TestStreamNetConnCloseWriteHalfClosesAndKeepsReading(t *testing.T) {
	stream := newFakeTunnelStream()
	var teardowns int
	conn := streamNetConn(stream, func() { teardowns++ })

	cw, ok := conn.(interface{ CloseWrite() error })
	if !ok {
		t.Fatal("streamNetConn result does not implement CloseWrite() error")
	}

	// (a) CloseWrite sends a half_close frame on the underlying stream.
	if err := cw.CloseWrite(); err != nil {
		t.Fatalf("CloseWrite: %v", err)
	}
	deadline := time.After(2 * time.Second)
	for {
		frames := stream.sentFrames()
		found := false
		for _, fr := range frames {
			if fr.halfClose {
				found = true
				break
			}
		}
		if found {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("no half_close frame sent within timeout; frames=%v", frames)
		case <-time.After(10 * time.Millisecond):
		}
	}

	// (b) data still flows stream -> conn after CloseWrite.
	stream.recvCh <- fakeFrame{payload: []byte("hello")}
	buf := make([]byte, 16)
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("Read after CloseWrite: %v", err)
	}
	if string(buf[:n]) != "hello" {
		t.Fatalf("Read = %q, want %q", buf[:n], "hello")
	}

	// (c) a subsequent Close tears everything down and runs teardown exactly once.
	close(stream.closed)
	if err := conn.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// Give the stream->pipe goroutine a moment to observe EOF and also call finish.
	time.Sleep(50 * time.Millisecond)
	if teardowns != 1 {
		t.Fatalf("teardowns = %d, want 1", teardowns)
	}
}
