package containerd

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/containerd/errdefs"
	digest "github.com/opencontainers/go-digest"
	"go.uber.org/zap"
)

const (
	// chunkStoreIdleAfter is how long the chunk store must see no chunk RPC,
	// with no assembly in flight, before periodic maintenance may sweep staged
	// chunks: a deploy touches the store at least once per chunk it sends.
	chunkStoreIdleAfter = 10 * time.Minute
	// chunkStoreMaintenanceInterval paces the idle-time maintenance pass.
	chunkStoreMaintenanceInterval = 15 * time.Minute
	// stagingRetention keeps the chunks of a cancelled or failed deploy long
	// enough for the user's next attempt to resume from them (WDY-3217).
	stagingRetention = 6 * time.Hour
	// cachePruneStagingIdleAfter is the quiet period an explicit cache prune
	// requires before it removes every staged chunk.
	cachePruneStagingIdleAfter = time.Minute
	// retiredStagingSuffix marks a staging directory from an earlier agent run.
	retiredStagingSuffix = ".retired-"
)

// chunkActivity tracks whether a deploy may be relying on staged chunks.
type chunkActivity struct {
	lastNano atomic.Int64
	inFlight atomic.Int32
}

func (a *chunkActivity) touch() { a.lastNano.Store(time.Now().UnixNano()) }

// begin marks an assembly or image preparation in flight until the returned
// func runs.
func (a *chunkActivity) begin() func() {
	a.inFlight.Add(1)
	a.touch()
	return func() {
		a.touch()
		a.inFlight.Add(-1)
	}
}

// idleFor reports whether nothing is in flight and no chunk RPC has touched
// the store for at least d.
func (a *chunkActivity) idleFor(d time.Duration, now time.Time) bool {
	if a.inFlight.Load() > 0 {
		return false
	}
	last := a.lastNano.Load()
	return last == 0 || now.Sub(time.Unix(0, last)) >= d
}

// sweep removes staged chunk files, and temp files a crash orphaned, last
// modified before cutoff.
func (s *staging) sweep(cutoff time.Time) (files int, bytes int64, err error) {
	return s.walk(cutoff, true)
}

// usage reports what sweep(cutoff) would remove.
func (s *staging) usage(cutoff time.Time) (files int, bytes int64, err error) {
	return s.walk(cutoff, false)
}

func (s *staging) walk(cutoff time.Time, remove bool) (files int, bytes int64, err error) {
	d, err := os.Open(s.dir)
	if os.IsNotExist(err) {
		return 0, 0, nil
	}
	if err != nil {
		return 0, 0, err
	}
	defer d.Close()
	for {
		entries, rerr := d.ReadDir(1024)
		for _, e := range entries {
			if !e.Type().IsRegular() {
				continue
			}
			info, ierr := e.Info()
			if ierr != nil || !info.ModTime().Before(cutoff) {
				continue
			}
			if remove {
				if err := os.Remove(filepath.Join(s.dir, e.Name())); err != nil && !os.IsNotExist(err) {
					continue
				}
			}
			files++
			bytes += info.Size()
		}
		if rerr == io.EOF {
			return files, bytes, nil
		}
		if rerr != nil {
			return files, bytes, rerr
		}
	}
}

// retire renames the staging directory aside so this agent run starts with an
// empty one. No deploy in this process can be relying on a chunk staged by an
// earlier run, and renaming is instant, so leftovers of any size can be
// deleted in the background without racing new uploads.
func (s *staging) retire(now time.Time) error {
	err := os.Rename(s.dir, fmt.Sprintf("%s%s%d", s.dir, retiredStagingSuffix, now.UnixNano()))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// purgeRetired deletes every staging directory retire moved aside, including
// ones a crash left behind before their purge finished.
func (s *staging) purgeRetired() (files int, bytes int64, err error) {
	dirs, err := filepath.Glob(s.dir + retiredStagingSuffix + "*")
	if err != nil {
		return 0, 0, err
	}
	for _, dir := range dirs {
		n, b, werr := newStaging(dir).sweep(time.Now().Add(time.Hour))
		files, bytes = files+n, bytes+b
		if werr != nil {
			err = werr
			continue
		}
		if rerr := os.RemoveAll(dir); rerr != nil {
			err = rerr
		}
	}
	return files, bytes, err
}

// StartChunkStoreMaintenance keeps the chunk store bounded (WDY-3212,
// WDY-3217). It retires this agent's leftover staging directory before
// returning — call it before the agent serves RPCs — then, in the background,
// deletes the retired chunks, drops index entries for layer blobs containerd
// no longer holds, and repeats the sweep and reconciliation every
// chunkStoreMaintenanceInterval while no deploy is using the store.
func (c *Client) StartChunkStoreMaintenance(ctx context.Context) {
	if err := c.staging.retire(time.Now()); err != nil {
		c.logger.Warn("Retiring leftover chunk staging failed", zap.Error(err))
	}
	go c.runChunkStoreMaintenance(ctx, chunkStoreMaintenanceInterval)
}

func (c *Client) runChunkStoreMaintenance(ctx context.Context, interval time.Duration) {
	files, bytes, err := c.staging.purgeRetired()
	if err != nil {
		c.logger.Warn("Deleting leftover chunk staging failed", zap.Error(err))
	}
	dropped, err := c.reconcileChunkIndex(ctx)
	if err != nil {
		c.logger.Warn("Reconciling chunk index failed", zap.Error(err))
	}
	c.logChunkStoreMaintenance(files, bytes, dropped)

	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			c.maintainIdleChunkStore(ctx, now)
		}
	}
}

// maintainIdleChunkStore sweeps staged chunks older than stagingRetention and
// reconciles the index, unless a deploy has used the store recently.
func (c *Client) maintainIdleChunkStore(ctx context.Context, now time.Time) {
	c.chunkSweepMu.Lock()
	if !c.chunkActivity.idleFor(chunkStoreIdleAfter, now) {
		c.chunkSweepMu.Unlock()
		return
	}
	files, bytes, err := c.staging.sweep(now.Add(-stagingRetention))
	c.chunkSweepMu.Unlock()
	if err != nil {
		c.logger.Warn("Sweeping chunk staging failed", zap.Error(err))
	}
	dropped, err := c.reconcileChunkIndex(ctx)
	if err != nil {
		c.logger.Warn("Reconciling chunk index failed", zap.Error(err))
	}
	c.logChunkStoreMaintenance(files, bytes, dropped)
}

func (c *Client) logChunkStoreMaintenance(files int, bytes int64, droppedBlobs int) {
	if files == 0 && droppedBlobs == 0 {
		return
	}
	c.logger.Info("Chunk store maintenance",
		zap.Int("staged_chunks_removed", files),
		zap.Int64("staged_bytes_removed", bytes),
		zap.Int("index_blobs_dropped", droppedBlobs))
}

// reconcileChunkIndex drops the entries of every indexed blob containerd no
// longer holds and returns how many blobs it dropped.
func (c *Client) reconcileChunkIndex(ctx context.Context) (int, error) {
	blobs, err := c.chunkIndex.Blobs()
	if err != nil {
		return 0, err
	}
	ctx = c.withNamespace(ctx)
	cs := c.client.ContentStore()
	dropped := 0
	for _, blob := range blobs {
		if dgst, perr := digest.Parse(blob); perr == nil {
			_, ierr := cs.Info(ctx, dgst)
			if ierr == nil {
				continue
			}
			if !errdefs.IsNotFound(ierr) {
				return dropped, fmt.Errorf("checking indexed blob %s: %w", blob, ierr)
			}
		}
		if err := c.chunkIndex.Drop(blob); err != nil {
			return dropped, err
		}
		dropped++
	}
	return dropped, nil
}
