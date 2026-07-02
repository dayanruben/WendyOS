package services

import (
	"context"
	"fmt"
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

// meshDialLAN connects to a peer agent's mTLS port and opens a MeshDial
// stream for one TCP connection. The stream gets its own cancellable context
// so it survives past the dial ctx; Close on the returned conn tears it down.
func (d *MeshDialer) meshDialLAN(ctx context.Context, hostport string, port uint16) (net.Conn, error) {
	tlsCfg, err := mtls.NewClientTLSConfig(d.certPEM, d.chainPEM, d.keyPEM, d.logger)
	if err != nil {
		return nil, fmt.Errorf("mesh: client TLS config: %w", err)
	}
	cc, err := grpc.NewClient(hostport, grpc.WithTransportCredentials(credentials.NewTLS(tlsCfg)))
	if err != nil {
		return nil, err
	}
	sctx, cancel := context.WithCancel(context.Background())
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
	return streamNetConn(&meshDialAdapter{stream: stream}, func() { cancel(); cc.Close() }), nil
}

// meshDialBroker opens a ClientTunnel to the target asset through the cloud
// broker, authenticated with this device's asset identity — the same relay
// the CLI uses, from the other kind of caller.
func (d *MeshDialer) meshDialBroker(_ context.Context, deviceID int32, port uint16) (net.Conn, error) {
	opts, md, err := brokerDialOpts(d.orgID, d.assetID, d.chainPEM)
	if err != nil {
		return nil, err
	}
	cc, err := grpc.NewClient(d.brokerURL, opts...)
	if err != nil {
		return nil, err
	}
	sctx, cancel := context.WithCancel(metadata.NewOutgoingContext(context.Background(), md))
	stream, err := cloudpb.NewTunnelBrokerServiceClient(cc).ClientTunnel(sctx)
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

// streamConn exposes a tunnelStream as a net.Conn via an in-process pipe,
// mirroring openBrokerTunnel (cloud_tunnel.go:284-357). It additionally
// implements CloseWrite so callers (mesh.Proxy's relay) can half-close the
// pipe->stream direction while still reading whatever the peer sends back —
// required by the mesh.PeerDialer contract (see proxy.go's relayBytes).
type streamConn struct {
	net.Conn // the local end of the net.Pipe(); Read/Write/etc. pass through

	// remote is the internal pipe end the two relay goroutines use. CloseWrite
	// forces its read deadline into the past so the pipe->stream goroutine's
	// blocked/next Read returns immediately — SetReadDeadline only affects
	// Reads, so the stream->pipe goroutine's Writes (and thus reads from this
	// conn) are unaffected.
	remote net.Conn

	once     *sync.Once
	teardown func()
}

func (c *streamConn) CloseWrite() error {
	return c.remote.SetReadDeadline(time.Unix(0, 1))
}

func (c *streamConn) Close() error {
	c.once.Do(c.teardown)
	return c.Conn.Close()
}

// streamNetConn exposes a tunnelStream as a net.Conn via an in-process pipe,
// mirroring openBrokerTunnel (cloud_tunnel.go:284-357). teardown runs exactly
// once: automatically once both relay directions have finished, or
// immediately on an explicit Close, whichever comes first.
func streamNetConn(s tunnelStream, teardown func()) net.Conn {
	local, remote := net.Pipe()
	var once sync.Once
	var directionsDone int32
	finish := func() {
		// Only tear down the whole stream once both directions are done —
		// CloseWrite deliberately stops just the pipe->stream direction and
		// must not cut off the still-running stream->pipe direction (see
		// streamConn.CloseWrite). An explicit Close bypasses this via once
		// directly.
		if atomic.AddInt32(&directionsDone, 1) >= 2 {
			once.Do(teardown)
		}
	}

	go func() { // stream -> pipe
		defer remote.Close()
		defer finish()
		for {
			payload, halfClose, err := s.recv()
			if err != nil {
				return
			}
			if len(payload) > 0 {
				if _, err := remote.Write(payload); err != nil {
					return
				}
			}
			if halfClose {
				return
			}
		}
	}()

	go func() { // pipe -> stream
		defer finish()
		buf := make([]byte, 256*1024)
		for {
			n, err := remote.Read(buf)
			if n > 0 {
				if sendErr := s.send(buf[:n], false); sendErr != nil {
					return
				}
			}
			if err != nil {
				_ = s.send(nil, true)
				_ = s.closeSend()
				return
			}
		}
	}()

	return &streamConn{
		Conn:     local,
		remote:   remote,
		once:     &once,
		teardown: teardown,
	}
}
