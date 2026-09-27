package containerd

import (
	"bytes"
	"context"
	"crypto/sha256"
	"math/rand"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/containerd/containerd/v2/core/content"
	"github.com/containerd/containerd/v2/plugins/content/local"
	digest "github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"github.com/wendylabsinc/wendy/go/internal/shared/chunk"
)

// countingStore counts ReaderAt opens and ReadAt calls against a real store.
type countingStore struct {
	content.Store
	opens, reads atomic.Int32
}

func (s *countingStore) ReaderAt(ctx context.Context, desc ocispec.Descriptor) (content.ReaderAt, error) {
	ra, err := s.Store.ReaderAt(ctx, desc)
	if err != nil {
		return nil, err
	}
	s.opens.Add(1)
	return &countingReaderAt{ReaderAt: ra, reads: &s.reads}, nil
}

type countingReaderAt struct {
	content.ReaderAt
	reads *atomic.Int32
}

func (r *countingReaderAt) ReadAt(p []byte, off int64) (int, error) {
	r.reads.Add(1)
	return r.ReaderAt.ReadAt(p, off)
}

// newCountingStoreClient is newLocalStoreClient with read counting.
func newCountingStoreClient(t *testing.T) (*Client, *countingStore) {
	t.Helper()
	store, err := local.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cs := &countingStore{Store: store}
	c := newStoreClient(t, cs)
	return c, cs
}

func randomBytes(seed int64, n int) []byte {
	b := make([]byte, n)
	rand.New(rand.NewSource(seed)).Read(b)
	return b
}

// commitIndexedBlob writes data to the store as a finished layer blob and
// indexes its chunks, like a layer assembled by an earlier deploy.
func commitIndexedBlob(t *testing.T, c *Client, cs content.Store, data []byte) (digest.Digest, []chunk.Ref) {
	t.Helper()
	dgst := digest.FromBytes(data)
	if err := content.WriteBlob(context.Background(), cs, dgst.String(), bytes.NewReader(data), ocispec.Descriptor{Digest: dgst, Size: int64(len(data))}); err != nil {
		t.Fatal(err)
	}
	refs, err := chunk.ChunkBytes(data)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.chunkIndex.AddLayer(dgst.String(), refs); err != nil {
		t.Fatal(err)
	}
	return dgst, refs
}

func TestPlanAssemblyCoalescesContiguousRuns(t *testing.T) {
	c, _ := newCountingStoreClient(t)
	blob := digest.FromString("previous layer").String()
	var refs []chunk.Ref
	for i := range 5 {
		refs = append(refs, chunk.Ref{Hash: [32]byte{byte(i + 1)}, Offset: uint64(i * 10), Len: 10})
	}
	if err := c.chunkIndex.AddLayer(blob, refs); err != nil {
		t.Fatal(err)
	}
	staged := []byte("a new chunk")
	sh := sha256.Sum256(staged)
	if err := c.staging.write(sh, staged); err != nil {
		t.Fatal(err)
	}

	// r0 r1 r2 | staged | r4: r3 is skipped, so r4 is not contiguous with r2.
	hashes := [][32]byte{refs[0].Hash, refs[1].Hash, refs[2].Hash, sh, refs[4].Hash}
	segs, planned, total, err := c.planAssembly(hashes)
	if err != nil {
		t.Fatal(err)
	}
	if len(segs) != 3 {
		t.Fatalf("got %d segments, want 3: %+v", len(segs), segs)
	}
	if segs[0].blob != blob || segs[0].offset != 0 || segs[0].size != 30 || len(segs[0].chunks) != 3 {
		t.Fatalf("segment 0 = %+v, want r0..r2 as one 30-byte read", segs[0])
	}
	if segs[1].blob != "" || segs[1].size != uint64(len(staged)) {
		t.Fatalf("segment 1 = %+v, want the staged chunk", segs[1])
	}
	if segs[2].blob != blob || segs[2].offset != 40 || segs[2].size != 10 {
		t.Fatalf("segment 2 = %+v, want r4 alone", segs[2])
	}
	if want := int64(40 + len(staged)); total != want {
		t.Fatalf("total = %d, want %d", total, want)
	}
	if planned[3].Offset != 30 || planned[4].Offset != uint64(30+len(staged)) {
		t.Fatalf("refs = %+v, want offsets by prefix sum in the new blob", planned)
	}
}

func TestPlanAssemblySplitsRunsAtMaxSegmentBytes(t *testing.T) {
	c, _ := newCountingStoreClient(t)
	blob := digest.FromString("huge layer").String()
	const size = maxSegmentBytes / 2
	refs := []chunk.Ref{
		{Hash: [32]byte{1}, Offset: 0, Len: size},
		{Hash: [32]byte{2}, Offset: size, Len: size},
		{Hash: [32]byte{3}, Offset: 2 * size, Len: size},
	}
	if err := c.chunkIndex.AddLayer(blob, refs); err != nil {
		t.Fatal(err)
	}
	segs, _, _, err := c.planAssembly([][32]byte{refs[0].Hash, refs[1].Hash, refs[2].Hash})
	if err != nil {
		t.Fatal(err)
	}
	if len(segs) != 2 || segs[0].size != maxSegmentBytes || segs[1].size != size {
		t.Fatalf("segments = %+v, want one full segment then the rest", segs)
	}
}

func TestPlanAssemblyReportsUnavailableChunk(t *testing.T) {
	c, _ := newCountingStoreClient(t)
	if _, _, _, err := c.planAssembly([][32]byte{{9}}); err == nil || !strings.Contains(err.Error(), "unavailable") {
		t.Fatalf("error = %v, want an unavailable chunk", err)
	}
}

// TestAssemblyReadsAStagedChunkFromTheIndexOnceAnotherAssemblyConsumedIt
// covers two layers sharing a staged chunk: the first assembly indexes the
// chunk into its blob and removes the staged file while the second is planned.
func TestAssemblyReadsAStagedChunkFromTheIndexOnceAnotherAssemblyConsumedIt(t *testing.T) {
	c, cs := newCountingStoreClient(t)
	shared := randomBytes(6, 10_000) // below chunk.MinSize: indexing it yields this one chunk
	h := sha256.Sum256(shared)
	if err := c.StageChunk(context.Background(), h, shared); err != nil {
		t.Fatal(err)
	}
	segs, _, _, err := c.planAssembly([][32]byte{h})
	if err != nil {
		t.Fatal(err)
	}

	commitIndexedBlob(t, c, cs, shared) // the other assembly's blob holds the chunk…
	c.staging.remove(h)                 // …and it released the staged file

	var got []byte
	for seg := range c.readSegments(context.Background(), segs, 0) {
		if seg.err != nil {
			t.Fatal(seg.err)
		}
		got = append(got, seg.data...)
	}
	if !bytes.Equal(got, shared) {
		t.Fatal("fallback read returned the wrong bytes")
	}
}
