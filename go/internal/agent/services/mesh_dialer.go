package services

import (
	"context"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"

	"github.com/wendylabsinc/wendy/go/internal/agent/mesh"
	"github.com/wendylabsinc/wendy/go/internal/agent/mtls"
	"github.com/wendylabsinc/wendy/go/internal/shared/discovery"
	agentpbv2 "github.com/wendylabsinc/wendy/go/proto/gen/agentpb/v2"
	cloudpb "github.com/wendylabsinc/wendy/go/proto/gen/cloudpb"
)

// Compile-time assertion that MeshDialer satisfies mesh.PeerDialer (Task 4).
var _ mesh.PeerDialer = (*MeshDialer)(nil)

const (
	// lanBudget bounds mDNS discovery + LAN connect before falling back to
	// the cloud relay; the outcome cache keeps repeat dials from re-paying it.
	lanBudget   = 1 * time.Second
	lanCacheTTL = 60 * time.Second
)

// MeshDialer implements mesh.PeerDialer: LAN-direct MeshDial when the peer is
// discoverable locally, cloud-broker ClientTunnel otherwise.
type MeshDialer struct {
	logger    *zap.Logger
	brokerURL string
	orgID     int32
	assetID   int32
	certPEM   string
	keyPEM    string
	chainPEM  string

	// Seams (overridden in tests).
	lanLookup  func(ctx context.Context, assetID int32) (hostport string, ok bool)
	dialLAN    func(ctx context.Context, hostport string, port uint16) (net.Conn, error)
	dialBroker func(ctx context.Context, deviceID int32, port uint16) (net.Conn, error)
	now        func() time.Time

	mu    sync.Mutex
	cache map[int32]lanCacheEntry
}

type lanCacheEntry struct {
	hostport string // "" = not on LAN
	expires  time.Time
}

// NewMeshDialer builds a MeshDialer. certPEM/keyPEM/chainPEM are this
// device's mTLS asset identity, used both to dial peers directly on the LAN
// and to authenticate to the cloud tunnel broker.
func NewMeshDialer(logger *zap.Logger, brokerURL string, orgID, assetID int32, certPEM, keyPEM, chainPEM string) *MeshDialer {
	d := &MeshDialer{
		logger:    logger,
		brokerURL: brokerURL,
		orgID:     orgID,
		assetID:   assetID,
		certPEM:   certPEM,
		keyPEM:    keyPEM,
		chainPEM:  chainPEM,
		now:       time.Now,
		cache:     make(map[int32]lanCacheEntry),
	}
	d.lanLookup = d.discoverOnLAN
	d.dialLAN = d.meshDialLAN
	d.dialBroker = d.meshDialBroker
	return d
}

// DialDevice opens a byte stream to port on the given mesh device. ctx bounds
// dialing only; the returned conn has an independent lifetime.
func (d *MeshDialer) DialDevice(ctx context.Context, deviceID int32, port uint16) (net.Conn, error) {
	if hostport, ok := d.lanAddr(ctx, deviceID); ok {
		conn, err := d.dialLAN(ctx, hostport, port)
		if err == nil {
			return conn, nil
		}
		d.logger.Warn("mesh: LAN dial failed, falling back to cloud relay",
			zap.Int32("device_id", deviceID), zap.String("lan_addr", hostport), zap.Error(err))
	}
	return d.dialBroker(ctx, deviceID, port)
}

// lanAddr resolves deviceID to a LAN hostport, consulting (and refreshing) the
// TTL cache so repeat dials don't re-pay the mDNS browse budget. Both
// positive and negative outcomes are cached.
func (d *MeshDialer) lanAddr(ctx context.Context, deviceID int32) (string, bool) {
	d.mu.Lock()
	if e, ok := d.cache[deviceID]; ok && d.now().Before(e.expires) {
		d.mu.Unlock()
		return e.hostport, e.hostport != ""
	}
	d.mu.Unlock()

	lctx, cancel := context.WithTimeout(ctx, lanBudget)
	hostport, ok := d.lanLookup(lctx, deviceID)
	cancel()
	if !ok {
		hostport = ""
	}
	d.mu.Lock()
	d.cache[deviceID] = lanCacheEntry{hostport: hostport, expires: d.now().Add(lanCacheTTL)}
	d.mu.Unlock()
	return hostport, hostport != ""
}

// discoverOnLAN browses _wendyos._udp for a provisioned device advertising
// the target asset ID.
func (d *MeshDialer) discoverOnLAN(ctx context.Context, assetID int32) (string, bool) {
	devices, err := discovery.Discover(ctx, discovery.DiscoveryOptions{})
	if err != nil {
		return "", false
	}
	for _, dev := range devices.LANDevices {
		if dev.AssetID == assetID && dev.IsMTLS && dev.IPAddress != "" {
			return net.JoinHostPort(dev.IPAddress, strconv.Itoa(dev.Port)), true
		}
	}
	return "", false
}

// dialBoundContext returns a stream context that outlives the caller's ctx —
// the returned conn must survive past the dial budget — while still letting
// ctx bound the connect/stream-open phase: until established() is called,
// cancelling ctx also cancels sctx. Callers must call cancel to release the
// stream (typically via the conn's teardown) and established() once the
// stream is up and the open frame has been sent.
func dialBoundContext(ctx context.Context) (sctx context.Context, cancel context.CancelFunc, established func()) {
	sctx, cancel = context.WithCancel(context.Background())
	stop := context.AfterFunc(ctx, cancel)
	return sctx, cancel, func() { stop() }
}

// meshDialLAN connects to a peer agent's mTLS port and opens a MeshDial
// stream for one TCP connection. ctx bounds connecting and opening the
// stream; once established the stream survives past ctx, and Close on the
// returned conn tears it down.
func (d *MeshDialer) meshDialLAN(ctx context.Context, hostport string, port uint16) (net.Conn, error) {
	tlsCfg, err := mtls.NewClientTLSConfig(d.certPEM, d.chainPEM, d.keyPEM, d.logger)
	if err != nil {
		return nil, fmt.Errorf("mesh: client TLS config: %w", err)
	}
	cc, err := grpc.NewClient(hostport, grpc.WithTransportCredentials(credentials.NewTLS(tlsCfg)))
	if err != nil {
		return nil, err
	}
	sctx, cancel, established := dialBoundContext(ctx)
	stream, err := agentpbv2.NewWendyMeshServiceClient(cc).MeshDial(sctx)
	if err != nil {
		cancel()
		cc.Close()
		return nil, err
	}
	if err := stream.Send(&agentpbv2.MeshDialMessage{
		Content: &agentpbv2.MeshDialMessage_Open{Open: &agentpbv2.MeshDialOpen{Port: uint32(port)}},
	}); err != nil {
		cancel()
		cc.Close()
		return nil, err
	}
	established()
	return streamNetConn(&meshDialAdapter{stream: stream}, func() { cancel(); cc.Close() }), nil
}

// meshDialBroker opens a ClientTunnel to the target asset through the cloud
// broker, authenticated with this device's asset identity — the same relay
// the CLI uses, from the other kind of caller. ctx bounds connecting and
// opening the tunnel; once established the stream survives past ctx.
func (d *MeshDialer) meshDialBroker(ctx context.Context, deviceID int32, port uint16) (net.Conn, error) {
	opts, md, err := brokerDialOpts(d.logger, d.orgID, d.assetID, d.chainPEM)
	if err != nil {
		return nil, err
	}
	cc, err := grpc.NewClient(d.brokerURL, opts...)
	if err != nil {
		return nil, err
	}
	sctx, cancel, established := dialBoundContext(ctx)
	stream, err := cloudpb.NewTunnelBrokerServiceClient(cc).ClientTunnel(metadata.NewOutgoingContext(sctx, md))
	if err != nil {
		cancel()
		cc.Close()
		return nil, err
	}
	if err := stream.Send(&cloudpb.ClientTunnelMessage{
		Content: &cloudpb.ClientTunnelMessage_Open{Open: &cloudpb.ClientTunnelOpen{
			AssetId: deviceID,
			Host:    "localhost",
			Port:    uint32(port),
		}},
	}); err != nil {
		cancel()
		cc.Close()
		return nil, err
	}
	established()
	return streamNetConn(&clientTunnelAdapter{stream: stream}, func() { cancel(); cc.Close() }), nil
}

// tunnelStream abstracts the two stream framings (MeshDial vs ClientTunnel)
// behind one send/recv shape so streamNetConn can serve both.
type tunnelStream interface {
	send(payload []byte, halfClose bool) error
	recv() (payload []byte, halfClose bool, err error)
	closeSend() error
}

type meshDialAdapter struct {
	stream agentpbv2.WendyMeshService_MeshDialClient
}

func (a *meshDialAdapter) send(p []byte, hc bool) error {
	return a.stream.Send(&agentpbv2.MeshDialMessage{
		Content: &agentpbv2.MeshDialMessage_Data{Data: &agentpbv2.MeshDialData{Payload: p, HalfClose: hc}},
	})
}
func (a *meshDialAdapter) recv() ([]byte, bool, error) {
	m, err := a.stream.Recv()
	if err != nil {
		return nil, false, err
	}
	return m.Payload, m.HalfClose, nil
}
func (a *meshDialAdapter) closeSend() error { return a.stream.CloseSend() }

type clientTunnelAdapter struct {
	stream cloudpb.TunnelBrokerService_ClientTunnelClient
}

func (a *clientTunnelAdapter) send(p []byte, hc bool) error {
	return a.stream.Send(&cloudpb.ClientTunnelMessage{
		Content: &cloudpb.ClientTunnelMessage_Data{Data: &cloudpb.TunnelData{Payload: p, HalfClose: hc}},
	})
}
func (a *clientTunnelAdapter) recv() ([]byte, bool, error) {
	m, err := a.stream.Recv()
	if err != nil {
		return nil, false, err
	}
	return m.Payload, m.HalfClose, nil
}
func (a *clientTunnelAdapter) closeSend() error { return a.stream.CloseSend() }

// streamConn exposes a tunnelStream directly as a net.Conn — no intermediate
// pipe, no relay goroutines — so the two stream directions stay fully
// independent: an inbound half_close ends only Reads (never truncating an
// in-flight upload), and CloseWrite ends only Writes while Reads keep
// draining the peer's response.
//
// Concurrency: Read may run concurrently with Write/CloseWrite/Close, but
// there must be at most one concurrent Read and one concurrent Write — the
// same single-reader/single-writer shape mesh.relayBytes uses — which maps
// exactly onto gRPC's one-concurrent-Recv/one-concurrent-Send stream rule.
//
// Close must always be called to release the underlying stream: teardown
// cancels the stream context and closes the gRPC connection, unblocking any
// Read/Write parked in recv/send, and runs exactly once.
type streamConn struct {
	s        tunnelStream
	teardown func()
	once     sync.Once

	// Read-side state; only the (single) reader touches these.
	readBuf []byte // leftover payload from a frame larger than the Read buffer
	readErr error  // sticky; io.EOF after a clean end or inbound half_close

	// writeClosed flips once on CloseWrite; Write and CloseWrite check it.
	writeClosed atomic.Bool
}

func (c *streamConn) Read(p []byte) (int, error) {
	for {
		if len(c.readBuf) > 0 {
			n := copy(p, c.readBuf)
			c.readBuf = c.readBuf[n:]
			return n, nil
		}
		if c.readErr != nil {
			return 0, c.readErr
		}
		payload, halfClose, err := c.s.recv()
		if err != nil {
			// gRPC returns io.EOF for a clean stream end; pass it (and any
			// other error) through sticky so subsequent Reads repeat it.
			c.readErr = err
			return 0, c.readErr
		}
		if halfClose {
			// Serve this frame's data first; EOF starts on the next Read.
			c.readErr = io.EOF
		}
		c.readBuf = payload
	}
}

func (c *streamConn) Write(p []byte) (int, error) {
	if c.writeClosed.Load() {
		return 0, net.ErrClosed
	}
	if err := c.s.send(p, false); err != nil {
		return 0, err
	}
	return len(p), nil
}

// CloseWrite half-closes the write direction: it sends a half_close frame and
// closes the gRPC send side. Reads are unaffected. Idempotent.
func (c *streamConn) CloseWrite() error {
	if c.writeClosed.Swap(true) {
		return nil
	}
	err := c.s.send(nil, true)
	if cerr := c.s.closeSend(); err == nil {
		err = cerr
	}
	return err
}

// Close releases the underlying stream (running teardown exactly once) and
// never blocks. Any concurrently parked Read/Write is unblocked with an error
// by the stream context cancellation inside teardown.
func (c *streamConn) Close() error {
	c.once.Do(c.teardown)
	return nil
}

// Deadlines are not supported: the conn is driven by relay loops that run
// until EOF/error, and Close unblocks parked calls. Accept and ignore them.
func (c *streamConn) SetDeadline(time.Time) error      { return nil }
func (c *streamConn) SetReadDeadline(time.Time) error  { return nil }
func (c *streamConn) SetWriteDeadline(time.Time) error { return nil }

func (c *streamConn) LocalAddr() net.Addr  { return streamConnAddr{} }
func (c *streamConn) RemoteAddr() net.Addr { return streamConnAddr{} }

type streamConnAddr struct{}

func (streamConnAddr) Network() string { return "wendy-mesh" }
func (streamConnAddr) String() string  { return "wendy-mesh" }

// streamNetConn exposes a tunnelStream as a net.Conn. teardown must release
// the stream (cancel its context + close the gRPC conn); it runs exactly
// once, on Close.
func streamNetConn(s tunnelStream, teardown func()) net.Conn {
	return &streamConn{s: s, teardown: teardown}
}
