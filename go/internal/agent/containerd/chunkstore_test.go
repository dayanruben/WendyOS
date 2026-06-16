package containerd

import (
	"bytes"
	"crypto/sha256"
	"testing"

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
