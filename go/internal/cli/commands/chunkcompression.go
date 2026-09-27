package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/wendylabsinc/wendy/go/internal/cli/grpcclient"
	"github.com/wendylabsinc/wendy/go/internal/shared/chunkupload"
	"github.com/wendylabsinc/wendy/go/internal/shared/version"
)

const (
	// chunkCompressionEnv selects the WriteChunks compression: auto (the
	// default), gzip or none. See chooseChunkUploadConfig.
	chunkCompressionEnv = "WENDY_CHUNK_COMPRESSION"

	// chunkStallTimeout is how long uncompressed WriteChunks streams may sit
	// open with no progress before the push reconnects and retries with gzip.
	// It exceeds the 17–27 s NVMe write stalls seen on an Orin Nano (WDY-3210),
	// and a false positive only costs a switch to gzip.
	chunkStallTimeout = 30 * time.Second

	// chunkStallMemory is how long a device that stalled uncompressed stays
	// on gzip.
	chunkStallMemory = 30 * 24 * time.Hour

	// firstUncompressedChunkOSVersion is the first WendyOS that auto sends
	// uncompressed chunks to. #1765's link stall was reproduced on 0.18.2.
	firstUncompressedChunkOSVersion = "0.19.0"
)

// chunkUploadConfig is how one chunk push sends its WriteChunks messages.
type chunkUploadConfig struct {
	compressor   string        // chunkupload.Gzip, or "" for uncompressed
	stallTimeout time.Duration // > 0 runs the stall watchdog; set only when uncompressed
	stallKey     string        // the device's stall-memory key; "" records nothing
}

// gzipChunkUploadConfig is gzip with no watchdog: PR 1's transport, used over
// cloud tunnels, for old or unknown WendyOS, after a stall, and by callers
// that do not know their device.
var gzipChunkUploadConfig = chunkUploadConfig{compressor: chunkupload.Gzip}

// describe names the config for the WENDY_TIMING tuning line.
func (c chunkUploadConfig) describe() string {
	if c.compressor != "" {
		return c.compressor
	}
	return fmt.Sprintf("uncompressed, stall watchdog %s", c.stallTimeout)
}

// chunkUploadTarget is what the compression policy knows about a push.
type chunkUploadTarget struct {
	tunnel    bool   // a cloud tunnel: bandwidth-bound, so gzip pays for itself
	osVersion string // the agent's os_version, "" when unknown
	deviceKey string // deviceFingerprintKey, "" when unknown
}

// stallKey identifies the device and its OS for the stall memory, so an OS
// update gives the device a fresh chance at uncompressed uploads.
func (t chunkUploadTarget) stallKey() string {
	if t.deviceKey == "" {
		return ""
	}
	return t.deviceKey + "@" + t.osVersion
}

// chooseChunkUploadConfig applies WENDY_CHUNK_COMPRESSION. gzip and none force
// that choice; anything else is auto, which picks gzip over a cloud tunnel,
// for WendyOS before 0.19.0 or an unknown version, and for a device that
// stalled uncompressed within chunkStallMemory, and no compression otherwise.
// On a direct USB-C link to an Orin Nano, device-side gunzip (78 MB/s per
// core) was the upload's bottleneck (WDY-3211).
func chooseChunkUploadConfig(mode string, t chunkUploadTarget, stalledRecently func(key string) bool) chunkUploadConfig {
	uncompressed := chunkUploadConfig{stallTimeout: chunkStallTimeout, stallKey: t.stallKey()}
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "gzip":
		return gzipChunkUploadConfig
	case "none":
		return uncompressed
	}
	if t.tunnel || osNeedsGzipChunks(t.osVersion) || stalledRecently(t.stallKey()) {
		return gzipChunkUploadConfig
	}
	return uncompressed
}

// osNeedsGzipChunks reports whether osVersion is a WendyOS older than
// firstUncompressedChunkOSVersion, or unknown. Dev builds and non-WendyOS
// version strings (a distro's "24.04") compare as new enough.
func osNeedsGzipChunks(osVersion string) bool {
	v := strings.TrimPrefix(strings.TrimSpace(osVersion), "WendyOS-")
	if v == "" {
		return true
	}
	if version.IsDev(v) {
		return false
	}
	return version.CompareVersions(v, firstUncompressedChunkOSVersion) < 0
}

// chunkUploadConfigFor resolves the config for a push over conn. A failed
// version probe leaves the OS version unknown, which picks gzip.
func chunkUploadConfigFor(ctx context.Context, conn *grpcclient.AgentConnection) chunkUploadConfig {
	t := chunkUploadTarget{tunnel: conn.Reconnect != nil}
	if v, err := agentVersionForRun(ctx, conn); err == nil {
		t.osVersion, t.deviceKey = v.GetOsVersion(), deviceFingerprintKey(v)
	}
	now := time.Now()
	return chooseChunkUploadConfig(os.Getenv(chunkCompressionEnv), t, func(key string) bool {
		return chunkUploadStalledRecently(key, now)
	})
}

// chunkStallTestDir, when non-empty, overrides the stall memory's directory.
var chunkStallTestDir string

func chunkStallPath() (string, error) {
	dir := chunkStallTestDir
	if dir == "" {
		base, err := os.UserCacheDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(base, "wendy")
	}
	return filepath.Join(dir, "chunk-upload-stalls.json"), nil
}

// loadChunkStalls returns the recorded stalls by key. A missing, unreadable
// or corrupt file reads as no stalls: the memory must never fail a deploy.
func loadChunkStalls() map[string]time.Time {
	stalls := map[string]time.Time{}
	p, err := chunkStallPath()
	if err != nil {
		return stalls
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return stalls
	}
	if err := json.Unmarshal(data, &stalls); err != nil {
		return map[string]time.Time{}
	}
	return stalls
}

// chunkUploadStalledRecently reports whether key's device stalled uncompressed
// within chunkStallMemory of now.
func chunkUploadStalledRecently(key string, now time.Time) bool {
	if key == "" {
		return false
	}
	at, ok := loadChunkStalls()[key]
	return ok && now.Sub(at) < chunkStallMemory
}

// rememberChunkUploadStall records that key's device stalled uncompressed at
// now, and drops entries older than chunkStallMemory. It replaces the file
// atomically. Callers treat an error as best effort.
func rememberChunkUploadStall(key string, now time.Time) error {
	if key == "" {
		return nil
	}
	stalls := loadChunkStalls()
	for k, at := range stalls {
		if now.Sub(at) >= chunkStallMemory {
			delete(stalls, k)
		}
	}
	stalls[key] = now
	data, err := json.Marshal(stalls)
	if err != nil {
		return err
	}
	p, err := chunkStallPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), "chunk-upload-stalls-*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), p); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return nil
}
