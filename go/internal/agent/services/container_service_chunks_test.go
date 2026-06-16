package services

import (
	"bytes"
	"context"
	"io"
	"testing"

	"go.uber.org/zap"
	"google.golang.org/grpc"

	agentpb "github.com/wendylabsinc/wendy/go/proto/gen/agentpb"
)

// fakeContainerd embeds mockContainerdClient and adds a hook for MissingChunks
// so chunk-related tests can inject custom behaviour without touching the shared mock.
type fakeContainerd struct {
	mockContainerdClient
	missingFn func(ctx context.Context, hashes [][32]byte) ([][32]byte, error)
}

func newFakeContainerd() *fakeContainerd {
	return &fakeContainerd{}
}

// MissingChunks delegates to missingFn when set; otherwise returns all hashes unchanged.
func (f *fakeContainerd) MissingChunks(ctx context.Context, hashes [][32]byte) ([][32]byte, error) {
	if f.missingFn != nil {
		return f.missingFn(ctx, hashes)
	}
	return hashes, nil
}

func TestQueryChunksReturnsMissing(t *testing.T) {
	fake := newFakeContainerd()
	fake.missingFn = func(_ context.Context, hs [][32]byte) ([][32]byte, error) {
		return hs[1:], nil // pretend the first is present
	}
	svc := NewContainerService(zap.NewNop(), fake)

	h0 := bytes.Repeat([]byte{0}, 32)
	h1 := bytes.Repeat([]byte{1}, 32)
	resp, err := svc.QueryChunks(context.Background(), &agentpb.QueryChunksRequest{
		ChunkHashes: [][]byte{h0, h1},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.GetMissingHashes()) != 1 || !bytes.Equal(resp.GetMissingHashes()[0], h1) {
		t.Fatalf("expected only h1 missing, got %v", resp.GetMissingHashes())
	}
}

// writeChunksStream is a fake grpc.ClientStreamingServer for unit-testing WriteChunks.
type writeChunksStream struct {
	grpc.ServerStream
	msgs []*agentpb.WriteChunksRequest
	pos  int
	sent *agentpb.WriteChunksResponse
	ctx  context.Context
}

func (s *writeChunksStream) Context() context.Context { return s.ctx }
func (s *writeChunksStream) Recv() (*agentpb.WriteChunksRequest, error) {
	if s.pos >= len(s.msgs) {
		return nil, io.EOF
	}
	m := s.msgs[s.pos]
	s.pos++
	return m, nil
}
func (s *writeChunksStream) SendAndClose(r *agentpb.WriteChunksResponse) error {
	s.sent = r
	return nil
}

func TestWriteChunks(t *testing.T) {
	fake := newFakeContainerd()
	svc := NewContainerService(zap.NewNop(), fake)

	h0 := bytes.Repeat([]byte{0x11}, 32)
	stream := &writeChunksStream{
		ctx: context.Background(),
		msgs: []*agentpb.WriteChunksRequest{
			{Hash: h0, Data: []byte("data")},
		},
	}
	if err := svc.WriteChunks(stream); err != nil {
		t.Fatal(err)
	}
	if stream.sent == nil {
		t.Fatal("SendAndClose was never called")
	}
}
