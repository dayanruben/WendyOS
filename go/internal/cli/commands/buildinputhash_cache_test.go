package commands

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// settleContextDigestClock makes files the test just wrote count as settled.
func settleContextDigestClock(t *testing.T) {
	t.Helper()
	orig := contextDigestClock
	contextDigestClock = settledNow
	t.Cleanup(func() { contextDigestClock = orig })
}

func pinnedProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, dir, "Dockerfile", "FROM scratch\nCOPY . /app\n")
	writeFile(t, dir, "app.py", "print('v1')\n")
	writeFile(t, dir, "model.bin", "weights\n")
	return dir
}

// TestComputeBuildInputHashUsesTheDigestCache proves a warm run takes a
// settled file's digest from the cache instead of reading it: a doctored
// cached digest shows up in the hash. Any change to the file ends that.
func TestComputeBuildInputHashUsesTheDigestCache(t *testing.T) {
	requireFileIdentity(t)
	useContextDigestCacheDir(t)
	settleContextDigestClock(t)
	dir := pinnedProject(t)

	cold := hashOrFatal(t, dir, nil)
	if warm := hashOrFatal(t, dir, nil); warm != cold {
		t.Fatalf("warm hash %s != cold hash %s", warm, cold)
	}

	p, _ := contextDigestCachePath(dir, filepath.Join(dir, "Dockerfile"))
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	var f contextDigestFile
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatal(err)
	}
	e, ok := f.Entries["model.bin"]
	if !ok {
		t.Fatalf("model.bin was not cached: %v", f.Entries)
	}
	e.Digest = sha256HexOf("something else")
	f.Entries["model.bin"] = e
	data, _ = json.Marshal(f)
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := hashOrFatal(t, dir, nil); got == cold {
		t.Fatal("the warm run read model.bin instead of using its cached digest")
	}

	// Rewriting the file (same bytes) changes its identity, so it is read.
	time.Sleep(50 * time.Millisecond)
	writeFile(t, dir, "model.bin", "weights\n")
	if got := hashOrFatal(t, dir, nil); got != cold {
		t.Fatalf("hash after re-reading model.bin = %s, want %s", got, cold)
	}
}

// TestComputeBuildInputHashSeesSameSizeEdits is the correctness bar for the
// cache: edits that keep a file's size and mtime still change the hash, with
// a racy (just-written) file and with a settled, cached one.
func TestComputeBuildInputHashSeesSameSizeEdits(t *testing.T) {
	useContextDigestCacheDir(t)
	old := time.Now().Add(-time.Hour).Truncate(time.Second)
	sameSizeEdit := func(t *testing.T, dir, content string) {
		t.Helper()
		writeFile(t, dir, "app.py", content)
		if err := os.Chtimes(filepath.Join(dir, "app.py"), old, old); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("racy", func(t *testing.T) {
		dir := pinnedProject(t)
		base := hashOrFatal(t, dir, nil)
		sameSizeEdit(t, dir, "print('v2')\n")
		if got := hashOrFatal(t, dir, nil); got == base {
			t.Fatal("a same-size edit within the racy window was missed")
		}
	})
	t.Run("settled, rewritten in place", func(t *testing.T) {
		requireFileIdentity(t)
		settleContextDigestClock(t)
		dir := pinnedProject(t)
		sameSizeEdit(t, dir, "print('v1')\n")
		base := hashOrFatal(t, dir, nil)
		time.Sleep(50 * time.Millisecond)
		sameSizeEdit(t, dir, "print('v2')\n")
		if got := hashOrFatal(t, dir, nil); got == base {
			t.Fatal("a same-size in-place rewrite with a reset mtime was missed")
		}
	})
	t.Run("settled, replaced by rename", func(t *testing.T) {
		requireFileIdentity(t)
		settleContextDigestClock(t)
		dir := pinnedProject(t)
		sameSizeEdit(t, dir, "print('v1')\n")
		base := hashOrFatal(t, dir, nil)
		writeFile(t, dir, "app.py.new", "print('v3')\n")
		if err := os.Chtimes(filepath.Join(dir, "app.py.new"), old, old); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(filepath.Join(dir, "app.py.new"), filepath.Join(dir, "app.py")); err != nil {
			t.Fatal(err)
		}
		if got := hashOrFatal(t, dir, nil); got == base {
			t.Fatal("a same-size file replaced by a rename was missed")
		}
	})
}

// TestComputeBuildInputHashTracksFileMode: COPY keeps permission bits, so
// chmod +x on an entrypoint changes the image and must change the hash.
func TestComputeBuildInputHashTracksFileMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no executable bit")
	}
	useContextDigestCacheDir(t)
	dir := pinnedProject(t)
	base := hashOrFatal(t, dir, nil)
	if err := os.Chmod(filepath.Join(dir, "app.py"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := hashOrFatal(t, dir, nil); got == base {
		t.Fatal("chmod +x did not change the hash")
	}
}
