package containerd

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/containerd/containerd/v2/core/content"
	"github.com/containerd/containerd/v2/core/images"
	"github.com/containerd/containerd/v2/core/leases"
	"github.com/containerd/containerd/v2/core/snapshots"
	"github.com/containerd/errdefs"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"go.uber.org/zap"
)

// unpackLeaseExpiration bounds how long the unpack lease keeps freshly created
// snapshots and content alive. It must be long enough to apply the largest
// expected image, but short enough that orphaned leases from a crashed agent
// don't accumulate indefinitely.
const unpackLeaseExpiration = 30 * time.Minute

// UnpackProgress reports progress during the image unpack operation.
type UnpackProgress struct {
	// Phase is one of "start", "layer", "complete".
	Phase string
	// LayerIndex is the zero-based index of the current layer being unpacked.
	LayerIndex int
	// TotalLayers is the total number of layers in the image.
	TotalLayers int
	// LayerSize is the compressed size of the current layer in bytes.
	LayerSize int64
	// Reused indicates whether the layer snapshot already existed and was reused.
	Reused bool
}

// UnpackImage unpacks an image's layers into the snapshotter so that the
// resulting chain-ID snapshots are present for a subsequent
// `WithNewSnapshot` call to build a container rootfs from. It computes chain
// IDs for each layer and creates snapshots incrementally, reusing existing
// snapshots when possible.
//
// The progress callback, if non-nil, is invoked for each phase of the unpack
// operation to allow callers to report progress upstream.
//
// The unpack runs inside a containerd lease and tags each committed snapshot
// with a `containerd.io/gc.root` label. Without this, containerd's metadata
// GC can reap a chain-ID snapshot between iterations of the loop — the
// snapshot has no inbound references until the next layer is committed on
// top of it — which surfaces as a random-layer "parent snapshot does not
// exist" failure during `Prepare`.
func (c *Client) UnpackImage(ctx context.Context, imageName string, progress func(UnpackProgress)) error {
	ctx = c.withNamespace(ctx)

	ctx, doneLease, err := c.client.WithLease(ctx, leases.WithExpiration(unpackLeaseExpiration))
	if err != nil {
		return fmt.Errorf("creating unpack lease: %w", err)
	}
	defer func() {
		// Release on a fresh context so a cancelled caller still tears the
		// lease down. The lease's expiration is the ultimate backstop.
		_ = doneLease(context.Background())
	}()

	cs := c.client.ContentStore()
	sn := c.client.SnapshotService("")

	img, err := c.client.GetImage(ctx, imageName)
	if err != nil {
		return fmt.Errorf("getting image %q: %w", imageName, err)
	}

	// Resolve through index if needed (platform selection).
	manifest, err := images.Manifest(ctx, cs, img.Target(), img.Platform())
	if err != nil {
		return fmt.Errorf("reading manifest for %q: %w", imageName, err)
	}

	totalLayers := len(manifest.Layers)
	if progress != nil {
		progress(UnpackProgress{Phase: "start", TotalLayers: totalLayers})
	}

	var parentChainID string
	for i, layerDesc := range manifest.Layers {
		diffID, err := layerDiffID(ctx, cs, layerDesc)
		if err != nil {
			return fmt.Errorf("getting diff ID for layer %d: %w", i, err)
		}

		chainID := computeChainID(parentChainID, diffID)

		if _, err := sn.Stat(ctx, chainID); err == nil {
			if progress != nil {
				progress(UnpackProgress{
					Phase:       "layer",
					LayerIndex:  i,
					TotalLayers: totalLayers,
					LayerSize:   layerDesc.Size,
					Reused:      true,
				})
			}
			c.logger.Debug("Reusing existing snapshot",
				zap.Int("layer", i),
				zap.String("chain_id", chainID),
			)
			parentChainID = chainID
			continue
		} else if !errdefs.IsNotFound(err) {
			return fmt.Errorf("stat snapshot %q: %w", chainID, err)
		}

		gcRootOpt := snapshots.WithLabels(map[string]string{
			labelKeyGCRoot: gcTimestamp(),
		})

		activeKey := fmt.Sprintf("extract-%s-%d", imageName, i)
		mounts, err := sn.Prepare(ctx, activeKey, parentChainID, gcRootOpt)
		if err != nil {
			if errdefs.IsAlreadyExists(err) {
				_ = sn.Remove(ctx, activeKey)
				mounts, err = sn.Prepare(ctx, activeKey, parentChainID, gcRootOpt)
			}
			if err != nil {
				return fmt.Errorf("preparing snapshot for layer %d: %w", i, err)
			}
		}

		if _, err := c.client.DiffService().Apply(ctx, layerDesc, mounts); err != nil {
			_ = sn.Remove(ctx, activeKey)
			return fmt.Errorf("applying layer %d: %w", i, err)
		}

		if err := sn.Commit(ctx, chainID, activeKey, gcRootOpt); err != nil {
			if !errdefs.IsAlreadyExists(err) {
				return fmt.Errorf("committing snapshot for layer %d: %w", i, err)
			}
			// Another process committed it concurrently; clean up our active key.
			_ = sn.Remove(ctx, activeKey)
		}

		if progress != nil {
			progress(UnpackProgress{
				Phase:       "layer",
				LayerIndex:  i,
				TotalLayers: totalLayers,
				LayerSize:   layerDesc.Size,
				Reused:      false,
			})
		}

		c.logger.Debug("Unpacked layer",
			zap.Int("layer", i),
			zap.String("chain_id", chainID),
			zap.Int64("size", layerDesc.Size),
		)

		parentChainID = chainID
	}

	if progress != nil {
		progress(UnpackProgress{Phase: "complete", TotalLayers: totalLayers})
	}

	return nil
}

// layerDiffID resolves the uncompressed diff ID for a layer descriptor.
func layerDiffID(ctx context.Context, cs content.Store, desc ocispec.Descriptor) (string, error) {
	diffID, err := images.GetDiffID(ctx, cs, desc)
	if err != nil {
		return "", err
	}
	return diffID.String(), nil
}

// readManifest reads and unmarshals an OCI manifest blob from the content store.
func readManifest(ctx context.Context, cs content.Store, desc ocispec.Descriptor) (*ocispec.Manifest, error) {
	p, err := content.ReadBlob(ctx, cs, desc)
	if err != nil {
		return nil, err
	}
	var manifest ocispec.Manifest
	if err := json.Unmarshal(p, &manifest); err != nil {
		return nil, err
	}
	return &manifest, nil
}
