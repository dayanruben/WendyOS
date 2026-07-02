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

// fakeDialer records the dial and hands back one end of a pipe whose other
// end echoes with a prefix.
type fakeDialer struct {
	gotDevice int32
	gotPort   uint16
	err       error
}

func (f *fakeDialer) DialDevice(_ context.Context, deviceID int32, port uint16) (net.Conn, error) {
	f.gotDevice, f.gotPort = deviceID, port
	if f.err != nil {
		return nil, f.err
	}
	a, b := net.Pipe()
	go func() {
		buf := make([]byte, 64)
		n, _ := b.Read(buf)
		fmt.Fprintf(b, "peer:%s", buf[:n])
		b.Close()
	}()
	return a, nil
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
