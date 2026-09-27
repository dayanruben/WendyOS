package containerd

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	containerdclient "github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/core/content"
	"github.com/containerd/containerd/v2/core/leases"
	digest "github.com/opencontainers/go-digest"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"github.com/wendylabsinc/wendy/go/internal/shared/chunk"
)

// stageAged stages data and backdates its file by age.
func stageAged(t *testing.T, s *staging, data []byte, age time.Duration) [32]byte {
	t.Helper()
	h := sha256.Sum256(data)
	if err := s.write(h, data); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-age)
	if err := os.Chtimes(s.path(h), old, old); err != nil {
		t.Fatal(err)
	}
	return h
}

func TestChunkActivityIdleFor(t *testing.T) {
	var a chunkActivity
	now := time.Now()
	if !a.idleFor(time.Minute, now) {
		t.Fatal("a store that never saw a chunk RPC is idle")
	}
	a.touch()
	if a.idleFor(time.Minute, time.Now()) {
		t.Fatal("a store touched just now is not idle")
	}
	if !a.idleFor(time.Minute, time.Now().Add(2*time.Minute)) {
		t.Fatal("a store untouched for longer than d is idle")
	}
	end := a.begin()
	if a.idleFor(time.Minute, time.Now().Add(time.Hour)) {
		t.Fatal("a store with an assembly in flight is never idle")
	}
	end()
	if !a.idleFor(time.Minute, time.Now().Add(2*time.Minute)) {
		t.Fatal("the store is idle again once the assembly ends")
	}
}

func TestStagingSweepRemovesOnlyFilesOlderThanCutoff(t *testing.T) {
	s := newStaging(t.TempDir())
	old := stageAged(t, s, []byte("abandoned chunk"), 7*time.Hour)
	fresh := stageAged(t, s, []byte("chunk of a live deploy"), time.Minute)
	orphan := filepath.Join(s.dir, "stage-123")
	if err := os.WriteFile(orphan, []byte("temp file a crash left"), 0o600); err != nil {
		t.Fatal(err)
	}
	aged := time.Now().Add(-7 * time.Hour)
	if err := os.Chtimes(orphan, aged, aged); err != nil {
		t.Fatal(err)
	}

	cutoff := time.Now().Add(-stagingRetention)
	if files, bytes, err := s.usage(cutoff); err != nil || files != 2 || bytes == 0 {
		t.Fatalf("usage = %d files, %d bytes, %v; want the 2 aged files", files, bytes, err)
	}
	files, _, err := s.sweep(cutoff)
	if err != nil || files != 2 {
		t.Fatalf("sweep removed %d files, %v; want 2", files, err)
	}
	if s.has(old) || !s.has(fresh) {
		t.Fatalf("after sweep: old present=%v, fresh present=%v", s.has(old), s.has(fresh))
	}
	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Fatal("orphaned temp file survived the sweep")
	}
}

func TestStagingRetireStartsEmptyAndPurgeDeletesLeftovers(t *testing.T) {
	s := newStaging(filepath.Join(t.TempDir(), "staging"))
	leftover := stageAged(t, s, []byte("chunk from the previous agent run"), time.Hour)

	if err := s.retire(time.Now()); err != nil {
		t.Fatal(err)
	}
	if s.has(leftover) {
		t.Fatal("retired chunk is still visible to this run")
	}
	data := []byte("chunk staged after startup")
	if err := s.write(sha256.Sum256(data), data); err != nil {
		t.Fatalf("staging after retire: %v", err)
	}

	files, _, err := s.purgeRetired()
	if err != nil || files != 1 {
		t.Fatalf("purgeRetired removed %d files, %v; want 1", files, err)
	}
	if left, _ := filepath.Glob(s.dir + retiredStagingSuffix + "*"); len(left) != 0 {
		t.Fatalf("retired dirs left behind: %v", left)
	}
	if !s.has(sha256.Sum256(data)) {
		t.Fatal("purge removed a chunk staged by this run")
	}
}

// recordingLeases is a leases.Manager that records synchronous deletes.
type recordingLeases struct {
	leases.Manager
	created, syncDeletes int
}

// newMaintenanceClient builds a Client over a fake content store (holding
// only present) and a lease recorder, with its own index and staging dir.
func newMaintenanceClient(t *testing.T, present ...digest.Digest) (*Client, *recordingLeases) {
	t.Helper()
	blobs := map[digest.Digest]content.Info{}
	for _, d := range present {
		blobs[d] = content.Info{Digest: d, Size: 1 << 20}
	}
	ls := &recordingLeases{}
	client, err := containerdclient.New("",
		containerdclient.WithDefaultNamespace("default"),
		containerdclient.WithServices(
			containerdclient.WithContentStore(&chunkAvailabilityContentStore{blobs: blobs}),
			containerdclient.WithLeasesService(ls),
		),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close() })
	return &Client{
		client:     client,
		logger:     zap.NewNop(),
		namespace:  "default",
		chunkIndex: newTestChunkIndex(t),
		staging:    newStaging(filepath.Join(t.TempDir(), "staging")),
	}, ls
}

func TestReconcileChunkIndexDropsBlobsContainerdNoLongerHolds(t *testing.T) {
	kept := digest.FromString("kept layer")
	c, _ := newMaintenanceClient(t, kept)
	if err := c.chunkIndex.AddLayer(kept.String(), []chunk.Ref{{Hash: [32]byte{1}, Len: 1}}); err != nil {
		t.Fatal(err)
	}
	const collected = 3
	for i := range collected {
		blob := digest.FromString(fmt.Sprint("collected layer ", i)).String()
		if err := c.chunkIndex.AddLayer(blob, []chunk.Ref{{Hash: [32]byte{2, byte(i)}, Len: 1}}); err != nil {
			t.Fatal(err)
		}
	}

	before := committedTxID(t, c.chunkIndex)
	dropped, err := c.reconcileChunkIndex(context.Background())
	if err != nil || dropped != collected {
		t.Fatalf("reconcile dropped %d, %v; want %d", dropped, err, collected)
	}
	if n := committedTxID(t, c.chunkIndex) - before; n != 1 {
		t.Fatalf("dropping %d collected blobs took %d transactions, want 1", collected, n)
	}
	for i := range collected {
		if _, ok := c.chunkIndex.Has([32]byte{2, byte(i)}); ok {
			t.Fatalf("entry of collected blob %d survived", i)
		}
	}
	if _, ok := c.chunkIndex.Has([32]byte{1}); !ok {
		t.Fatal("entry of a present blob was dropped")
	}
}

// panickingContentStore stands in for any bug a maintenance pass could hit.
type panickingContentStore struct{ content.Store }

func (panickingContentStore) Info(context.Context, digest.Digest) (content.Info, error) {
	panic("content store bug")
}

// TestChunkStoreMaintenanceSurvivesAPanickingPass: nothing above the
// maintenance goroutine recovers a panic, so one escaping it would kill the
// agent.
func TestChunkStoreMaintenanceSurvivesAPanickingPass(t *testing.T) {
	c := newChunkAvailabilityClient(t, panickingContentStore{}, newTestChunkIndex(t), filepath.Join(t.TempDir(), "staging"))
	core, logs := observer.New(zap.ErrorLevel)
	c.logger = zap.New(core)
	if err := c.chunkIndex.AddLayer(digest.FromString("layer").String(), []chunk.Ref{{Hash: [32]byte{1}, Len: 1}}); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // return right after the startup pass
	c.runChunkStoreMaintenance(ctx, time.Hour)
	if n := logs.FilterMessage("Chunk store maintenance panicked").Len(); n != 1 {
		t.Fatalf("logged %d maintenance panics, want 1", n)
	}
}

func TestMaintainIdleChunkStoreSkipsWhileADeployIsActive(t *testing.T) {
	c, _ := newMaintenanceClient(t)
	h := stageAged(t, c.staging, []byte("chunk of an abandoned deploy"), 7*time.Hour)

	c.chunkActivity.touch()
	c.maintainIdleChunkStore(context.Background(), time.Now())
	if !c.staging.has(h) {
		t.Fatal("maintenance swept staging while a deploy was active")
	}

	c.maintainIdleChunkStore(context.Background(), time.Now().Add(chunkStoreIdleAfter+time.Minute))
	if c.staging.has(h) {
		t.Fatal("idle maintenance kept a chunk older than stagingRetention")
	}
}
