package containerd

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	digest "github.com/opencontainers/go-digest"
	bolt "go.etcd.io/bbolt"
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

// bbolt page type flags (go.etcd.io/bbolt/internal/common).
const (
	boltBranchPageFlag = 0x01
	boltMetaPageFlag   = 0x04
)

// writeChunkIndexFile builds a closed index file with 4 KiB pages, as on a
// device, holding perLayer entries for each of layers blobs. Entry i of layer
// l has hash sha256("l-i").
func writeChunkIndexFile(t *testing.T, layers, perLayer int) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "chunk-index.db")
	db, err := bolt.Open(path, 0o600, &bolt.Options{PageSize: 4096})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	ix, err := OpenChunkIndex(path, "", zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	for l := range layers {
		refs := make([]chunk.Ref, perLayer)
		for i := range refs {
			refs[i] = chunk.Ref{Hash: sha256.Sum256(fmt.Appendf(nil, "%d-%d", l, i)), Offset: uint64(i), Len: 1}
		}
		if err := ix.AddLayer(digest.FromString(fmt.Sprint("layer ", l)).String(), refs); err != nil {
			t.Fatal(err)
		}
	}
	if err := ix.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

// chunkIndexMeta reads the page size, and the root-bucket and freelist page
// ids, from the newer of an index file's two meta pages. A page starts with a
// 16-byte header; the meta that follows holds the page size at byte 24, the
// root bucket's page at 32, the freelist's page at 48 and the txid at 64.
// bbolt writes them in host byte order, little-endian wherever the agent runs.
func chunkIndexMeta(t *testing.T, path string) (pageSize int, root, freelist uint64) {
	t.Helper()
	f, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	pageSize = int(binary.LittleEndian.Uint32(f[24:28]))
	newest, newestTx := 0, uint64(0)
	for i := range 2 {
		if tx := binary.LittleEndian.Uint64(f[i*pageSize+64:]); tx >= newestTx {
			newest, newestTx = i, tx
		}
	}
	meta := f[newest*pageSize:]
	return pageSize, binary.LittleEndian.Uint64(meta[32:40]), binary.LittleEndian.Uint64(meta[48:56])
}

// setChunkIndexPageFlags overwrites one page's type flags, as a torn write
// would. The meta checksums cover only the meta pages, so bbolt cannot see it.
func setChunkIndexPageFlags(t *testing.T, path string, pageSize int, pgid uint64, flags uint16) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteAt(binary.LittleEndian.AppendUint16(nil, flags), int64(pgid)*int64(pageSize)+8); err != nil {
		t.Fatal(err)
	}
}

// chunkIndexBucketPage returns the page holding the root of a bucket that is
// too big to live inline in its parent's page.
func chunkIndexBucketPage(t *testing.T, path string, bucket []byte) uint64 {
	t.Helper()
	db, err := bolt.Open(path, 0o600, &bolt.Options{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var pgid uint64
	if err := db.View(func(tx *bolt.Tx) error {
		pgid = uint64(tx.Bucket(bucket).Root())
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if pgid == 0 {
		t.Fatalf("bucket %s is inline; the test index needs more entries", bucket)
	}
	return pgid
}

// corruptChunkIndexFiles lists the files OpenChunkIndex moved aside.
func corruptChunkIndexFiles(t *testing.T, path string) []string {
	t.Helper()
	aside, err := filepath.Glob(path + ".corrupt-*")
	if err != nil {
		t.Fatal(err)
	}
	return aside
}

// TestOpenChunkIndexReplacesAFileWithACorruptPage damages a real index file the
// way a torn write or a lost tail would, where the meta checksums cannot see
// it. bbolt panics or faults on such pages rather than returning an error, so
// without recovery the agent would crash-loop on every start.
func TestOpenChunkIndexReplacesAFileWithACorruptPage(t *testing.T) {
	truncate := func(t *testing.T, path string, size int64) {
		t.Helper()
		if err := os.Truncate(path, size); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		name    string
		corrupt func(t *testing.T, path string)
	}{
		{"freelist page", func(t *testing.T, path string) {
			pageSize, _, freelist := chunkIndexMeta(t, path)
			setChunkIndexPageFlags(t, path, pageSize, freelist, boltBranchPageFlag)
		}},
		{"root bucket page", func(t *testing.T, path string) {
			pageSize, root, _ := chunkIndexMeta(t, path)
			setChunkIndexPageFlags(t, path, pageSize, root, boltMetaPageFlag)
		}},
		{"tail lost", func(t *testing.T, path string) {
			// Cut the file at the start of the OS page holding its freelist
			// page. bbolt maps the file rounded up to a power of two, so
			// reading that page faults (SIGBUS) rather than panicking: a
			// fault recover alone cannot catch.
			pageSize, _, freelist := chunkIndexMeta(t, path)
			osPage := int64(os.Getpagesize())
			size := int64(freelist) * int64(pageSize) / osPage * osPage
			if size < 2*int64(pageSize) || size&(size-1) == 0 {
				t.Fatalf("cutting the file to %d bytes would not leave its freelist page mapped past the end; change the test index", size)
			}
			truncate(t, path, size)
		}},
		{"cut short before the second meta page", func(t *testing.T, path string) {
			pageSize, _, _ := chunkIndexMeta(t, path)
			truncate(t, path, int64(pageSize))
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeChunkIndexFile(t, 2, 1000)
			tc.corrupt(t, path)

			ix, err := OpenChunkIndex(path, "", zap.NewNop())
			if err != nil {
				t.Fatalf("a corrupt index must be replaced, got %v", err)
			}
			defer ix.Close()
			if n, err := ix.Len(); err != nil || n != 0 {
				t.Fatalf("Len = %d, %v; want a new, empty index", n, err)
			}
			if err := ix.AddLayer(digest.FromString("layer after recovery").String(), []chunk.Ref{{Hash: [32]byte{7}, Len: 1}}); err != nil {
				t.Fatal(err)
			}
			if _, ok := ix.Has([32]byte{7}); !ok {
				t.Fatal("the replacement index does not record entries")
			}
			if aside := corruptChunkIndexFiles(t, path); len(aside) != 1 {
				t.Fatalf("corrupt file not moved aside: %v", aside)
			}
		})
	}
}

// TestChunkIndexDisablesItselfOnACorruptPage: a torn page inside the chunks
// bucket goes unnoticed at open, and bbolt panics on the first read of it. That
// read fails instead, the index behaves as the disabled one from then on, and
// its file is moved aside so the next start opens an empty one.
func TestChunkIndexDisablesItselfOnACorruptPage(t *testing.T) {
	path := writeChunkIndexFile(t, 2, 1000)
	pageSize, _, _ := chunkIndexMeta(t, path)
	setChunkIndexPageFlags(t, path, pageSize, chunkIndexBucketPage(t, path, bucketChunks), boltMetaPageFlag)

	ix, err := OpenChunkIndex(path, "", zap.NewNop())
	if err != nil {
		t.Fatalf("the damage is in a data page, so open should succeed: %v", err)
	}
	indexed := sha256.Sum256([]byte("0-0"))
	if _, found, err := ix.Lookup([][32]byte{indexed}); !errors.Is(err, errChunkIndexCorrupt) || found[0] {
		t.Fatalf("Lookup = found %v, err %v; want a corruption error and nothing found", found[0], err)
	}

	blob := digest.FromString("layer indexed after the failure").String()
	if err := ix.AddLayer(blob, []chunk.Ref{{Hash: [32]byte{1}, Len: 1}}); err != nil {
		t.Fatalf("AddLayer = %v; want the disabled index's silent no-op", err)
	}
	if _, found, err := ix.Lookup([][32]byte{indexed, {1}}); err != nil || found[0] || found[1] {
		t.Fatalf("Lookup = %v, %v; want nothing found and no error", found, err)
	}
	if blobs, err := ix.Blobs(); err != nil || len(blobs) != 0 {
		t.Fatalf("Blobs = %v, %v; want none", blobs, err)
	}
	if n, err := ix.Len(); err != nil || n != 0 {
		t.Fatalf("Len = %d, %v; want 0", n, err)
	}
	if err := ix.Drop(blob); err != nil {
		t.Fatal(err)
	}
	if err := ix.Close(); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("the corrupt file is still at %s: %v", path, err)
	}
	if aside := corruptChunkIndexFiles(t, path); len(aside) != 1 {
		t.Fatalf("corrupt file not moved aside: %v", aside)
	}
	next, err := OpenChunkIndex(path, "", zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	if n, err := next.Len(); err != nil || n != 0 {
		t.Fatalf("next start's index Len = %d, %v; want a new, empty index", n, err)
	}
}

// TestOpenChunkIndexKeepsAValidFileItCannotOpen: a failure that is not the
// file's fault (here EACCES; on a device ENOSPC or EIO) leaves the file in
// place, so only this start runs with the disabled index.
func TestOpenChunkIndexKeepsAValidFileItCannotOpen(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root, file permissions do not deny opening")
	}
	path := writeChunkIndexFile(t, 1, 10)
	if err := os.Chmod(path, 0o400); err != nil {
		t.Fatal(err)
	}
	if ix, err := OpenChunkIndex(path, "", zap.NewNop()); err == nil {
		ix.Close()
		t.Fatal("opening an index the agent cannot write must fail")
	}
	if aside := corruptChunkIndexFiles(t, path); len(aside) != 0 {
		t.Fatalf("a valid index was moved aside: %v", aside)
	}

	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	ix, err := OpenChunkIndex(path, "", zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer ix.Close()
	if n, err := ix.Len(); err != nil || n != 10 {
		t.Fatalf("Len = %d, %v; want the 10 entries the file held", n, err)
	}
}

func TestOpenChunkIndexKeepsOnlyTheNewestCorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "chunk-index.db")
	older := path + ".corrupt-1"
	if err := os.WriteFile(older, []byte("moved aside by an earlier start"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("this is not a bbolt file"), 0o600); err != nil {
		t.Fatal(err)
	}
	ix, err := OpenChunkIndex(path, "", zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer ix.Close()
	if aside := corruptChunkIndexFiles(t, path); len(aside) != 1 || aside[0] == older {
		t.Fatalf("corrupt files = %v; want only the one just moved aside", aside)
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
