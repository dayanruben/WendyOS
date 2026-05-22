# Container Extraction Speed Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Speed up container extraction by parallelising snapshot existence checks in `UnpackImage` and adding zstd compression support to `AssembleImage`.

**Architecture:** Two independent changes. (1) `UnpackImage` pre-computes all chain IDs then fans out `sn.Stat` calls concurrently before entering the sequential apply loop — eliminating N serial metadata round-trips on re-deploy. (2) A new `layerMediaType` helper reads the `compression` enum field added to `RunContainerLayerHeader` and selects the correct OCI media type; `AssembleImage` calls it instead of the inline `gzip bool` branch.

**Tech Stack:** Go 1.26, containerd v2 SDK (`github.com/containerd/containerd/v2`), `github.com/containerd/errdefs`, `github.com/opencontainers/image-spec v1.1.1` (provides `MediaTypeImageLayerZstd`), protobuf / `protoc`.

---

## Task 1: Extract `statLayers` helper (TDD)

**Files:**
- Modify: `go/internal/agent/containerd/unpack.go`
- Create: `go/internal/agent/containerd/unpack_test.go`

### Step 1 — Write failing tests

Create `go/internal/agent/containerd/unpack_test.go`:

```go
package containerd

import (
	"context"
	"errors"
	"testing"

	"github.com/containerd/containerd/v2/core/snapshots"
	"github.com/containerd/errdefs"
)

// mockStatter implements snapshotStatter for testing.
type mockStatter struct {
	// exists maps chain ID → true if the snapshot exists.
	exists map[string]bool
	// errs maps chain ID → error to return (overrides exists).
	errs map[string]error
}

func (m *mockStatter) Stat(_ context.Context, key string) (snapshots.Info, error) {
	if err, ok := m.errs[key]; ok {
		return snapshots.Info{}, err
	}
	if m.exists[key] {
		return snapshots.Info{Name: key}, nil
	}
	return snapshots.Info{}, errdefs.ErrNotFound
}

func TestStatLayers_AllExist(t *testing.T) {
	ids := []string{"sha256:aaa", "sha256:bbb", "sha256:ccc"}
	sn := &mockStatter{exists: map[string]bool{
		"sha256:aaa": true, "sha256:bbb": true, "sha256:ccc": true,
	}}
	got, err := statLayers(context.Background(), sn, ids)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for i, v := range got {
		if !v {
			t.Errorf("exists[%d] = false; want true", i)
		}
	}
}

func TestStatLayers_NoneExist(t *testing.T) {
	ids := []string{"sha256:aaa", "sha256:bbb"}
	sn := &mockStatter{exists: map[string]bool{}}
	got, err := statLayers(context.Background(), sn, ids)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for i, v := range got {
		if v {
			t.Errorf("exists[%d] = true; want false", i)
		}
	}
}

func TestStatLayers_Mixed(t *testing.T) {
	ids := []string{"sha256:aaa", "sha256:bbb", "sha256:ccc"}
	sn := &mockStatter{exists: map[string]bool{"sha256:bbb": true}}
	got, err := statLayers(context.Background(), sn, ids)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got[0] {
		t.Error("exists[0] should be false")
	}
	if !got[1] {
		t.Error("exists[1] should be true")
	}
	if got[2] {
		t.Error("exists[2] should be false")
	}
}

func TestStatLayers_PropagatesNonNotFoundError(t *testing.T) {
	sentinel := errors.New("storage failure")
	ids := []string{"sha256:aaa", "sha256:bbb"}
	sn := &mockStatter{errs: map[string]error{"sha256:aaa": sentinel}}
	_, err := statLayers(context.Background(), sn, ids)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("error = %v; want sentinel", err)
	}
}

func TestStatLayers_EmptyInput(t *testing.T) {
	sn := &mockStatter{}
	got, err := statLayers(context.Background(), sn, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("len(got) = %d; want 0", len(got))
	}
}
```

- [ ] **Step 2 — Run tests to confirm they fail**

```bash
cd go && go test ./internal/agent/containerd/ -run TestStatLayers -v
```

Expected: compile error — `statLayers` undefined, `snapshotStatter` undefined.

- [ ] **Step 3 — Add `snapshotStatter` interface and `statLayers` to `unpack.go`**

Add after the existing `import` block and before `UnpackImage` in `go/internal/agent/containerd/unpack.go`:

```go
// snapshotStatter is the subset of snapshots.Snapshotter used for existence checks.
type snapshotStatter interface {
	Stat(ctx context.Context, key string) (snapshots.Info, error)
}

// statLayers checks which chain-ID snapshots already exist by fanning out
// sn.Stat calls concurrently. Returns a bool slice indexed by layer position.
// Any non-NotFound error from any goroutine is returned (first one wins).
func statLayers(ctx context.Context, sn snapshotStatter, chainIDs []string) ([]bool, error) {
	exists := make([]bool, len(chainIDs))
	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		firstErr error
	)
	for i, id := range chainIDs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := sn.Stat(ctx, id)
			if err == nil {
				exists[i] = true
			} else if !errdefs.IsNotFound(err) {
				mu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	return exists, firstErr
}
```

Also add `"sync"` to the imports in `unpack.go` (it already imports several packages — add `"sync"` to the list).

- [ ] **Step 4 — Run tests to confirm they pass**

```bash
cd go && go test ./internal/agent/containerd/ -run TestStatLayers -v
```

Expected: all 5 `TestStatLayers_*` tests PASS.

- [ ] **Step 5 — Commit**

```bash
cd go && git add internal/agent/containerd/unpack.go internal/agent/containerd/unpack_test.go
git commit -m "feat: add statLayers helper for parallel snapshot existence checks"
```

---

## Task 2: Wire parallel stat pre-check into `UnpackImage`

**Files:**
- Modify: `go/internal/agent/containerd/unpack.go`

- [ ] **Step 1 — Replace the inline stat + chain-ID logic in `UnpackImage`**

In `go/internal/agent/containerd/unpack.go`, replace the section starting at `var parentChainID string` through the entire for-loop body with the code below. The `// Read all diff IDs…` block above stays unchanged.

```go
totalLayers := len(manifest.Layers)
if progress != nil {
	progress(UnpackProgress{Phase: "start", TotalLayers: totalLayers})
}

// Pre-compute all chain IDs in a single pass, then check existence in parallel.
chainIDs := make([]string, len(diffIDs))
parent := ""
for i, diffID := range diffIDs {
	chainIDs[i] = computeChainID(parent, diffID.String())
	parent = chainIDs[i]
}

exists, err := statLayers(ctx, sn, chainIDs)
if err != nil {
	return fmt.Errorf("pre-checking layer snapshots: %w", err)
}

var parentChainID string
for i, layerDesc := range manifest.Layers {
	chainID := chainIDs[i]

	if exists[i] {
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
	}

	// Unique per-attempt active key so concurrent unpacks of the same
	// image (or a stale key from a crashed prior attempt) can't collide
	// on the AlreadyExists path and clobber each other's in-progress
	// snapshot. The lease pins the active snapshot during this loop
	// iteration; only the committed chain-ID snapshot needs gc.root
	// to survive lease release.
	activeKey := fmt.Sprintf("extract-%s-%d-%s", imageName, i, uuid.NewString())
	mounts, err := sn.Prepare(ctx, activeKey, parentChainID)
	if err != nil {
		return fmt.Errorf("preparing snapshot for layer %d: %w", i, err)
	}

	if _, err := c.client.DiffService().Apply(ctx, layerDesc, mounts); err != nil {
		c.removeActiveSnapshot(cleanupCtx, sn, activeKey, "active snapshot after apply failure", i)
		return fmt.Errorf("applying layer %d: %w", i, err)
	}

	gcRootOpt := snapshots.WithLabels(map[string]string{
		labelKeyGCRoot: gcTimestamp(),
	})
	commitErr := sn.Commit(ctx, chainID, activeKey, gcRootOpt)
	switch {
	case commitErr == nil:
		c.logger.Debug("Unpacked layer",
			zap.Int("layer", i),
			zap.String("chain_id", chainID),
			zap.Int64("size", layerDesc.Size),
		)
		if progress != nil {
			progress(UnpackProgress{
				Phase:       "layer",
				LayerIndex:  i,
				TotalLayers: totalLayers,
				LayerSize:   layerDesc.Size,
				Reused:      false,
			})
		}
	case errdefs.IsAlreadyExists(commitErr):
		// A concurrent unpack committed the same chain ID first. Our
		// active key still exists; clean it up and report the layer
		// as reused rather than freshly unpacked.
		c.removeActiveSnapshot(cleanupCtx, sn, activeKey, "active snapshot after concurrent commit", i)
		if progress != nil {
			progress(UnpackProgress{
				Phase:       "layer",
				LayerIndex:  i,
				TotalLayers: totalLayers,
				LayerSize:   layerDesc.Size,
				Reused:      true,
			})
		}
	default:
		return fmt.Errorf("committing snapshot for layer %d: %w", i, commitErr)
	}

	parentChainID = chainID
}

if progress != nil {
	progress(UnpackProgress{Phase: "complete", TotalLayers: totalLayers})
}

return nil
```

Note: the old loop also had `} else if !errdefs.IsNotFound(err) { return fmt.Errorf(...) }` after the stat call — that guard is now handled inside `statLayers`, so remove it entirely from the loop. The `sn` variable is still needed for `Prepare`, `Commit`, and `removeActiveSnapshot` — its declaration (`sn := c.client.SnapshotService("")`) remains unchanged.

- [ ] **Step 2 — Build to verify no compile errors**

```bash
cd go && go build ./internal/agent/containerd/...
```

Expected: exits 0 with no output.

- [ ] **Step 3 — Run all containerd package tests**

```bash
cd go && go test ./internal/agent/containerd/... -v
```

Expected: all tests PASS (including the new `TestStatLayers_*` from Task 1).

- [ ] **Step 4 — Commit**

```bash
cd go && git add internal/agent/containerd/unpack.go
git commit -m "perf: parallel snapshot stat pre-check in UnpackImage"
```

---

## Task 3: Add `CompressionType` enum to proto

**Files:**
- Modify: `Proto/wendy/agent/services/v1/wendy_agent_v1_container_service.proto`
- Regenerate: `go/proto/gen/agentpb/wendy_agent_v1_container_service.pb.go`

- [ ] **Step 1 — Add enum and field to `RunContainerLayerHeader`**

Open `Proto/wendy/agent/services/v1/wendy_agent_v1_container_service.proto` and replace the `RunContainerLayerHeader` message with:

```protobuf
message RunContainerLayerHeader {
    // The hash identity of the layer.
    string digest = 1;

    int64 size = 2;

    string diff_id = 3;

    // Whether the layer is compressed with gzip.
    // Deprecated: use compression instead. Kept for backward compatibility with
    // older CLIs that do not send the compression field.
    bool gzip = 4;

    // Compression format of the layer blob. When set to a non-zero value,
    // takes precedence over the gzip field.
    CompressionType compression = 5;

    enum CompressionType {
        COMPRESSION_GZIP = 0;  // default; treated as gzip when gzip=true, uncompressed when gzip=false
        COMPRESSION_ZSTD = 1;
        COMPRESSION_NONE = 2;
    }
}
```

- [ ] **Step 2 — Regenerate proto**

```bash
cd go && make proto
```

Expected: exits 0. `go/proto/gen/agentpb/wendy_agent_v1_container_service.pb.go` is updated with the new enum type `RunContainerLayerHeader_CompressionType` and constants `RunContainerLayerHeader_COMPRESSION_GZIP`, `RunContainerLayerHeader_COMPRESSION_ZSTD`, `RunContainerLayerHeader_COMPRESSION_NONE`.

- [ ] **Step 3 — Build everything to verify no compile errors**

```bash
cd go && go build ./...
```

Expected: exits 0.

- [ ] **Step 4 — Commit**

```bash
git add Proto/wendy/agent/services/v1/wendy_agent_v1_container_service.proto go/proto/gen/agentpb/
git commit -m "feat: add CompressionType enum to RunContainerLayerHeader proto"
```

---

## Task 4: Extract and test `layerMediaType` helper (TDD)

**Files:**
- Modify: `go/internal/agent/containerd/client.go`
- Modify: `go/internal/agent/containerd/client_test.go`

- [ ] **Step 1 — Write failing tests**

Append to `go/internal/agent/containerd/client_test.go`:

```go
func TestLayerMediaType_Zstd(t *testing.T) {
	got := layerMediaType(agentpb.RunContainerLayerHeader_COMPRESSION_ZSTD, false)
	want := "application/vnd.oci.image.layer.v1.tar+zstd"
	if got != want {
		t.Errorf("layerMediaType(ZSTD, false) = %q; want %q", got, want)
	}
}

func TestLayerMediaType_ZstdIgnoresGzipBool(t *testing.T) {
	got := layerMediaType(agentpb.RunContainerLayerHeader_COMPRESSION_ZSTD, true)
	want := "application/vnd.oci.image.layer.v1.tar+zstd"
	if got != want {
		t.Errorf("layerMediaType(ZSTD, true) = %q; want %q", got, want)
	}
}

func TestLayerMediaType_None(t *testing.T) {
	got := layerMediaType(agentpb.RunContainerLayerHeader_COMPRESSION_NONE, true)
	want := "application/vnd.oci.image.layer.v1.tar"
	if got != want {
		t.Errorf("layerMediaType(NONE, true) = %q; want %q", got, want)
	}
}

func TestLayerMediaType_GzipDefault_GzipTrue(t *testing.T) {
	// Old CLI path: compression field absent (zero value = GZIP), gzip=true.
	got := layerMediaType(agentpb.RunContainerLayerHeader_COMPRESSION_GZIP, true)
	want := "application/vnd.oci.image.layer.v1.tar+gzip"
	if got != want {
		t.Errorf("layerMediaType(GZIP, true) = %q; want %q", got, want)
	}
}

func TestLayerMediaType_GzipDefault_GzipFalse(t *testing.T) {
	// Old CLI path: compression field absent (zero value = GZIP), gzip=false → uncompressed.
	got := layerMediaType(agentpb.RunContainerLayerHeader_COMPRESSION_GZIP, false)
	want := "application/vnd.oci.image.layer.v1.tar"
	if got != want {
		t.Errorf("layerMediaType(GZIP, false) = %q; want %q", got, want)
	}
}
```

- [ ] **Step 2 — Run tests to confirm they fail**

```bash
cd go && go test ./internal/agent/containerd/ -run TestLayerMediaType -v
```

Expected: compile error — `layerMediaType` undefined.

- [ ] **Step 3 — Add `layerMediaType` to `client.go`**

Add the following function to `go/internal/agent/containerd/client.go` (anywhere before `AssembleImage`). Also ensure `ocispec "github.com/opencontainers/image-spec/specs-go/v1"` is in the import block — it already is.

```go
// layerMediaType returns the OCI media type for a layer given its compression.
// The compression field takes precedence; when it is COMPRESSION_GZIP (the zero
// default), the legacy gzip bool determines the type for backward compatibility.
func layerMediaType(compression agentpb.RunContainerLayerHeader_CompressionType, gzip bool) string {
	switch compression {
	case agentpb.RunContainerLayerHeader_COMPRESSION_ZSTD:
		return ocispec.MediaTypeImageLayerZstd
	case agentpb.RunContainerLayerHeader_COMPRESSION_NONE:
		return ocispec.MediaTypeImageLayer
	default: // COMPRESSION_GZIP (0) or unrecognised
		if gzip {
			return ocispec.MediaTypeImageLayerGzip
		}
		return ocispec.MediaTypeImageLayer
	}
}
```

- [ ] **Step 4 — Run tests to confirm they pass**

```bash
cd go && go test ./internal/agent/containerd/ -run TestLayerMediaType -v
```

Expected: all 5 `TestLayerMediaType_*` tests PASS.

- [ ] **Step 5 — Commit**

```bash
cd go && git add internal/agent/containerd/client.go internal/agent/containerd/client_test.go
git commit -m "feat: add layerMediaType helper for zstd/gzip/none OCI media type selection"
```

---

## Task 5: Wire `layerMediaType` into `AssembleImage`

**Files:**
- Modify: `go/internal/agent/containerd/client.go`

- [ ] **Step 1 — Replace the inline media-type branch in `AssembleImage`**

In `go/internal/agent/containerd/client.go`, inside `AssembleImage`, find:

```go
		mediaType := ocispec.MediaTypeImageLayerGzip
		if !l.GetGzip() {
			mediaType = ocispec.MediaTypeImageLayer
		}
```

Replace with:

```go
		mediaType := layerMediaType(l.GetCompression(), l.GetGzip())
```

- [ ] **Step 2 — Build to verify no compile errors**

```bash
cd go && go build ./internal/agent/containerd/...
```

Expected: exits 0.

- [ ] **Step 3 — Run all containerd and services package tests**

```bash
cd go && go test ./internal/agent/containerd/... ./internal/agent/services/... -v
```

Expected: all tests PASS.

- [ ] **Step 4 — Run the full test suite**

```bash
cd go && go test ./...
```

Expected: all tests PASS (integration tests that require containerd will be skipped on a dev machine — that is expected).

- [ ] **Step 5 — Commit**

```bash
cd go && git add internal/agent/containerd/client.go
git commit -m "perf: use layerMediaType in AssembleImage; add zstd support"
```
