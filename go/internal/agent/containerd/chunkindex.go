package containerd

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	digest "github.com/opencontainers/go-digest"
	bolt "go.etcd.io/bbolt"
	berrors "go.etcd.io/bbolt/errors"
	"go.uber.org/zap"

	"github.com/wendylabsinc/wendy/go/internal/shared/chunk"
)

const (
	// defaultChunkIndexPath is the persistent chunk index.
	defaultChunkIndexPath = "/var/lib/wendy/chunk-index.db"
	// legacyChunkIndexPath is the JSON index agents before WDY-3212 rewrote in
	// full on every new layer. It is imported once, then removed.
	legacyChunkIndexPath = "/var/lib/wendy/chunk-index.json"
	// chunkIndexTxEntries bounds one write transaction, so indexing a
	// multi-GB layer never holds a whole huge insert's dirty pages in memory.
	chunkIndexTxEntries = 16384
	// chunkLocSize is an encoded chunkLoc: blob digest (32) | offset (8) | length (8).
	chunkLocSize = 48
)

var (
	// bucketChunks maps a chunk hash to the blob range holding its bytes.
	bucketChunks = []byte("chunks")
	// bucketBlobChunks has one empty-valued key per indexed chunk, blob digest
	// (32 bytes) followed by chunk hash (32 bytes), so dropping a blob costs
	// O(its chunks) instead of a scan of the whole index.
	bucketBlobChunks = []byte("blob-chunks")
	// bucketBlobs is the set of indexed blob digests, for reconciliation.
	bucketBlobs = []byte("blobs")
)

type chunkLoc struct {
	Blob   string `json:"blob"`
	Offset uint64 `json:"offset"`
	Len    uint64 `json:"len"`
}

// ChunkIndex maps content-defined chunk hashes to byte ranges inside
// uncompressed Wendy layer blobs already in the containerd content store.
//
// It is a cache of what the content store holds: losing it costs re-sent
// chunks, never correctness. Updates touch only the entries they change, so
// indexing a layer costs O(chunks in that layer) rather than a rewrite of every
// chunk ever indexed, which grew every deploy's cost with device history
// (WDY-3212).
//
// The zero ChunkIndex is a disabled index: it holds nothing and records
// nothing, so every chunk is reported missing and re-sent.
type ChunkIndex struct {
	db *bolt.DB
}

// OpenChunkIndex opens the index at path, creating it if needed. A legacy JSON
// index at legacyPath (empty to skip) is imported once and then removed. A file
// bbolt cannot open is moved aside and replaced by an empty index.
func OpenChunkIndex(path, legacyPath string, logger *zap.Logger) (*ChunkIndex, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	db, err := openChunkIndexDB(path)
	if err != nil {
		if errors.Is(err, berrors.ErrTimeout) {
			// Another process holds the lock; moving the file aside would
			// pull it out from under that process.
			return nil, fmt.Errorf("opening chunk index %s: %w", path, err)
		}
		aside := fmt.Sprintf("%s.corrupt-%d", path, time.Now().Unix())
		logger.Warn("Chunk index unreadable; starting an empty one",
			zap.String("path", path), zap.String("moved_to", aside), zap.Error(err))
		if rerr := os.Rename(path, aside); rerr != nil {
			return nil, fmt.Errorf("opening chunk index %s: %w (moving it aside: %v)", path, err, rerr)
		}
		if db, err = openChunkIndexDB(path); err != nil {
			return nil, fmt.Errorf("creating chunk index %s: %w", path, err)
		}
	}
	ix := &ChunkIndex{db: db}
	if legacyPath != "" {
		ix.importLegacyJSON(legacyPath, logger)
	}
	return ix, nil
}

func openChunkIndexDB(path string) (*bolt.DB, error) {
	db, err := bolt.Open(path, 0o600, &bolt.Options{Timeout: time.Second})
	if err != nil {
		return nil, err
	}
	if err := db.Update(func(tx *bolt.Tx) error {
		for _, name := range [][]byte{bucketChunks, bucketBlobChunks, bucketBlobs} {
			if _, err := tx.CreateBucketIfNotExists(name); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

// Close releases the index file.
func (ix *ChunkIndex) Close() error {
	if ix.db == nil {
		return nil
	}
	return ix.db.Close()
}

// Has reports where a chunk's bytes live.
func (ix *ChunkIndex) Has(h [32]byte) (chunkLoc, bool) {
	locs, found, err := ix.Lookup([][32]byte{h})
	if err != nil || !found[0] {
		return chunkLoc{}, false
	}
	return locs[0], true
}

// Lookup resolves many hashes in one read transaction. found[i] reports whether
// hashes[i] is indexed, and locs[i] is its location when it is.
func (ix *ChunkIndex) Lookup(hashes [][32]byte) (locs []chunkLoc, found []bool, err error) {
	locs = make([]chunkLoc, len(hashes))
	found = make([]bool, len(hashes))
	if ix.db == nil {
		return locs, found, nil
	}
	err = ix.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketChunks)
		for i, h := range hashes {
			if v := b.Get(h[:]); len(v) == chunkLocSize {
				locs[i], found[i] = decodeChunkLoc(v), true
			}
		}
		return nil
	})
	return locs, found, err
}

// AddLayer records refs as byte ranges of blobDigest. A hash already indexed in
// another blob is re-pointed here: the newest blob is the one most likely to
// outlive a prune.
func (ix *ChunkIndex) AddLayer(blobDigest string, refs []chunk.Ref) error {
	if ix.db == nil {
		return nil
	}
	blob, err := blobKey(blobDigest)
	if err != nil {
		return err
	}
	for start := 0; ; start += chunkIndexTxEntries {
		end := min(start+chunkIndexTxEntries, len(refs))
		if err := ix.db.Update(func(tx *bolt.Tx) error {
			if err := tx.Bucket(bucketBlobs).Put(blob[:], nil); err != nil {
				return err
			}
			chunks, pairs := tx.Bucket(bucketChunks), tx.Bucket(bucketBlobChunks)
			for _, r := range refs[start:end] {
				if err := chunks.Put(r.Hash[:], encodeChunkLoc(blob, r.Offset, r.Len)); err != nil {
					return err
				}
				if err := pairs.Put(blobChunkKey(blob, r.Hash), nil); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			return err
		}
		if end == len(refs) {
			return nil
		}
	}
}

// Drop removes every entry recorded for blobDigest. A hash a later blob
// re-indexed keeps pointing at that later blob.
func (ix *ChunkIndex) Drop(blobDigest string) error {
	if ix.db == nil {
		return nil
	}
	blob, err := blobKey(blobDigest)
	if err != nil {
		return nil // a digest AddLayer rejects was never indexed
	}
	return ix.db.Update(func(tx *bolt.Tx) error {
		chunks, pairs := tx.Bucket(bucketChunks), tx.Bucket(bucketBlobChunks)
		var keys [][]byte
		c := pairs.Cursor()
		for k, _ := c.Seek(blob[:]); k != nil && bytes.HasPrefix(k, blob[:]); k, _ = c.Next() {
			keys = append(keys, bytes.Clone(k))
		}
		for _, k := range keys {
			hash := k[len(blob):]
			if v := chunks.Get(hash); len(v) == chunkLocSize && bytes.Equal(v[:len(blob)], blob[:]) {
				if err := chunks.Delete(hash); err != nil {
					return err
				}
			}
			if err := pairs.Delete(k); err != nil {
				return err
			}
		}
		return tx.Bucket(bucketBlobs).Delete(blob[:])
	})
}

// Blobs lists every indexed blob digest.
func (ix *ChunkIndex) Blobs() ([]string, error) {
	if ix.db == nil {
		return nil, nil
	}
	var out []string
	err := ix.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketBlobs).ForEach(func(k, _ []byte) error {
			out = append(out, "sha256:"+hex.EncodeToString(k))
			return nil
		})
	})
	return out, err
}

// Len is the number of indexed chunk hashes.
func (ix *ChunkIndex) Len() (int, error) {
	if ix.db == nil {
		return 0, nil
	}
	var n int
	err := ix.db.View(func(tx *bolt.Tx) error {
		n = tx.Bucket(bucketChunks).Stats().KeyN
		return nil
	})
	return n, err
}

// importLegacyJSON moves the entries of a pre-WDY-3212 JSON index into the
// store and deletes the JSON. Unreadable content is dropped rather than
// retried: the index is a cache.
func (ix *ChunkIndex) importLegacyJSON(path string, logger *zap.Logger) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return
	}
	imported := 0
	if err == nil && len(data) > 0 {
		var entries map[string]chunkLoc
		if err = json.Unmarshal(data, &entries); err == nil {
			imported, err = ix.importEntries(entries)
		}
	}
	if err != nil {
		logger.Warn("Discarding unreadable legacy chunk index", zap.String("path", path), zap.Error(err))
	} else {
		logger.Info("Migrated legacy chunk index", zap.String("path", path), zap.Int("entries", imported))
	}
	for _, p := range []string{path, path + ".tmp"} {
		if rerr := os.Remove(p); rerr != nil && !os.IsNotExist(rerr) {
			logger.Warn("Could not remove legacy chunk index", zap.String("path", p), zap.Error(rerr))
		}
	}
}

func (ix *ChunkIndex) importEntries(entries map[string]chunkLoc) (int, error) {
	byBlob := make(map[string][]chunk.Ref)
	for key, loc := range entries {
		raw, err := hex.DecodeString(key)
		if err != nil || len(raw) != 32 {
			continue
		}
		var h [32]byte
		copy(h[:], raw)
		byBlob[loc.Blob] = append(byBlob[loc.Blob], chunk.Ref{Hash: h, Offset: loc.Offset, Len: loc.Len})
	}
	imported := 0
	for blob, refs := range byBlob {
		if _, err := blobKey(blob); err != nil {
			continue // not a digest this index can hold; those chunks are re-sent when needed
		}
		if err := ix.AddLayer(blob, refs); err != nil {
			return imported, err
		}
		imported += len(refs)
	}
	return imported, nil
}

// blobKey is the raw 32-byte sha256 of a "sha256:<hex>" blob digest.
func blobKey(blob string) ([32]byte, error) {
	var k [32]byte
	d, err := digest.Parse(blob)
	if err != nil {
		return k, err
	}
	if d.Algorithm() != digest.SHA256 {
		return k, fmt.Errorf("chunk index holds sha256 blobs only, got %s", d.Algorithm())
	}
	_, err = hex.Decode(k[:], []byte(d.Encoded()))
	return k, err
}

func blobChunkKey(blob, hash [32]byte) []byte {
	k := make([]byte, 0, 64)
	k = append(k, blob[:]...)
	return append(k, hash[:]...)
}

func encodeChunkLoc(blob [32]byte, offset, length uint64) []byte {
	v := make([]byte, chunkLocSize)
	copy(v, blob[:])
	binary.BigEndian.PutUint64(v[32:40], offset)
	binary.BigEndian.PutUint64(v[40:48], length)
	return v
}

func decodeChunkLoc(v []byte) chunkLoc {
	return chunkLoc{
		Blob:   "sha256:" + hex.EncodeToString(v[:32]),
		Offset: binary.BigEndian.Uint64(v[32:40]),
		Len:    binary.BigEndian.Uint64(v[40:48]),
	}
}
