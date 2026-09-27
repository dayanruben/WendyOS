package containerd

import (
	"fmt"

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
