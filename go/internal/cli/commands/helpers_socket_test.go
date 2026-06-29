package commands

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/wendylabsinc/wendy/go/proto/gen/agentpb"
	"google.golang.org/grpc"
)

type versionOnlyAgent struct {
	agentpb.UnimplementedWendyAgentServiceServer
}

func (versionOnlyAgent) GetAgentVersion(context.Context, *agentpb.GetAgentVersionRequest) (*agentpb.GetAgentVersionResponse, error) {
	return &agentpb.GetAgentVersionResponse{Version: "sock"}, nil
}

func TestConnectWithAutoTLS_UsesAgentSocketEnv(t *testing.T) {
	// Short temp dir to stay under the unix-socket sun_path limit on macOS
	// (see localsocket_test.go for the convention).
	dir, err := os.MkdirTemp("/tmp", "wsk")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	sock := filepath.Join(dir, "agent.sock")

	lis, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := grpc.NewServer()
	agentpb.RegisterWendyAgentServiceServer(srv, versionOnlyAgent{})
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)

	t.Setenv("WENDY_AGENT_SOCKET", sock)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// plaintextAddr is deliberately bogus: if the socket path is honored we never
	// touch the network and this unroutable address is never dialed.
	conn, _, err := connectWithAutoTLSDiagnostics(ctx, "203.0.113.1:50051")
	if err != nil {
		t.Fatalf("connectWithAutoTLSDiagnostics: %v", err)
	}
	defer conn.Close()
	resp, err := conn.AgentService.GetAgentVersion(ctx, &agentpb.GetAgentVersionRequest{})
	if err != nil {
		t.Fatalf("GetAgentVersion: %v", err)
	}
	if resp.GetVersion() != "sock" {
		t.Fatalf("got %q, want sock — did not route through the socket", resp.GetVersion())
	}
}
