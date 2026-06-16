package commands

import (
	"bytes"
	"context"

	"github.com/wendylabsinc/wendy/go/internal/shared/chunk"
	agentpb "github.com/wendylabsinc/wendy/go/proto/gen/agentpb"
)

// pushLayersByChunks implements chunk-diff layer push for a set of uncompressed
// OCI layers. For each layer it:
//  1. CDC-chunks the raw tar bytes.
//  2. Queries the device for which chunk hashes are missing.
//  3. Streams only the missing chunk bytes via WriteChunks.
//  4. Returns one RunContainerLayerHeader per layer (COMPRESSION_NONE, carrying
//     the full ordered chunk manifest).
func pushLayersByChunks(ctx context.Context, cs agentpb.WendyContainerServiceClient, layers []localLayer) ([]*agentpb.RunContainerLayerHeader, error) {
	headers := make([]*agentpb.RunContainerLayerHeader, 0, len(layers))

	for _, l := range layers {
		refs, err := chunk.Chunk(bytes.NewReader(l.Tar))
		if err != nil {
			return nil, err
		}

		// Build the ordered hash manifest and the flat [][]byte for QueryChunks.
		allHashes := make([][]byte, len(refs))
		for i, r := range refs {
			h := r.Hash // copy to avoid aliasing the loop variable
			allHashes[i] = h[:]
		}

		qresp, err := cs.QueryChunks(ctx, &agentpb.QueryChunksRequest{ChunkHashes: allHashes})
		if err != nil {
			return nil, err
		}

		missing := make(map[[32]byte]bool, len(qresp.GetMissingHashes()))
		for _, hb := range qresp.GetMissingHashes() {
			var h [32]byte
			copy(h[:], hb)
			missing[h] = true
		}

		wc, err := cs.WriteChunks(ctx)
		if err != nil {
			return nil, err
		}
		for _, r := range refs {
			if !missing[r.Hash] {
				continue
			}
			hb := r.Hash // copy
			if err := wc.Send(&agentpb.WriteChunksRequest{
				Hash: hb[:],
				Data: l.Tar[r.Offset : r.Offset+r.Len],
			}); err != nil {
				return nil, err
			}
		}
		if _, err := wc.CloseAndRecv(); err != nil {
			return nil, err
		}

		headers = append(headers, &agentpb.RunContainerLayerHeader{
			Digest:      l.DiffID,
			DiffId:      l.DiffID,
			Size:        int64(len(l.Tar)),
			Compression: agentpb.RunContainerLayerHeader_COMPRESSION_NONE,
			ChunkHashes: allHashes,
		})
	}

	return headers, nil
}
