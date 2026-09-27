package containerd

import (
	"bytes"
	"context"
	"crypto/sha256"
	"math/rand"
	"os"
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

// TestAssembleLayerFromChunksReadsReusedChunksInFewReads is the WDY-3213 case:
// a rebuilt layer that is mostly an earlier layer's chunks plus a few new ones.
func TestAssembleLayerFromChunksReadsReusedChunksInFewReads(t *testing.T) {
	c, cs := newCountingStoreClient(t)
	previous := randomBytes(1, 2<<20)
	_, prevRefs := commitIndexedBlob(t, c, cs, previous)
	if len(prevRefs) < 8 {
		t.Fatalf("fixture has %d chunks, want several", len(prevRefs))
	}

	// New layer: the first half of the old chunks, one new chunk, the rest.
	half := len(prevRefs) / 2
	fresh := randomBytes(2, 40_000)
	var layer []byte
	var hashes [][32]byte
	for _, r := range prevRefs[:half] {
		layer = append(layer, previous[r.Offset:r.Offset+r.Len]...)
		hashes = append(hashes, r.Hash)
	}
	fh := sha256.Sum256(fresh)
	if err := c.StageChunk(context.Background(), fh, fresh); err != nil {
		t.Fatal(err)
	}
	layer = append(layer, fresh...)
	hashes = append(hashes, fh)
	for _, r := range prevRefs[half:] {
		layer = append(layer, previous[r.Offset:r.Offset+r.Len]...)
		hashes = append(hashes, r.Hash)
	}
	diffID := digest.FromBytes(layer)
	cs.opens.Store(0)
	cs.reads.Store(0)

	if err := c.AssembleLayerFromChunks(context.Background(), diffID.String(), hashes); err != nil {
		t.Fatal(err)
	}

	info, err := cs.Info(context.Background(), diffID)
	if err != nil || info.Size != int64(len(layer)) {
		t.Fatalf("assembled blob: %+v, %v", info, err)
	}
	if got := cs.opens.Load(); got != 1 {
		t.Fatalf("opened the previous blob %d times, want once for the whole assembly", got)
	}
	if got := cs.reads.Load(); got != 2 {
		t.Fatalf("%d reads for %d reused chunks in two runs, want 2", got, len(prevRefs))
	}
}

func TestAssembleLayerFromChunksResumesAPartialIngest(t *testing.T) {
	c, cs := newCountingStoreClient(t)
	layer := randomBytes(3, 600_000)
	refs, err := chunk.ChunkBytes(layer)
	if err != nil {
		t.Fatal(err)
	}
	hashes := make([][32]byte, len(refs))
	for i, r := range refs {
		hashes[i] = r.Hash
		if err := c.StageChunk(context.Background(), r.Hash, layer[r.Offset:r.Offset+r.Len]); err != nil {
			t.Fatal(err)
		}
	}
	diffID := digest.FromBytes(layer)

	// An earlier attempt wrote part of the layer under the same ref, then died.
	w, err := content.OpenWriter(context.Background(), cs, content.WithRef(diffID.String()),
		content.WithDescriptor(ocispec.Descriptor{Digest: diffID, Size: int64(len(layer))}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(layer[:250_000]); err != nil {
		t.Fatal(err)
	}
	w.Close()

	if err := c.AssembleLayerFromChunks(context.Background(), diffID.String(), hashes); err != nil {
		t.Fatalf("resuming the partial ingest: %v", err)
	}
	ra, err := cs.Store.ReaderAt(context.Background(), ocispec.Descriptor{Digest: diffID})
	if err != nil {
		t.Fatal(err)
	}
	defer ra.Close()
	got := make([]byte, len(layer))
	if _, err := ra.ReadAt(got, 0); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, layer) {
		t.Fatal("resumed blob differs from the layer")
	}
}

func TestAssembleLayerFromChunksRejectsACorruptStagedChunk(t *testing.T) {
	c, cs := newCountingStoreClient(t)
	data := randomBytes(4, 50_000)
	h := sha256.Sum256(data)
	if err := c.StageChunk(context.Background(), h, data); err != nil {
		t.Fatal(err)
	}
	// Bit rot after staging: the file no longer matches its name.
	if err := os.WriteFile(c.staging.path(h), randomBytes(5, len(data)), 0o600); err != nil {
		t.Fatal(err)
	}
	diffID := digest.FromBytes(data)
	err := c.AssembleLayerFromChunks(context.Background(), diffID.String(), [][32]byte{h})
	if err == nil || !strings.Contains(err.Error(), "hash mismatch") {
		t.Fatalf("error = %v, want a hash mismatch", err)
	}
	if _, err := cs.Info(context.Background(), diffID); err == nil {
		t.Fatal("a layer with a corrupt chunk was committed")
	}
}

// TestWriteAssembledLayerReportsUnverifiedWhenTheBlobAlreadyExists covers rule
// 1's first bullet directly: content.OpenWriter reports AlreadyExists before
// anything reads a segment, so writeAssembledLayer must say so rather than
// claim it verified a manifest it never looked at.
func TestWriteAssembledLayerReportsUnverifiedWhenTheBlobAlreadyExists(t *testing.T) {
	c, cs := newCountingStoreClient(t)
	data := randomBytes(7, 1_000)
	dgst, _ := commitIndexedBlob(t, c, cs, data)

	verified, err := c.writeAssembledLayer(context.Background(), dgst.String(), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if verified {
		t.Fatal("verified = true for a blob this call never read")
	}
}
