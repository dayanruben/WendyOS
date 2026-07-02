package services

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"

	agentpbv2 "github.com/wendylabsinc/wendy/go/proto/gen/agentpb/v2"
)

func certWithURN(t *testing.T, urn string) *x509.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(urn)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		URIs:         []*url.URL{u},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return cert
}

func ctxWithPeerCert(cert *x509.Certificate) context.Context {
	return peer.NewContext(context.Background(), &peer.Peer{
		AuthInfo: credentials.TLSInfo{State: tls.ConnectionState{PeerCertificates: []*x509.Certificate{cert}}},
	})
}

// fakeMeshDialStream implements agentpbv2.WendyMeshService_MeshDialServer.
type fakeMeshDialStream struct {
	agentpbv2.WendyMeshService_MeshDialServer // panics if an unstubbed method is hit
	ctx                                       context.Context
	in                                        chan *agentpbv2.MeshDialMessage
	out                                       chan *agentpbv2.MeshDialData
}

func (f *fakeMeshDialStream) Context() context.Context { return f.ctx }
func (f *fakeMeshDialStream) Recv() (*agentpbv2.MeshDialMessage, error) {
	m, ok := <-f.in
	if !ok {
		return nil, io.EOF
	}
	return m, nil
}
func (f *fakeMeshDialStream) Send(d *agentpbv2.MeshDialData) error {
	f.out <- d
	return nil
}

func newMeshServiceForTest(t *testing.T) (*MeshService, string) {
	t.Helper()
	dir := t.TempDir()
	return NewMeshService(zap.NewNop(), dir), dir
}

func TestMeshDialRejectsUserCert(t *testing.T) {
	svc, _ := newMeshServiceForTest(t)
	stream := &fakeMeshDialStream{ctx: ctxWithPeerCert(certWithURN(t, "urn:wendy:org:7:user:9"))}
	err := svc.MeshDial(stream)
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("err = %v, want PermissionDenied", err)
	}
}

func TestMeshDialRejectsNoPeer(t *testing.T) {
	svc, _ := newMeshServiceForTest(t)
	stream := &fakeMeshDialStream{ctx: context.Background()}
	if status.Code(svc.MeshDial(stream)) != codes.PermissionDenied {
		t.Fatal("want PermissionDenied without mTLS peer info")
	}
}

func TestMeshDialRejectsWhenDisabled(t *testing.T) {
	svc, dir := newMeshServiceForTest(t)
	if err := os.WriteFile(filepath.Join(dir, "mesh-disabled"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	stream := &fakeMeshDialStream{ctx: ctxWithPeerCert(certWithURN(t, "urn:wendy:org:7:asset:215"))}
	if status.Code(svc.MeshDial(stream)) != codes.PermissionDenied {
		t.Fatal("want PermissionDenied when mesh-disabled file exists")
	}
}

func TestMeshDialRequiresOpenFirst(t *testing.T) {
	svc, _ := newMeshServiceForTest(t)
	in := make(chan *agentpbv2.MeshDialMessage, 1)
	in <- &agentpbv2.MeshDialMessage{Content: &agentpbv2.MeshDialMessage_Data{Data: &agentpbv2.MeshDialData{Payload: []byte("x")}}}
	stream := &fakeMeshDialStream{ctx: ctxWithPeerCert(certWithURN(t, "urn:wendy:org:7:asset:215")), in: in}
	if status.Code(svc.MeshDial(stream)) != codes.InvalidArgument {
		t.Fatal("want InvalidArgument when first message is not open")
	}
}

func TestMeshDialRelaysToLocalPort(t *testing.T) {
	// Local echo server standing in for a published app port.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		buf := make([]byte, 64)
		n, _ := c.Read(buf)
		fmt.Fprintf(c, "echo:%s", buf[:n])
		c.Close()
	}()

	svc, _ := newMeshServiceForTest(t)
	// Point local dialing at the test listener regardless of requested port.
	svc.dialLocal = func(_ string, timeout time.Duration) (net.Conn, error) {
		return net.DialTimeout("tcp", ln.Addr().String(), timeout)
	}

	in := make(chan *agentpbv2.MeshDialMessage, 3)
	out := make(chan *agentpbv2.MeshDialData, 3)
	in <- &agentpbv2.MeshDialMessage{Content: &agentpbv2.MeshDialMessage_Open{Open: &agentpbv2.MeshDialOpen{Port: 8080}}}
	in <- &agentpbv2.MeshDialMessage{Content: &agentpbv2.MeshDialMessage_Data{Data: &agentpbv2.MeshDialData{Payload: []byte("ping")}}}
	close(in)
	stream := &fakeMeshDialStream{ctx: ctxWithPeerCert(certWithURN(t, "urn:wendy:org:7:asset:215")), in: in, out: out}

	if err := svc.MeshDial(stream); err != nil {
		t.Fatalf("MeshDial: %v", err)
	}
	var got []byte
	for d := range collectUntilHalfClose(out) {
		got = append(got, d...)
	}
	if string(got) != "echo:ping" {
		t.Fatalf("relayed %q, want %q", got, "echo:ping")
	}
}

// collectUntilHalfClose drains payloads until a half_close frame arrives.
func collectUntilHalfClose(out chan *agentpbv2.MeshDialData) <-chan []byte {
	ch := make(chan []byte)
	go func() {
		defer close(ch)
		for d := range out {
			if len(d.Payload) > 0 {
				ch <- d.Payload
			}
			if d.HalfClose {
				return
			}
		}
	}()
	return ch
}
