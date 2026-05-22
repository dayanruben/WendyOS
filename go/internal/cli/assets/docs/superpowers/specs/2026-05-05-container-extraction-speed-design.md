# Container Extraction Speed — Design

**Date:** 2026-05-05

## Problem

Container extraction (the `UnpackImage` call that follows image assembly or registry push) is slower than necessary for two independent reasons:

1. **Serial stat round-trips on re-deploy:** `UnpackImage` checks whether each layer's chain-ID snapshot already exists by calling `sn.Stat` inline in the sequential apply loop. On a fully-cached image this serializes N metadata round-trips before discovering all layers are reused — the same latency as a first-time unpack on the stat path alone.

2. **Slow decompression on ARM64:** Layers pushed via the containerd registry HTTP API (the primary path) carry gzip compression. On ARM64 hardware (Jetson, Orin, etc.), gzip decompression is the dominant CPU cost in `DiffService().Apply`. zstd decompresses 3–5× faster on these targets and typically achieves better compression ratios.

## Scope

- `go/internal/agent/containerd/unpack.go` — parallel stat pre-check
- `go/internal/agent/containerd/client.go` — zstd media type in `AssembleImage`
- `go/proto/…/wendy_agent_v1_container_service.proto` — compression enum on `RunContainerLayerHeader`

Registry push tooling (CLI or external) is out of scope for this change; it must push zstd-compressed layers independently to benefit from the decompression speedup.

## Design

### Change 1: Parallel stat pre-check in `UnpackImage`

Split the existing single loop into two phases.

**Phase 1 — compute chain IDs + concurrent stat:**

Compute all chain IDs in a single in-memory pass (no I/O), then fan out all `sn.Stat` calls concurrently via goroutines and `sync.WaitGroup`. Collect results into an `exists []bool` slice indexed by layer position.

```
for each layer i in parallel:
    exists[i] = (sn.Stat(ctx, chainIDs[i]) == nil)
```

**Phase 2 — sequential apply using pre-computed existence:**

The apply loop is structurally unchanged — snapshots must be prepared and committed in parent-chain order. But the reuse check is now a slice lookup (`exists[i]`) instead of an RPC.

**Invariant:** If `exists[i]` is true, `exists[i-1]` is guaranteed true as well — a committed chain-ID snapshot cannot exist without its parent having been committed first. Skipping a layer is therefore safe without any gap-checking logic.

**Concurrency safety:** The existing `AlreadyExists` handling in `sn.Commit` already covers the case where a concurrent unpack races to commit the same chain ID between the pre-check and the apply. No new locking is required.

**Impact:** Fully-cached re-deploys (common in development) go from O(N) serial RPCs to a single parallel fan-out before the loop is entered. First-time unpacks see no regression — the stat calls are simply moved earlier and run faster in parallel.

### Change 2: zstd compression support in `AssembleImage`

**Context:** For images pushed via the registry HTTP API, the manifest already carries the correct media type; `DiffService().Apply` dispatches to the right decompressor automatically. No change to `UnpackImage` is required — the extraction speedup is purely a function of what compression format the layer blobs use.

For images assembled via the `AssembleImage` gRPC path, the agent sets the media type based on the `gzip bool` field of `RunContainerLayerHeader`. Extend this to support zstd.

**Proto change — add compression enum:**

```protobuf
// In RunContainerLayerHeader:
enum CompressionType {
  COMPRESSION_GZIP = 0;   // default; matches existing gzip=true behavior
  COMPRESSION_ZSTD = 1;
  COMPRESSION_NONE = 2;
}
CompressionType compression = 5;
```

Field zero (`COMPRESSION_GZIP`) is the proto default, so existing CLIs that do not send the field continue to work without any changes. The existing `gzip bool` field is kept for backward compatibility; the new `compression` field takes precedence when present and non-zero.

**`AssembleImage` change:**

Replace the `if !l.GetGzip()` branch with a switch on `compression`, falling back to the `gzip` bool for unrecognized values:

```
COMPRESSION_ZSTD → ocispec.MediaTypeImageLayerZstd
COMPRESSION_NONE → ocispec.MediaTypeImageLayer
COMPRESSION_GZIP (or unrecognized) → ocispec.MediaTypeImageLayerGzip
                                       (unless gzip=false → MediaTypeImageLayer)
```

`ocispec.MediaTypeImageLayerZstd` is already defined in `opencontainers/image-spec`.

## Data flow (unchanged)

```
registry push (zstd layers)
        ↓
containerd content store  ←→  AssembleImage (gRPC path, zstd via new proto field)
        ↓
UnpackImage
  Phase 1: compute chain IDs → parallel sn.Stat fan-out → exists[]
  Phase 2: sequential Prepare → DiffService.Apply (zstd or gzip) → Commit
        ↓
container rootfs snapshot ready
```

## Error handling

- **Stat errors in Phase 1:** Any error other than `NotFound` should be treated as a transient failure and the layer treated as not-yet-existing (conservative: will attempt to apply the layer). This matches current behavior.
- **`AlreadyExists` on Commit:** Already handled; no change.
- **zstd blob with wrong media type:** Containerd will surface a decompression error from `DiffService.Apply`. The caller sees a wrapped error from `UnpackImage` as before.

## Testing

- Unit test for the parallel stat pre-check: mock snapshotter returns exists for some layers, absent for others; verify correct layers are applied and reused.
- Unit test for `AssembleImage` with `COMPRESSION_ZSTD`: verify `MediaTypeImageLayerZstd` is written to the manifest.
- Backward-compat test: `RunContainerLayerHeader` with only `gzip=true` (no `compression` field) still produces `MediaTypeImageLayerGzip`.
- Integration test: push a zstd-compressed image to the local registry, create a container, verify it starts.
