package containerd

import (
	"crypto/sha256"
	"os"
	"path/filepath"
	"testing"
	"time"
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
