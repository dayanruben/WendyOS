package containerd

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	digest "github.com/opencontainers/go-digest"
	"go.uber.org/zap"

	"github.com/wendylabsinc/wendy/go/internal/shared/chunk"
)

// newTestChunkIndex opens an index in a temp dir, closed when the test ends.
func newTestChunkIndex(t *testing.T) *ChunkIndex {
	t.Helper()
	ix, err := OpenChunkIndex(filepath.Join(t.TempDir(), "chunk-index.db"), "", zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ix.Close() })
	return ix
}

func TestChunkIndexAddLookup(t *testing.T) {
	ix := newTestChunkIndex(t)
	blob := digest.FromString("layer a").String()
	h1, h2 := [32]byte{1}, [32]byte{2}
	if err := ix.AddLayer(blob, []chunk.Ref{{Hash: h1, Offset: 7, Len: 10}}); err != nil {
		t.Fatal(err)
	}

	loc, ok := ix.Has(h1)
	if !ok || loc != (chunkLoc{Blob: blob, Offset: 7, Len: 10}) {
		t.Fatalf("Has(h1) = %+v, %v", loc, ok)
	}
	locs, found, err := ix.Lookup([][32]byte{h1, h2})
	if err != nil {
		t.Fatal(err)
	}
	if !found[0] || found[1] || locs[0].Offset != 7 {
		t.Fatalf("Lookup = %+v %v", locs, found)
	}
}

func TestChunkIndexPersistsAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "chunk-index.db")
	ix, err := OpenChunkIndex(path, "", zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	blob := digest.FromString("layer b").String()
	h := [32]byte{9}
	if err := ix.AddLayer(blob, []chunk.Ref{{Hash: h, Offset: 5, Len: 7}}); err != nil {
		t.Fatal(err)
	}
	if err := ix.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := OpenChunkIndex(path, "", zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if _, ok := reopened.Has(h); !ok {
		t.Fatal("entry lost across reopen")
	}
}

func TestChunkIndexDropRemovesOnlyThatBlobsEntries(t *testing.T) {
	ix := newTestChunkIndex(t)
	older, newer := digest.FromString("old layer").String(), digest.FromString("new layer").String()
	shared, onlyOld, onlyNew := [32]byte{1}, [32]byte{2}, [32]byte{3}
	if err := ix.AddLayer(older, []chunk.Ref{{Hash: shared, Len: 1}, {Hash: onlyOld, Offset: 1, Len: 1}}); err != nil {
		t.Fatal(err)
	}
	// The newer blob re-indexes the shared chunk: last writer wins.
	if err := ix.AddLayer(newer, []chunk.Ref{{Hash: shared, Offset: 4, Len: 1}, {Hash: onlyNew, Offset: 5, Len: 1}}); err != nil {
		t.Fatal(err)
	}

	if err := ix.Drop(older); err != nil {
		t.Fatal(err)
	}
	if _, ok := ix.Has(onlyOld); ok {
		t.Fatal("entry of the dropped blob survived")
	}
	if loc, ok := ix.Has(shared); !ok || loc.Blob != newer || loc.Offset != 4 {
		t.Fatalf("shared chunk = %+v, %v; want it still in the newer blob", loc, ok)
	}
	if _, ok := ix.Has(onlyNew); !ok {
		t.Fatal("entry of the surviving blob was removed")
	}
	blobs, err := ix.Blobs()
	if err != nil {
		t.Fatal(err)
	}
	if len(blobs) != 1 || blobs[0] != newer {
		t.Fatalf("Blobs = %v, want [%s]", blobs, newer)
	}
	if n, err := ix.Len(); err != nil || n != 2 {
		t.Fatalf("Len = %d, %v; want 2", n, err)
	}
}

func TestChunkIndexAddLayerSpansTransactions(t *testing.T) {
	ix := newTestChunkIndex(t)
	blob := digest.FromString("big layer").String()
	refs := make([]chunk.Ref, chunkIndexTxEntries+10)
	for i := range refs {
		refs[i] = chunk.Ref{Hash: [32]byte{byte(i), byte(i >> 8), byte(i >> 16), 0xaa}, Offset: uint64(i), Len: 1}
	}
	if err := ix.AddLayer(blob, refs); err != nil {
		t.Fatal(err)
	}
	if n, err := ix.Len(); err != nil || n != len(refs) {
		t.Fatalf("Len = %d, %v; want %d", n, err, len(refs))
	}
	if err := ix.Drop(blob); err != nil {
		t.Fatal(err)
	}
	if n, err := ix.Len(); err != nil || n != 0 {
		t.Fatalf("Len after Drop = %d, %v; want 0", n, err)
	}
}

func TestChunkIndexRejectsNonSHA256Blob(t *testing.T) {
	ix := newTestChunkIndex(t)
	if err := ix.AddLayer("sha512:"+hex.EncodeToString(make([]byte, 64)), []chunk.Ref{{Hash: [32]byte{1}, Len: 1}}); err == nil {
		t.Fatal("expected an error for a non-sha256 blob digest")
	}
}

func TestOpenChunkIndexImportsAndRemovesLegacyJSON(t *testing.T) {
	dir := t.TempDir()
	legacy := filepath.Join(dir, "chunk-index.json")
	blob := digest.FromString("legacy layer").String()
	h := [32]byte{4}
	data, err := json.Marshal(map[string]chunkLoc{
		hex.EncodeToString(h[:]): {Blob: blob, Offset: 3, Len: 9},
		"not-hex":                {Blob: blob, Offset: 0, Len: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacy, data, 0o644); err != nil {
		t.Fatal(err)
	}

	ix, err := OpenChunkIndex(filepath.Join(dir, "chunk-index.db"), legacy, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer ix.Close()
	if loc, ok := ix.Has(h); !ok || loc != (chunkLoc{Blob: blob, Offset: 3, Len: 9}) {
		t.Fatalf("migrated entry = %+v, %v", loc, ok)
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Fatalf("legacy JSON still present: %v", err)
	}
}

func TestOpenChunkIndexDiscardsUnreadableLegacyJSON(t *testing.T) {
	dir := t.TempDir()
	legacy := filepath.Join(dir, "chunk-index.json")
	if err := os.WriteFile(legacy, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	ix, err := OpenChunkIndex(filepath.Join(dir, "chunk-index.db"), legacy, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer ix.Close()
	if n, _ := ix.Len(); n != 0 {
		t.Fatalf("Len = %d, want an empty index", n)
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Fatalf("unreadable legacy JSON still present: %v", err)
	}
}

func TestOpenChunkIndexReplacesCorruptFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "chunk-index.db")
	if err := os.WriteFile(path, []byte("this is not a bbolt file, but it is long enough to have meta pages"), 0o600); err != nil {
		t.Fatal(err)
	}
	ix, err := OpenChunkIndex(path, "", zap.NewNop())
	if err != nil {
		t.Fatalf("a corrupt index must be replaced, got %v", err)
	}
	defer ix.Close()
	if err := ix.AddLayer(digest.FromString("x").String(), []chunk.Ref{{Hash: [32]byte{1}, Len: 1}}); err != nil {
		t.Fatal(err)
	}
	aside, _ := filepath.Glob(path + ".corrupt-*")
	if len(aside) != 1 {
		t.Fatalf("corrupt file not moved aside: %v", aside)
	}
}

func TestDisabledChunkIndexHoldsNothing(t *testing.T) {
	var ix ChunkIndex // what NewClient falls back to when the index file cannot be opened
	blob := digest.FromString("layer").String()
	if err := ix.AddLayer(blob, []chunk.Ref{{Hash: [32]byte{1}, Len: 1}}); err != nil {
		t.Fatal(err)
	}
	if _, ok := ix.Has([32]byte{1}); ok {
		t.Fatal("a disabled index must report every chunk missing")
	}
	if err := ix.Drop(blob); err != nil {
		t.Fatal(err)
	}
	if blobs, err := ix.Blobs(); err != nil || len(blobs) != 0 {
		t.Fatalf("Blobs = %v, %v", blobs, err)
	}
	if err := ix.Close(); err != nil {
		t.Fatal(err)
	}
}
