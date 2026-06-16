package containerd

import (
	"bytes"
	"context"
	"crypto/sha256"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/wendylabsinc/wendy/go/internal/shared/chunk"
)

func TestReconstructFromChunks(t *testing.T) {
	full := bytes.Repeat([]byte("wendy-layer-"), 50_000) // ~600 KiB, multi-chunk
	refs, err := chunk.Chunk(bytes.NewReader(full))
	if err != nil {
		t.Fatal(err)
	}

	// Simulate: every chunk is "staged" (available by hash).
	staged := map[[32]byte][]byte{}
	var order [][32]byte
	off := 0
	for _, r := range refs {
		staged[r.Hash] = full[off : off+int(r.Len)]
		order = append(order, r.Hash)
		off += int(r.Len)
	}

	got, err := reconstruct(order, func(h [32]byte) ([]byte, error) { return staged[h], nil })
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, full) {
		t.Fatalf("reconstructed bytes differ (len %d vs %d)", len(got), len(full))
	}
	if sha256.Sum256(got) != sha256.Sum256(full) {
		t.Fatal("digest mismatch")
	}
}

func stage(c *Client, data []byte) error {
	return c.StageChunk(context.Background(), sha256.Sum256(data), data)
}

func TestStageChunkRejectsOversizedChunk(t *testing.T) {
	c := &Client{staging: newStaging()}
	big := bytes.Repeat([]byte{0xab}, maxStagedChunkBytes+1)
	err := stage(c, big)
	if status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("expected ResourceExhausted for oversized chunk, got %v", err)
	}
	if c.staging.totalBytes != 0 {
		t.Fatalf("oversized chunk must not be staged; totalBytes=%d", c.staging.totalBytes)
	}
}

func TestStageChunkEnforcesTotalCapAndAccounting(t *testing.T) {
	c := &Client{staging: newStaging()}

	// A distinct 1 MiB chunk indexed by i in its first byte.
	chunkN := func(i byte) []byte {
		b := bytes.Repeat([]byte{i}, 1<<20)
		b[0] = i
		return b
	}

	// Stage two distinct chunks; totalBytes tracks their sum.
	a, b := chunkN(1), chunkN(2)
	if err := stage(c, a); err != nil {
		t.Fatalf("stage a: %v", err)
	}
	if err := stage(c, b); err != nil {
		t.Fatalf("stage b: %v", err)
	}
	if want := int64(len(a) + len(b)); c.staging.totalBytes != want {
		t.Fatalf("totalBytes=%d, want %d", c.staging.totalBytes, want)
	}

	// Re-staging an already-staged chunk is idempotent and must not double-count.
	if err := stage(c, a); err != nil {
		t.Fatalf("re-stage a: %v", err)
	}
	if want := int64(len(a) + len(b)); c.staging.totalBytes != want {
		t.Fatalf("idempotent re-stage changed totalBytes to %d, want %d", c.staging.totalBytes, want)
	}

	// Pretend the staging buffer is nearly full, then exceed the cap.
	c.staging.totalBytes = c.staging.maxBytes - 10
	err := stage(c, chunkN(3))
	if status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("expected ResourceExhausted when over total cap, got %v", err)
	}
}
