package mesh

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/netip"
	"testing"
	"time"

	"go.uber.org/zap"
)

// halfClosablePipe wraps one end of a net.Pipe to give it a CloseWrite
// method, since net.Conn from net.Pipe does not implement half-close on its
// own. CloseWrite signals writeClosed so the peer side of the fake can
// distinguish "no more data is coming" from a live read.
type halfClosablePipe struct {
	net.Conn
	writeClosed chan struct{}
}

func newHalfClosablePipe(c net.Conn) *halfClosablePipe {
	return &halfClosablePipe{Conn: c, writeClosed: make(chan struct{})}
}

func (h *halfClosablePipe) CloseWrite() error {
	close(h.writeClosed)
	return nil
}

// fakeDialer records the dial and hands back one end of a pipe whose other
// end echoes with a prefix.
type fakeDialer struct {
	gotDevice int32
	gotPort   uint16
	err       error

	// plainPipe, when set, makes DialDevice return the bare net.Pipe conn
	// (no CloseWrite) instead of wrapping it in halfClosablePipe. The
	// fake's remote goroutine then has no half-close signal available and
	// can only observe completion via a full Close of its peer - exactly
	// the scenario relayBytes must handle without leaking.
	plainPipe bool

	// closed, when non-nil and plainPipe is set, receives the error
	// observed by the fake's second Read on its pipe end once (if ever)
	// the local peer conn is torn down.
	closed chan error
}

func (f *fakeDialer) DialDevice(_ context.Context, deviceID int32, port uint16) (net.Conn, error) {
	f.gotDevice, f.gotPort = deviceID, port
	if f.err != nil {
		return nil, f.err
	}
	a, b := net.Pipe()
	if f.plainPipe {
		go func() {
			buf := make([]byte, 64)
			_, _ = b.Read(buf) // drain the client's message; a has no CloseWrite.
			// A bare net.Pipe end has no half-close signal, so the only
			// way this goroutine can learn "the local side is done
			// sending" is a full Close of a, which unblocks this
			// pending Read with io.EOF. If relayBytes never closes a
			// (the bug under test), this blocks forever.
			_, err := b.Read(buf)
			if f.closed != nil {
				f.closed <- err
			}
		}()
		return a, nil
	}

	hcp := newHalfClosablePipe(a)
	go func() {
		buf := make([]byte, 64)
		n, _ := b.Read(buf)
		fmt.Fprintf(b, "peer:%s", buf[:n])
		// Wait for the relay to signal (via CloseWrite -> writeClosed)
		// that no more data is coming from the client before we close
		// our end, so the response above is guaranteed to have been
		// written first. This exercises the CloseWrite path
		// deterministically instead of racing on a sleep.
		<-hcp.writeClosed
		b.Close()
	}()
	return hcp, nil
}

func startTestProxy(t *testing.T, d PeerDialer, dst netip.AddrPort) net.Addr {
	t.Helper()
	p := NewProxy(zap.NewNop(), d)
	p.origDst = func(*net.TCPConn) (netip.AddrPort, error) { return dst, nil }
	if err := p.Start("127.0.0.1:0"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })
	return p.Addr()
}

func TestProxyRelaysToDialedPeer(t *testing.T) {
	d := &fakeDialer{}
	addr := startTestProxy(t, d, netip.MustParseAddrPort("10.99.0.215:8080"))

	conn, err := net.Dial("tcp", addr.String())
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}
	defer conn.Close()
	if _, err := conn.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	conn.(*net.TCPConn).CloseWrite()
	got, err := io.ReadAll(conn)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "peer:hello" {
		t.Fatalf("relayed %q, want %q", got, "peer:hello")
	}
	if d.gotDevice != 215 || d.gotPort != 8080 {
		t.Fatalf("dialed device %d port %d, want 215/8080", d.gotDevice, d.gotPort)
	}
}

// TestProxyFullClosesNonCloseWritePeer is the regression test for a leak:
// when the peer conn does not implement CloseWrite (e.g. a bare net.Pipe
// end, as the upcoming PeerDialer returns for tunnel conns), the relay must
// still fully close it once the opposite direction finishes, or the far end
// of the pipe blocks on read forever and the relay goroutines/FDs never
// exit.
func TestProxyFullClosesNonCloseWritePeer(t *testing.T) {
	d := &fakeDialer{plainPipe: true, closed: make(chan error, 1)}
	addr := startTestProxy(t, d, netip.MustParseAddrPort("10.99.0.215:8080"))

	conn, err := net.Dial("tcp", addr.String())
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}
	defer conn.Close()
	if _, err := conn.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	conn.(*net.TCPConn).CloseWrite()

	select {
	case err := <-d.closed:
		if err != io.EOF && err != io.ErrClosedPipe {
			t.Fatalf("peer far end observed %v on close, want io.EOF or io.ErrClosedPipe", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("relay leaked the peer conn: far end never observed a close for a non-CloseWrite peer")
	}
}

func TestProxyClosesOnNonMeshVIP(t *testing.T) {
	d := &fakeDialer{}
	addr := startTestProxy(t, d, netip.MustParseAddrPort("192.168.1.1:80"))
	conn, err := net.Dial("tcp", addr.String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 1)
	if _, err := conn.Read(buf); err != io.EOF {
		t.Fatalf("expected EOF for non-mesh destination, got %v", err)
	}
	if d.gotDevice != 0 {
		t.Fatal("dialer must not be called for a non-mesh destination")
	}
}
