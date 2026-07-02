package mesh

import (
	"context"
	"io"
	"net"
	"net/netip"
	"time"

	"go.uber.org/zap"
)

// ProxyPort is the host TCP port the WENDY-MESH nat REDIRECT rule points at.
// 50051 is the agent's plaintext port and 50052 its mTLS port; 50058 is
// otherwise unused by the agent.
const ProxyPort = 50058

// PeerDialer opens a byte stream to a port on another mesh device. The
// context bounds dialing only; the returned conn outlives it.
type PeerDialer interface {
	DialDevice(ctx context.Context, deviceID int32, port uint16) (net.Conn, error)
}

// Proxy terminates REDIRECTed mesh VIP connections, recovers where the
// container was actually connecting, and splices the connection onto a
// PeerDialer stream toward that device.
type Proxy struct {
	logger      *zap.Logger
	dialer      PeerDialer
	origDst     func(*net.TCPConn) (netip.AddrPort, error) // swapped in tests
	dialTimeout time.Duration
	ln          net.Listener
}

func NewProxy(logger *zap.Logger, dialer PeerDialer) *Proxy {
	return &Proxy{
		logger:      logger,
		dialer:      dialer,
		origDst:     originalDst,
		dialTimeout: 15 * time.Second,
	}
}

func (p *Proxy) Start(addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	p.ln = ln
	go p.acceptLoop()
	return nil
}

func (p *Proxy) Addr() net.Addr { return p.ln.Addr() }

func (p *Proxy) Close() error {
	if p.ln != nil {
		return p.ln.Close()
	}
	return nil
}

func (p *Proxy) acceptLoop() {
	for {
		conn, err := p.ln.Accept()
		if err != nil {
			return
		}
		go p.handleConn(conn)
	}
}

func (p *Proxy) handleConn(conn net.Conn) {
	defer conn.Close()
	tcp, ok := conn.(*net.TCPConn)
	if !ok {
		return
	}
	dst, err := p.origDst(tcp)
	if err != nil {
		p.logger.Warn("mesh proxy: original destination unavailable", zap.Error(err))
		return
	}
	deviceID, err := DeviceForVIP(dst.Addr())
	if err != nil {
		p.logger.Warn("mesh proxy: destination is not a mesh VIP", zap.String("dst", dst.String()), zap.Error(err))
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), p.dialTimeout)
	peer, err := p.dialer.DialDevice(ctx, deviceID, dst.Port())
	cancel()
	if err != nil {
		p.logger.Warn("mesh proxy: dialing device failed",
			zap.Int32("device_id", deviceID), zap.Uint16("port", dst.Port()), zap.Error(err))
		return
	}
	defer peer.Close()
	relayBytes(conn, peer)
}

// relayBytes splices two connections until both directions finish,
// propagating half-closes where the transport supports them.
func relayBytes(a, b net.Conn) {
	done := make(chan struct{}, 2)
	cp := func(dst, src net.Conn) {
		_, _ = io.Copy(dst, src)
		type closeWriter interface{ CloseWrite() error }
		if cw, ok := dst.(closeWriter); ok {
			_ = cw.CloseWrite()
		}
		done <- struct{}{}
	}
	go cp(a, b)
	go cp(b, a)
	<-done
	<-done
}
