package containerd

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"sync"

	"github.com/containerd/errdefs"
	digest "github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"go.uber.org/zap"

	"github.com/wendylabsinc/wendy/go/internal/shared/chunk"
)

// staging holds chunk bytes received via StageChunk until the next
// AssembleLayerFromChunks consumes them. Process-lifetime, in-memory.
type staging struct {
	mu sync.Mutex
	m  map[[32]byte][]byte
}

func newStaging() *staging { return &staging{m: make(map[[32]byte][]byte)} }

// reconstruct concatenates chunk bytes in order. fetch returns the bytes for a
// hash (from staging or an indexed blob range). Verifies each chunk's hash.
func reconstruct(order [][32]byte, fetch func([32]byte) ([]byte, error)) ([]byte, error) {
	var buf bytes.Buffer
	for i, h := range order {
		b, err := fetch(h)
		if err != nil {
			return nil, fmt.Errorf("chunk %d: %w", i, err)
		}
		if b == nil {
			return nil, fmt.Errorf("chunk %d (%x) unavailable", i, h)
		}
		if sha256.Sum256(b) != h {
			return nil, fmt.Errorf("chunk %d hash mismatch", i)
		}
		buf.Write(b)
	}
	return buf.Bytes(), nil
}

func (c *Client) MissingChunks(_ context.Context, hashes [][32]byte) ([][32]byte, error) {
	missing := c.chunkIndex.Missing(hashes)
	// Exclude any already staged this session.
	c.staging.mu.Lock()
	defer c.staging.mu.Unlock()
	var out [][32]byte
	for _, h := range missing {
		if _, ok := c.staging.m[h]; !ok {
			out = append(out, h)
		}
	}
	return out, nil
}

func (c *Client) StageChunk(_ context.Context, h [32]byte, data []byte) error {
	if sha256.Sum256(data) != h {
		return fmt.Errorf("staged chunk hash mismatch")
	}
	c.staging.mu.Lock()
	defer c.staging.mu.Unlock()
	c.staging.m[h] = data
	return nil
}

// readIndexedChunk reads a chunk's bytes from the uncompressed blob it was
// indexed into. Returns (nil,nil) when the blob is gone so the caller can treat
// it as a miss and prune the stale entry.
func (c *Client) readIndexedChunk(ctx context.Context, loc chunkLoc) ([]byte, error) {
	cs := c.client.ContentStore()
	dgst, err := digest.Parse(loc.Blob)
	if err != nil {
		return nil, err
	}
	ra, err := cs.ReaderAt(ctx, ocispec.Descriptor{Digest: dgst})
	if err != nil {
		if errdefs.IsNotFound(err) {
			c.chunkIndex.Drop(loc.Blob)
			return nil, nil
		}
		return nil, err
	}
	defer ra.Close()
	b := make([]byte, loc.Len)
	if _, err := ra.ReadAt(b, int64(loc.Offset)); err != nil {
		return nil, err
	}
	return b, nil
}

func (c *Client) AssembleLayerFromChunks(ctx context.Context, diffID string, hashes [][32]byte) error {
	nsCtx := c.withNamespace(ctx)

	// Fast path: if the (uncompressed) layer blob already exists in the content
	// store, it was reassembled and indexed on a previous deploy. Skip the
	// expensive reconstruct + re-hash + re-chunk + index-save entirely — for an
	// unchanged layer this avoids reading and re-chunking the full layer on
	// every deploy, which dominates redeploy latency for large base images.
	if dgst, err := digest.Parse(diffID); err == nil {
		if _, err := c.client.ContentStore().Info(nsCtx, dgst); err == nil {
			return nil
		}
	}

	fetch := func(h [32]byte) ([]byte, error) {
		c.staging.mu.Lock()
		b, ok := c.staging.m[h]
		c.staging.mu.Unlock()
		if ok {
			return b, nil
		}
		if loc, ok := c.chunkIndex.Has(h); ok {
			return c.readIndexedChunk(nsCtx, loc)
		}
		return nil, nil
	}

	full, err := reconstruct(hashes, fetch)
	if err != nil {
		return err
	}

	// digest == diff_id for uncompressed layers; verify before writing.
	if got := digest.FromBytes(full).String(); got != diffID {
		return fmt.Errorf("reassembled layer digest %s != diff_id %s", got, diffID)
	}

	if err := c.WriteLayer(nsCtx, diffID, bytes.NewReader(full), int64(len(full))); err != nil {
		return err
	}

	// Re-chunk the canonical bytes so the index references the freshly written
	// blob (offsets are relative to this blob).
	refs, err := chunk.Chunk(bytes.NewReader(full))
	if err != nil {
		return err
	}
	c.chunkIndex.AddLayer(diffID, refs)
	if err := c.chunkIndex.Save(); err != nil {
		c.logger.Warn("failed to persist chunk index", zap.Error(err))
	}

	// Release staged chunks now embedded in the blob.
	c.staging.mu.Lock()
	for _, h := range hashes {
		delete(c.staging.m, h)
	}
	c.staging.mu.Unlock()

	return nil
}
