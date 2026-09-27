package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wendylabsinc/wendy/go/internal/shared/chunkupload"
)

func TestChooseChunkUploadConfig(t *testing.T) {
	never := func(string) bool { return false }
	lan := func(osVersion string) chunkUploadTarget {
		return chunkUploadTarget{osVersion: osVersion, deviceKey: "0123abcd", directLink: true}
	}
	routable := func(osVersion string) chunkUploadTarget {
		return chunkUploadTarget{osVersion: osVersion, deviceKey: "0123abcd"}
	}
	uncompressed := func(key string) chunkUploadConfig {
		return chunkUploadConfig{stallTimeout: chunkStallTimeout, stallKey: key}
	}
	for _, tc := range []struct {
		name    string
		mode    string
		target  chunkUploadTarget
		stalled func(string) bool
		want    chunkUploadConfig
	}{
		{"auto: current WendyOS on a direct link", "", lan("0.19.3"), never, uncompressed("0123abcd@0.19.3")},
		{"auto: display prefix", "auto", lan("WendyOS-0.19.3"), never, uncompressed("0123abcd@WendyOS-0.19.3")},
		{"auto: nightly of the first version", "", lan("0.19.0-nightly"), never, uncompressed("0123abcd@0.19.0-nightly")},
		{"auto: dev OS build", "", lan("0.18.0-dev"), never, uncompressed("0123abcd@0.18.0-dev")},
		{"auto: non-WendyOS distro version", "", lan("24.04"), never, uncompressed("0123abcd@24.04")},
		{"auto: WendyOS 0.18.2 (#1765)", "", lan("0.18.2"), never, gzipChunkUploadConfig},
		{"auto: unknown OS version", "", lan(""), never, gzipChunkUploadConfig},
		{"auto: cloud tunnel", "", chunkUploadTarget{tunnel: true, osVersion: "0.19.3", deviceKey: "0123abcd", directLink: true}, never, gzipChunkUploadConfig},
		{"auto: stalled before", "", lan("0.19.3"), func(k string) bool { return k == "0123abcd@0.19.3" }, gzipChunkUploadConfig},
		{"auto: no device key records nothing", "", chunkUploadTarget{osVersion: "0.19.3", directLink: true}, never, uncompressed("")},
		{"auto: routable LAN address keeps gzip", "", routable("0.19.3"), never, gzipChunkUploadConfig},
		{"auto: routable Wi-Fi address keeps gzip even with no stall history", "", routable("0.19.3"), never, gzipChunkUploadConfig},
		{"gzip forced on a direct link", "gzip", lan("0.19.3"), never, gzipChunkUploadConfig},
		{"none forced over a tunnel", "none", chunkUploadTarget{tunnel: true, osVersion: "0.18.2", deviceKey: "k", directLink: true}, never, uncompressed("k@0.18.2")},
		{"none forced on a routable LAN address", "none", routable("0.19.3"), never, uncompressed("0123abcd@0.19.3")},
		{"case and space are ignored", " GZIP ", lan("0.19.3"), never, gzipChunkUploadConfig},
		{"an unknown mode means auto", "zstd", lan("0.18.2"), never, gzipChunkUploadConfig},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := chooseChunkUploadConfig(tc.mode, tc.target, tc.stalled); got != tc.want {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
		})
	}
	if gzipChunkUploadConfig.compressor != chunkupload.Gzip || gzipChunkUploadConfig.stallTimeout != 0 {
		t.Fatalf("gzip config = %+v, want gzip with no watchdog", gzipChunkUploadConfig)
	}
}

// TestChunkCompressionModeFromEnvWarnsOnceOnAnUnknownValue is M4: a typo'd
// WENDY_CHUNK_COMPRESSION must not fail silently into auto.
func TestChunkCompressionModeFromEnvWarnsOnceOnAnUnknownValue(t *testing.T) {
	warnUnknownChunkCompressionOnce = &sync.Once{}
	t.Cleanup(func() { warnUnknownChunkCompressionOnce = &sync.Once{} })
	t.Setenv(chunkCompressionEnv, "zstd")

	out := captureStderr(t, func() {
		if got := chunkCompressionModeFromEnv(); got != "zstd" {
			t.Fatalf("mode = %q, want the raw env value returned unchanged", got)
		}
	})
	if !strings.Contains(out, "zstd") || !strings.Contains(out, "auto") {
		t.Fatalf("warning = %q, want it to name the bad value and the valid ones", out)
	}

	// A second read of the same bad value must not warn again (sync.Once).
	out2 := captureStderr(t, func() { chunkCompressionModeFromEnv() })
	if out2 != "" {
		t.Fatalf("warned a second time: %q", out2)
	}
}

// TestChunkCompressionModeFromEnvAcceptsKnownValues: every value
// chooseChunkUploadConfig itself recognizes must never warn.
func TestChunkCompressionModeFromEnvAcceptsKnownValues(t *testing.T) {
	warnUnknownChunkCompressionOnce = &sync.Once{}
	t.Cleanup(func() { warnUnknownChunkCompressionOnce = &sync.Once{} })
	for _, v := range []string{"", "auto", "gzip", "none", " GZIP ", "None", " "} {
		t.Setenv(chunkCompressionEnv, v)
		out := captureStderr(t, func() { chunkCompressionModeFromEnv() })
		if out != "" {
			t.Fatalf("value %q warned: %q", v, out)
		}
	}
}

func TestChunkStallMemory(t *testing.T) {
	chunkStallTestDir = t.TempDir()
	t.Cleanup(func() { chunkStallTestDir = "" })
	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)

	if chunkUploadStalledRecently("dev@0.19.3", now) {
		t.Fatal("a device with no recorded stall reported one")
	}
	if err := rememberChunkUploadStall("old@0.19.1", now.Add(-40*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := rememberChunkUploadStall("dev@0.19.3", now); err != nil {
		t.Fatal(err)
	}
	if !chunkUploadStalledRecently("dev@0.19.3", now.Add(29*24*time.Hour)) {
		t.Fatal("a stall 29 days ago was forgotten")
	}
	if chunkUploadStalledRecently("dev@0.19.3", now.Add(31*24*time.Hour)) {
		t.Fatal("a stall 31 days ago was still remembered")
	}
	if chunkUploadStalledRecently("dev@0.19.4", now) {
		t.Fatal("an OS update did not reset the device's stall memory")
	}
	stalls := loadChunkStalls()
	if _, ok := stalls["old@0.19.1"]; ok {
		t.Fatal("remembering a stall did not prune an expired entry")
	}
	if rememberChunkUploadStall("", now) != nil || chunkUploadStalledRecently("", now) {
		t.Fatal("an empty key must record and report nothing")
	}
}

// TestChunkStallMemoryFilesAreWorldReadable is M5: under `sudo wendy run` on
// macOS, a 0600 stall data file or lock file becomes root-owned and blocks
// the user's own later, unprivileged runs from ever reading or locking it
// again. Both files must come out 0644, matching the CLI's other cache files.
func TestChunkStallMemoryFilesAreWorldReadable(t *testing.T) {
	chunkStallTestDir = t.TempDir()
	t.Cleanup(func() { chunkStallTestDir = "" })

	if err := rememberChunkUploadStall("dev@0.19.3", time.Now()); err != nil {
		t.Fatal(err)
	}
	p, err := chunkStallPath()
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{p, p + ".lock"} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat %s: %v", path, err)
		}
		if perm := info.Mode().Perm(); perm != 0o644 {
			t.Fatalf("%s mode = %o, want 0644", path, perm)
		}
	}
}

func TestChunkStallMemoryToleratesACorruptFile(t *testing.T) {
	chunkStallTestDir = t.TempDir()
	t.Cleanup(func() { chunkStallTestDir = "" })
	p, err := chunkStallPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if chunkUploadStalledRecently("dev@0.19.3", now) {
		t.Fatal("a corrupt file reported a stall")
	}
	if err := rememberChunkUploadStall("dev@0.19.3", now); err != nil {
		t.Fatal(err)
	}
	if !chunkUploadStalledRecently("dev@0.19.3", now) {
		t.Fatal("remembering a stall did not replace the corrupt file")
	}
	if _, err := os.Stat(filepath.Join(chunkStallTestDir, "chunk-upload-stalls.json")); err != nil {
		t.Fatal(err)
	}
}

func TestChunkUploadConfigDescribe(t *testing.T) {
	if got := gzipChunkUploadConfig.describe(); got != "gzip" {
		t.Fatalf("gzip describe = %q", got)
	}
	if got := (chunkUploadConfig{stallTimeout: 30 * time.Second}).describe(); got != "uncompressed, stall watchdog 30s" {
		t.Fatalf("uncompressed describe = %q", got)
	}
}

func TestChunkStallMemoryKeepsConcurrentRecords(t *testing.T) {
	chunkStallTestDir = t.TempDir()
	t.Cleanup(func() { chunkStallTestDir = "" })
	now := time.Now()

	// 16 goroutines each record a stall concurrently using a start barrier.
	numGoroutines := 16
	var wg sync.WaitGroup
	ready := make(chan struct{})
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(i int) {
			defer wg.Done()
			// Wait for the signal to start.
			<-ready
			key := fmt.Sprintf("dev%d@0.19.3", i)
			if err := rememberChunkUploadStall(key, now); err != nil {
				t.Errorf("goroutine %d: %v", i, err)
			}
		}(i)
	}

	// Signal all goroutines to proceed concurrently.
	close(ready)
	wg.Wait()

	// Verify all 16 keys were recorded without loss.
	stalls := loadChunkStalls()
	for i := 0; i < numGoroutines; i++ {
		key := fmt.Sprintf("dev%d@0.19.3", i)
		if _, ok := stalls[key]; !ok {
			t.Errorf("concurrent record for goroutine %d was lost", i)
		}
	}
}
