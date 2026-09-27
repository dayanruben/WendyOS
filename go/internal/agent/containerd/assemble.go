package containerd

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/containerd/containerd/v2/core/content"
	"github.com/containerd/errdefs"
	digest "github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"go.uber.org/zap"

	"github.com/wendylabsinc/wendy/go/internal/shared/chunk"
)

const (
	// maxSegmentBytes bounds one read of a layer's bytes and one write to the
	// content store. It matches the containerd proxy writer's own 8 MiB message
	// split, so a segment costs one Write round trip rather than the eight
	// 1 MiB round trips of content.WriteBlob's copy loop.
	maxSegmentBytes = 8 << 20
	// assemblyReadAhead is how many verified segments may wait for the writer,
	// so reading and hashing the next segments overlaps the current write.
	assemblyReadAhead = 4
)

// assemblySegment is a run of a layer's chunks read with one call: consecutive
// staged chunk files (blob == ""), or consecutive chunks that sit back to back
// in the same indexed blob.
type assemblySegment struct {
	blob   string
	offset uint64 // start of the run within blob
	size   uint64
	chunks []assemblyChunk
}

type assemblyChunk struct {
	hash [32]byte
	len  uint64
}

// planAssembly resolves where every chunk of a layer lives and groups the
// chunks into segments. The reused chunks of a rebuilt dependency layer are
// almost always contiguous in their previous blob, so ~85% reuse of a 330 MB
// layer becomes a few dozen large reads instead of two containerd RPCs per
// 64 KiB chunk (WDY-3213). It also returns each chunk's range in the blob being
// assembled, for the index, and that blob's size.
func (c *Client) planAssembly(hashes [][32]byte) ([]assemblySegment, []chunk.Ref, int64, error) {
	stagedLen := make([]int64, len(hashes))
	var unstaged [][32]byte
	for i, h := range hashes {
		if n, ok := c.staging.statLen(h); ok {
			stagedLen[i] = n
		} else {
			stagedLen[i] = -1
			unstaged = append(unstaged, h)
		}
	}
	locs, found, err := c.chunkIndex.Lookup(unstaged)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("reading chunk index: %w", err)
	}

	var (
		segs  []assemblySegment
		refs  = make([]chunk.Ref, len(hashes))
		total int64
		next  int // position in unstaged, locs and found
	)
	for i, h := range hashes {
		var (
			n    uint64
			blob string
			off  uint64
		)
		if stagedLen[i] >= 0 {
			n = uint64(stagedLen[i])
		} else {
			if !found[next] {
				return nil, nil, 0, fmt.Errorf("chunk %d (%x) unavailable", i, h)
			}
			loc := locs[next]
			next++
			n, blob, off = loc.Len, loc.Blob, loc.Offset
		}
		last := len(segs) - 1
		if last >= 0 && segs[last].blob == blob && segs[last].size+n <= maxSegmentBytes &&
			(blob == "" || segs[last].offset+segs[last].size == off) {
			segs[last].chunks = append(segs[last].chunks, assemblyChunk{hash: h, len: n})
			segs[last].size += n
		} else {
			segs = append(segs, assemblySegment{blob: blob, offset: off, size: n, chunks: []assemblyChunk{{hash: h, len: n}}})
		}
		refs[i] = chunk.Ref{Hash: h, Offset: uint64(total), Len: n}
		total += int64(n)
	}
	return segs, refs, total, nil
}

// segmentData is one segment's verified bytes, or the error that ended reading.
type segmentData struct {
	data []byte
	err  error
}

// readSegments reads segs in order on its own goroutine, verifies every chunk's
// SHA-256, and delivers each segment's bytes while staying up to
// assemblyReadAhead segments ahead of the consumer. The first skip bytes of the
// layer are not delivered: a resumed ingest already holds them. The channel
// closes after the last segment, after an error, or when ctx ends.
func (c *Client) readSegments(ctx context.Context, segs []assemblySegment, skip int64) <-chan segmentData {
	out := make(chan segmentData, assemblyReadAhead)
	go func() {
		defer close(out)
		r := segmentReader{c: c, ctx: ctx, readers: map[string]content.ReaderAt{}}
		defer r.close()
		var pos int64
		for _, seg := range segs {
			start := pos
			pos += int64(seg.size)
			if pos <= skip {
				continue
			}
			data, err := r.read(seg)
			if err == nil && skip > start {
				data = data[skip-start:]
			}
			select {
			case out <- segmentData{data: data, err: err}:
			case <-ctx.Done():
				return
			}
			if err != nil {
				return
			}
		}
	}()
	return out
}

// segmentReader reads segments, holding one ReaderAt per source blob for the
// whole assembly instead of opening one per chunk.
type segmentReader struct {
	c       *Client
	ctx     context.Context
	readers map[string]content.ReaderAt
}

func (r *segmentReader) close() {
	for _, ra := range r.readers {
		ra.Close()
	}
}

// read returns seg's bytes once every chunk in it matches its hash.
func (r *segmentReader) read(seg assemblySegment) ([]byte, error) {
	data := make([]byte, seg.size)
	if seg.blob != "" {
		if err := r.readBlob(seg.blob, seg.offset, data); err != nil {
			return nil, err
		}
	} else {
		var off uint64
		for _, ch := range seg.chunks {
			if err := r.readStaged(ch, data[off:off+ch.len]); err != nil {
				return nil, err
			}
			off += ch.len
		}
	}
	var off uint64
	for _, ch := range seg.chunks {
		if sha256.Sum256(data[off:off+ch.len]) != ch.hash {
			return nil, fmt.Errorf("chunk %x hash mismatch", ch.hash)
		}
		off += ch.len
	}
	return data, nil
}

// readStaged fills dst with a staged chunk. When another layer's assembly has
// consumed the file since planning, the chunk is read from the blob that
// assembly indexed it into — the fallback the per-chunk reader always had.
func (r *segmentReader) readStaged(ch assemblyChunk, dst []byte) error {
	err := r.c.staging.readInto(ch.hash, dst)
	if !os.IsNotExist(err) {
		return err
	}
	loc, ok := r.c.chunkIndex.Has(ch.hash)
	if !ok || loc.Len != ch.len {
		return fmt.Errorf("chunk %x unavailable", ch.hash)
	}
	return r.readBlob(loc.Blob, loc.Offset, dst)
}

// readBlob fills dst from blob at off.
func (r *segmentReader) readBlob(blob string, off uint64, dst []byte) error {
	ra, ok := r.readers[blob]
	if !ok {
		dgst, err := digest.Parse(blob)
		if err != nil {
			return err
		}
		ra, err = r.c.client.ContentStore().ReaderAt(r.ctx, ocispec.Descriptor{Digest: dgst})
		if err != nil {
			if errdefs.IsNotFound(err) {
				// The index outlived the blob. Forget it, so the next
				// QueryChunks asks the CLI for these chunks again.
				if derr := r.c.chunkIndex.Drop(blob); derr != nil {
					r.c.logger.Warn("Dropping stale chunk-index entries failed", zap.String("blob", blob), zap.Error(derr))
				}
			}
			return fmt.Errorf("opening indexed blob %s: %w", blob, err)
		}
		r.readers[blob] = ra
	}
	if err := readFullAt(ra, dst, int64(off)); err != nil {
		return fmt.Errorf("reading %d bytes at %d of %s: %w", len(dst), off, blob, err)
	}
	return nil
}

// readFullAt fills dst from ra at off.
func readFullAt(ra content.ReaderAt, dst []byte, off int64) error {
	n, err := ra.ReadAt(dst, off)
	if n == len(dst) {
		return nil // a full read may still report io.EOF at the blob's end
	}
	if err == nil {
		err = io.ErrUnexpectedEOF
	}
	return err
}

// readInto fills dst with the staged chunk h. A chunk that is not staged
// returns an error for which os.IsNotExist is true.
func (s *staging) readInto(h [32]byte, dst []byte) error {
	f, err := os.Open(s.path(h))
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := io.ReadFull(f, dst); err != nil {
		return fmt.Errorf("reading staged chunk %x: %w", h, err)
	}
	return nil
}

// writeAssembledLayer writes segs into the content store as diffID, one Write
// per verified segment instead of content.WriteBlob's 1 MiB copy loop. Like
// content.Copy, it resumes a partial ingest by skipping the bytes the ingest
// already holds, so concurrent assemblies of the same layer still serialize on
// containerd's per-ref lock rather than clobbering each other.
//
// verified reports that every chunk of the manifest was hash-checked against
// the bytes now committed as diffID: the reader checked each chunk it wrote,
// and checkResumedPrefix read back and checked the prefix the reader skipped.
// Only then may the manifest's ranges be indexed. It is false, with a nil
// error, whenever this call committed nothing it checked:
//   - OpenWriter or Commit reports the blob already exists. A concurrent
//     assembly committed it first, and over the proxy the content server
//     answers a Commit with AlreadyExists before it hashes a byte of this
//     ingest, so the outcome says nothing about this manifest.
//   - The resumed prefix read back from the blob does not match the manifest,
//     or could not be read.
//
// The blob itself is valid either way: its digest was verified when it was
// committed. Only the manifest is unproven.
func (c *Client) writeAssembledLayer(ctx context.Context, diffID string, size int64, segs []assemblySegment) (verified bool, err error) {
	dgst, err := digest.Parse(diffID)
	if err != nil {
		return false, fmt.Errorf("parsing digest %q: %w", diffID, err)
	}
	w, err := content.OpenWriter(ctx, c.client.ContentStore(),
		content.WithRef(diffID),
		content.WithDescriptor(ocispec.Descriptor{Digest: dgst, Size: size}))
	if err != nil {
		if errdefs.IsAlreadyExists(err) {
			c.logger.Debug("Layer already exists in content store", zap.String("digest", diffID))
			return false, nil
		}
		return false, fmt.Errorf("opening layer %s for writing: %w", diffID, err)
	}
	defer w.Close()
	st, err := w.Status()
	if err != nil {
		return false, fmt.Errorf("checking layer %s ingest: %w", diffID, err)
	}

	readCtx, cancel := context.WithCancel(ctx)
	defer cancel() // stops the reader if a write fails
	for seg := range c.readSegments(readCtx, segs, st.Offset) {
		if seg.err != nil {
			return false, fmt.Errorf("reassembling layer %s: %w", diffID, seg.err)
		}
		if _, err := w.Write(seg.data); err != nil {
			return false, fmt.Errorf("writing layer %s: %w", diffID, err)
		}
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	labels := map[string]string{
		labelKeyGCRoot:     gcTimestamp(),
		labelKeyWendyLayer: "true",
	}
	if err := w.Commit(ctx, size, dgst, content.WithLabels(labels)); err != nil {
		if errdefs.IsAlreadyExists(err) {
			c.logger.Debug("Layer already exists in content store", zap.String("digest", diffID))
			return false, nil
		}
		return false, fmt.Errorf("committing layer %s: %w", diffID, err)
	}
	c.logger.Info("Wrote layer to content store", zap.String("digest", diffID), zap.Int64("size", size))
	if st.Offset > 0 {
		return c.checkResumedPrefix(ctx, dgst, segs, st.Offset), nil
	}
	return true, nil
}

// errPrefixMismatch marks a resumed prefix that does not hold the manifest's
// chunks.
var errPrefixMismatch = errors.New("resumed prefix does not match the manifest")

// checkResumedPrefix reports whether the first prefix bytes of the committed
// blob dgst hold the chunks segs names there. The reader skipped those bytes:
// an earlier attempt wrote them, or, under containerd's shared-content policy,
// the writer already held a blob another namespace committed. Commit vouched
// for them as part of the blob, not as this manifest's chunks. So every chunk
// that starts in the prefix is read back and hash-checked, in pieces of at most
// maxSegmentBytes. A mismatch or a failed read is logged and reported false:
// the blob is valid, but the manifest must not be indexed.
func (c *Client) checkResumedPrefix(ctx context.Context, dgst digest.Digest, segs []assemblySegment, prefix int64) bool {
	err := c.verifyPrefix(ctx, dgst, segs, prefix)
	switch {
	case err == nil:
		return true
	case errors.Is(err, errPrefixMismatch):
		c.logger.Warn("Resumed layer prefix does not match its chunk manifest; not indexing it",
			zap.String("digest", dgst.String()), zap.Int64("resumed_bytes", prefix), zap.Error(err))
	default:
		c.logger.Warn("Could not read back a resumed layer prefix; not indexing it",
			zap.String("digest", dgst.String()), zap.Int64("resumed_bytes", prefix), zap.Error(err))
	}
	return false
}

// verifyPrefix checks the chunks of segs that start in the first prefix bytes
// of the blob dgst against their hashes; see checkResumedPrefix.
func (c *Client) verifyPrefix(ctx context.Context, dgst digest.Digest, segs []assemblySegment, prefix int64) error {
	ra, err := c.client.ContentStore().ReaderAt(ctx, ocispec.Descriptor{Digest: dgst})
	if err != nil {
		return fmt.Errorf("opening committed layer: %w", err)
	}
	defer ra.Close()
	buf := make([]byte, maxSegmentBytes)

	var (
		start   int64 // offset of the pending piece in the blob
		pending []assemblyChunk
		size    uint64 // bytes pending
	)
	check := func() error {
		piece := buf[:size]
		if err := readFullAt(ra, piece, start); err != nil {
			return fmt.Errorf("reading %d bytes at %d of the committed layer: %w", size, start, err)
		}
		var off uint64
		for _, ch := range pending {
			if sha256.Sum256(piece[off:off+ch.len]) != ch.hash {
				return fmt.Errorf("%w: chunk %x at %d", errPrefixMismatch, ch.hash, start+int64(off))
			}
			off += ch.len
		}
		start += int64(size)
		pending, size = pending[:0], 0
		return nil
	}
	pos := int64(0) // offset of the next chunk in the blob
chunks:
	for _, seg := range segs {
		for _, ch := range seg.chunks {
			if pos >= prefix {
				break chunks
			}
			if size+ch.len > maxSegmentBytes {
				if err := check(); err != nil {
					return err
				}
			}
			pending = append(pending, ch)
			size += ch.len
			pos += int64(ch.len)
		}
	}
	if size == 0 {
		return nil
	}
	return check()
}
