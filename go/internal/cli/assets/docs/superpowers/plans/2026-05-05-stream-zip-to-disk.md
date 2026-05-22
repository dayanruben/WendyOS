# Stream Zip → Disk Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stream zip decompression directly into the disk writer so the 59 GB extracted image never lands on disk; cache the compressed zip (~5.5 GB) instead.

**Architecture:** `writeImageToDisk` changes from accepting a file path to accepting `io.Reader + int64`. A new `streamZipImageEntry` helper opens a zip and returns a streaming reader over the first `.img/.raw/.wic` entry. `resolveOSImage` now caches the zip file instead of the extracted image; `openOSImageStream` wraps it to return a ready-to-stream reader+size pair. The "Extracting image…" progress bar disappears; extraction happens silently inside the write goroutine.

**Tech Stack:** Go stdlib (`archive/zip`, `io`, `os/exec`), Bubble Tea TUI, `dd` (macOS/Linux), raw syscall writes (Windows).

---

## File Map

| File | Change |
|---|---|
| `go/internal/cli/commands/os_install.go` | Add `osCachedZipPath`, `streamZipImageEntry`, `zipReadCloser`, `openOSImageStream`, `openLocalImageStream`; update `resolveOSImage`; delete `extractImageFromZipWithProgress`; update `installLinuxImage` and `runOSInstallDirect` call sites |
| `go/internal/cli/commands/os_install_test.go` | Add tests for `osCachedZipPath`, `streamZipImageEntry`, `openOSImageStream`, `resolveOSImage` (updated) |
| `go/internal/cli/commands/disklister_darwin.go` | `writeImageToDisk`: drop `if=` arg, set `cmd.Stdin = r`, add `totalSize` param |
| `go/internal/cli/commands/disklister_linux.go` | Same as darwin; fix index shift when removing `if=` from ddArgs |
| `go/internal/cli/commands/disklister_windows.go` | `writeImageToDisk`: replace `os.Open(imagePath)` with reader param |
| `go/internal/cli/commands/os_download.go` | Update cache check to look for `.zip` first, then legacy `.img`; update Long description |

---

## Task 1: Branch

- [ ] **Create branch**

```bash
git checkout main && git pull && git checkout -b jo/stream-zip-to-disk
```

---

## Task 2: Tests for `osCachedZipPath` and `streamZipImageEntry`

**Files:**
- Modify: `go/internal/cli/commands/os_install_test.go`

- [ ] **Step 1: Add missing imports to `os_install_test.go`**

The new tests need `"archive/zip"` and `"io"`. Add them to the existing import block:

```go
import (
    "archive/zip"
    "bytes"
    "fmt"
    "io"
    "net/http"
    "net/http/httptest"
    "os"
    "strconv"
    "strings"
    "sync/atomic"
    "testing"

    "github.com/wendylabsinc/wendy/internal/shared/version"
)
```

- [ ] **Step 2: Write failing tests**

Add to `os_install_test.go` after the existing `TestOsCachedImagePath_Sanitization` block:

```go
func TestOsCachedZipPath_Sanitization(t *testing.T) {
	path, err := osCachedZipPath("raspberry-pi-5", "0.10.4")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasSuffix(path, ".zip") {
		t.Fatalf("expected .zip suffix, got %q", path)
	}

	_, err = osCachedZipPath("raspberry-pi-5", "../../../etc/passwd")
	if err == nil {
		t.Fatal("expected error for path traversal in version")
	}

	_, err = osCachedZipPath("../evil", "0.10.4")
	if err == nil {
		t.Fatal("expected error for path traversal in device key")
	}
}

func makeTestZip(t *testing.T, entryName string, content []byte) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "test-*.zip")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	w := zip.NewWriter(f)
	fw, err := w.Create(entryName)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return f.Name()
}

func TestStreamZipImageEntry(t *testing.T) {
	content := []byte("fake image data 12345")

	t.Run("reads img entry", func(t *testing.T) {
		zipPath := makeTestZip(t, "wendyos.img", content)
		r, size, err := streamZipImageEntry(zipPath)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer r.Close()
		if size != int64(len(content)) {
			t.Errorf("size = %d; want %d", size, len(content))
		}
		got, err := io.ReadAll(r)
		if err != nil {
			t.Fatalf("reading: %v", err)
		}
		if !bytes.Equal(got, content) {
			t.Errorf("content mismatch")
		}
	})

	t.Run("reads raw entry", func(t *testing.T) {
		zipPath := makeTestZip(t, "wendyos.raw", content)
		r, _, err := streamZipImageEntry(zipPath)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		r.Close()
	})

	t.Run("reads wic entry", func(t *testing.T) {
		zipPath := makeTestZip(t, "wendyos.wic", content)
		r, _, err := streamZipImageEntry(zipPath)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		r.Close()
	})

	t.Run("no image entry returns error", func(t *testing.T) {
		zipPath := makeTestZip(t, "readme.txt", content)
		_, _, err := streamZipImageEntry(zipPath)
		if err == nil {
			t.Fatal("expected error for zip with no image entry")
		}
	})

	t.Run("nonexistent file returns error", func(t *testing.T) {
		_, _, err := streamZipImageEntry("/nonexistent/path/image.zip")
		if err == nil {
			t.Fatal("expected error for nonexistent file")
		}
	})
}
```

- [ ] **Step 3: Run tests to verify they fail**

```bash
cd go && go test ./internal/cli/commands/ -run 'TestOsCachedZipPath|TestStreamZipImageEntry' -v 2>&1 | head -30
```

Expected: compile error (functions not defined yet).

- [ ] **Step 4: Commit test file**

```bash
git add go/internal/cli/commands/os_install_test.go
git commit -m "test: add tests for osCachedZipPath and streamZipImageEntry"
```

---

## Task 3: Implement `osCachedZipPath`, `zipReadCloser`, and `streamZipImageEntry`

**Files:**
- Modify: `go/internal/cli/commands/os_install.go`

- [ ] **Step 1: Add `osCachedZipPath` immediately after the existing `osCachedImagePath` function (around line 951)**

```go
// osCachedZipPath returns the expected cache path for a device+version zip.
// Format: <cache>/os-images/<device>-<version>.zip
func osCachedZipPath(deviceKey, version string) (string, error) {
	safeDevice := filepath.Base(deviceKey)
	safeVersion := filepath.Base(version)
	if safeDevice != deviceKey || safeVersion != version ||
		strings.Contains(deviceKey, "..") || strings.Contains(version, "..") {
		return "", fmt.Errorf("invalid device key or version: %q / %q", deviceKey, version)
	}

	dir, err := osCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, fmt.Sprintf("%s-%s.zip", safeDevice, safeVersion)), nil
}
```

- [ ] **Step 2: Add `zipReadCloser` and `streamZipImageEntry` immediately after `osCachedZipPath`**

```go
// zipReadCloser wraps a zip.ReadCloser and its entry's ReadCloser so both
// are released with a single Close call.
type zipReadCloser struct {
	archive *zip.ReadCloser
	entry   io.ReadCloser
}

func (z *zipReadCloser) Read(p []byte) (int, error) { return z.entry.Read(p) }

func (z *zipReadCloser) Close() error {
	z.entry.Close()
	return z.archive.Close()
}

// streamZipImageEntry opens a zip archive and returns a streaming reader over
// the first .img, .raw, or .wic entry it finds, plus the uncompressed size.
// The caller must Close the returned reader.
func streamZipImageEntry(zipPath string) (io.ReadCloser, int64, error) {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, 0, fmt.Errorf("opening zip: %w", err)
	}

	for _, f := range r.File {
		if f.FileInfo().IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(f.Name))
		if ext != ".img" && ext != ".raw" && ext != ".wic" {
			continue
		}

		rc, err := f.Open()
		if err != nil {
			r.Close()
			return nil, 0, fmt.Errorf("opening %s in zip: %w", f.Name, err)
		}

		size := int64(f.UncompressedSize64)
		if size == 0 {
			size = f.FileInfo().Size()
		}

		return &zipReadCloser{archive: r, entry: rc}, size, nil
	}

	r.Close()
	return nil, 0, fmt.Errorf("no .img, .raw, or .wic file found in zip archive")
}
```

- [ ] **Step 3: Add `"archive/zip"` to imports** (it is already imported — verify at the top of `os_install.go`; add if missing)

- [ ] **Step 4: Run tests**

```bash
cd go && go test ./internal/cli/commands/ -run 'TestOsCachedZipPath|TestStreamZipImageEntry' -v
```

Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add go/internal/cli/commands/os_install.go
git commit -m "feat: add osCachedZipPath, zipReadCloser, streamZipImageEntry helpers"
```

---

## Task 4: Tests for updated `resolveOSImage` and new `openOSImageStream`

**Files:**
- Modify: `go/internal/cli/commands/os_install_test.go`

- [ ] **Step 1: Write failing tests**

Add to `os_install_test.go`:

```go
func TestResolveOSImage_ZipCacheHit(t *testing.T) {
	// Seed a fake zip in the cache dir by calling osCachedZipPath and writing there.
	content := []byte("fake image bytes")
	zipPath, err := osCachedZipPath("test-device", "9.9.9")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(zipPath)

	// Write a minimal zip containing a .img entry.
	f, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(f)
	fw, _ := w.Create("image.img")
	fw.Write(content) //nolint:errcheck
	w.Close()
	f.Close()

	img := &imageInfo{Version: "9.9.9", DownloadURL: "https://example.com/image.zip"}
	got, err := resolveOSImage("test-device", img)
	if err != nil {
		t.Fatalf("resolveOSImage: %v", err)
	}
	if got != zipPath {
		t.Errorf("got %q; want %q", got, zipPath)
	}
}

func TestResolveOSImage_LegacyImgCacheHit(t *testing.T) {
	imgPath, err := osCachedImagePath("test-device", "8.8.8")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(imgPath)

	if err := os.WriteFile(imgPath, []byte("legacydata"), 0o644); err != nil {
		t.Fatal(err)
	}

	img := &imageInfo{Version: "8.8.8", DownloadURL: "https://example.com/image.zip"}
	got, err := resolveOSImage("test-device", img)
	if err != nil {
		t.Fatalf("resolveOSImage: %v", err)
	}
	if got != imgPath {
		t.Errorf("got %q; want %q (legacy img cache)", got, imgPath)
	}
}

func TestOpenOSImageStream_ZipCacheHit(t *testing.T) {
	content := []byte("stream me please")
	zipPath, err := osCachedZipPath("stream-device", "7.7.7")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(zipPath)

	f, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(f)
	fw, _ := w.Create("wendyos.img")
	fw.Write(content) //nolint:errcheck
	w.Close()
	f.Close()

	img := &imageInfo{Version: "7.7.7", DownloadURL: "https://example.com/image.zip"}
	r, size, err := openOSImageStream("stream-device", img)
	if err != nil {
		t.Fatalf("openOSImageStream: %v", err)
	}
	defer r.Close()

	if size != int64(len(content)) {
		t.Errorf("size = %d; want %d", size, len(content))
	}
	got, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("reading: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Error("content mismatch")
	}
}

func TestOpenOSImageStream_LegacyImgCacheHit(t *testing.T) {
	content := []byte("old img cache data")
	imgPath, err := osCachedImagePath("legacy-device", "6.6.6")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(imgPath)

	if err := os.WriteFile(imgPath, content, 0o644); err != nil {
		t.Fatal(err)
	}

	img := &imageInfo{Version: "6.6.6", DownloadURL: "https://example.com/image.zip"}
	r, size, err := openOSImageStream("legacy-device", img)
	if err != nil {
		t.Fatalf("openOSImageStream: %v", err)
	}
	defer r.Close()

	if size != int64(len(content)) {
		t.Errorf("size = %d; want %d", size, len(content))
	}
	got, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("reading: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Error("content mismatch")
	}
}
```

- [ ] **Step 2: Run to verify failure**

```bash
cd go && go test ./internal/cli/commands/ -run 'TestResolveOSImage|TestOpenOSImageStream' -v 2>&1 | head -20
```

Expected: compile error (functions not defined).

- [ ] **Step 3: Commit test file**

```bash
git add go/internal/cli/commands/os_install_test.go
git commit -m "test: add tests for updated resolveOSImage and openOSImageStream"
```

---

## Task 5: Update `resolveOSImage` and add `openOSImageStream` + `openLocalImageStream`

**Files:**
- Modify: `go/internal/cli/commands/os_install.go`

- [ ] **Step 1: Replace the existing `resolveOSImage` function body** (currently lines 956–993)

```go
// resolveOSImage returns the path to a cached file ready for streaming.
// For zip URLs: checks legacy .img cache, then .zip cache, then downloads.
// For non-zip URLs: checks legacy .img cache, then downloads the img directly.
func resolveOSImage(deviceKey string, img *imageInfo) (string, error) {
	isZip := strings.HasSuffix(strings.ToLower(img.DownloadURL), ".zip")

	// Legacy .img cache hit (backward compat with pre-streaming caches).
	imgCached, err := osCachedImagePath(deviceKey, img.Version)
	if err != nil {
		return "", err
	}
	if info, statErr := os.Stat(imgCached); statErr == nil && info.Size() > 0 {
		fmt.Printf("Using cached image (%s)\n", imgCached)
		return imgCached, nil
	}

	if isZip {
		// Zip cache hit.
		zipCached, zipErr := osCachedZipPath(deviceKey, img.Version)
		if zipErr != nil {
			return "", zipErr
		}
		if info, statErr := os.Stat(zipCached); statErr == nil && info.Size() > 0 {
			fmt.Printf("Using cached image (%s)\n", zipCached)
			return zipCached, nil
		}
		// Cache miss: download zip, rename to zip cache path.
		downloadPath, dlErr := downloadImage(img)
		if dlErr != nil {
			return "", fmt.Errorf("downloading image: %w", dlErr)
		}
		if renameErr := os.Rename(downloadPath, zipCached); renameErr != nil {
			os.Remove(downloadPath)
			return "", fmt.Errorf("caching image: %w", renameErr)
		}
		return zipCached, nil
	}

	// Non-zip URL: download img directly and cache as .img.
	downloadPath, err := downloadImage(img)
	if err != nil {
		return "", fmt.Errorf("downloading image: %w", err)
	}
	if err := os.Rename(downloadPath, imgCached); err != nil {
		os.Remove(downloadPath)
		return "", fmt.Errorf("caching image: %w", err)
	}
	return imgCached, nil
}
```

- [ ] **Step 2: Add `openOSImageStream` and `openLocalImageStream` immediately after `resolveOSImage`**

```go
// openOSImageStream resolves the cached file for deviceKey+img, then returns
// a streaming reader over the image bytes and the total uncompressed size.
// The caller must Close the returned reader.
func openOSImageStream(deviceKey string, img *imageInfo) (io.ReadCloser, int64, error) {
	cachePath, err := resolveOSImage(deviceKey, img)
	if err != nil {
		return nil, 0, err
	}
	if strings.HasSuffix(strings.ToLower(cachePath), ".zip") {
		return streamZipImageEntry(cachePath)
	}
	f, err := os.Open(cachePath)
	if err != nil {
		return nil, 0, fmt.Errorf("opening cached image: %w", err)
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, 0, fmt.Errorf("stat cached image: %w", err)
	}
	return f, info.Size(), nil
}

// openLocalImageStream opens an arbitrary local file for streaming.
// If the path ends in .zip it finds the first image entry inside it.
// Otherwise it opens the file directly as a reader.
func openLocalImageStream(imagePath string) (io.ReadCloser, int64, error) {
	if strings.HasSuffix(strings.ToLower(imagePath), ".zip") {
		return streamZipImageEntry(imagePath)
	}
	f, err := os.Open(imagePath)
	if err != nil {
		return nil, 0, fmt.Errorf("opening image: %w", err)
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, 0, fmt.Errorf("stat image: %w", err)
	}
	return f, info.Size(), nil
}
```

- [ ] **Step 3: Delete `extractImageFromZipWithProgress`** (the full function, lines 828–919)

Remove the entire `extractImageFromZipWithProgress` function.

- [ ] **Step 4: Run tests**

```bash
cd go && go test ./internal/cli/commands/ -run 'TestResolveOSImage|TestOpenOSImageStream|TestOsCachedZipPath|TestStreamZipImageEntry' -v
```

Expected: all PASS. Build may still fail at call sites — that's fine, will fix in Task 7.

- [ ] **Step 5: Commit**

```bash
git add go/internal/cli/commands/os_install.go
git commit -m "feat: add openOSImageStream, update resolveOSImage to cache zip, delete extractImageFromZipWithProgress"
```

---

## Task 6: Update `writeImageToDisk` — macOS

**Files:**
- Modify: `go/internal/cli/commands/disklister_darwin.go`

- [ ] **Step 1: Replace `writeImageToDisk` (lines 193–237)**

```go
func writeImageToDisk(r io.Reader, totalSize int64, d drive, progressFn func(written int64)) error {
	if err := unmountDisk(d.DevicePath); err != nil {
		return err
	}

	bs := "8m"
	if d.StorageType == StorageNVMe {
		bs = "64m"
	}

	// Use rdisk for faster raw writes on macOS. Read from stdin so the
	// caller can pipe an io.Reader (e.g. a streaming zip entry) without
	// materialising the image to disk first.
	cmd := exec.Command("sudo", "dd",
		fmt.Sprintf("of=%s", d.RawPath),
		"bs="+bs,
		"status=progress",
	)
	cmd.Stdin = r

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("creating stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting dd: %w", err)
	}

	scannerDone := make(chan struct{})
	go func() {
		defer close(scannerDone)
		scanDDProgress(stderr, progressFn)
	}()

	waitErr := cmd.Wait()
	<-scannerDone

	if waitErr != nil {
		return fmt.Errorf("writing image: %w", waitErr)
	}

	// Sync to flush any remaining writes.
	exec.Command("sync").Run() //nolint:errcheck

	return nil
}
```

- [ ] **Step 2: Verify build**

```bash
cd go && go build ./internal/cli/commands/ 2>&1 | head -30
```

Expected: may show errors in other files (call sites not updated yet) but no errors in `disklister_darwin.go` itself. On Linux the darwin file won't compile — that's fine.

- [ ] **Step 3: Commit**

```bash
git add go/internal/cli/commands/disklister_darwin.go
git commit -m "feat: writeImageToDisk on macOS reads from io.Reader instead of file path"
```

---

## Task 7: Update `writeImageToDisk` — Linux

**Files:**
- Modify: `go/internal/cli/commands/disklister_linux.go`

- [ ] **Step 1: Replace `writeImageToDisk` (lines 160–202)**

Note the index shift: removing `if=<path>` from ddArgs shifts all subsequent indices by -1. `bs` is now at index 2 (was 3).

```go
func writeImageToDisk(r io.Reader, totalSize int64, d drive, progressFn func(written int64)) error {
	if err := unmountDisk(d.DevicePath); err != nil {
		return err
	}

	ddArgs := []string{
		"dd",
		fmt.Sprintf("of=%s", d.DevicePath),
		"bs=4M",
		"status=progress",
		"conv=fdatasync",
	}
	if d.StorageType == StorageNVMe {
		ddArgs[2] = "bs=64M" // index 2 = "bs=4M" (no if= arg now)
		ddArgs = append(ddArgs, "oflag=direct")
	}

	cmd := exec.Command("sudo", ddArgs...)
	cmd.Stdin = r

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("creating stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting dd: %w", err)
	}

	scannerDone := make(chan struct{})
	go func() {
		defer close(scannerDone)
		scanDDProgress(stderr, progressFn)
	}()

	waitErr := cmd.Wait()
	<-scannerDone

	if waitErr != nil {
		return fmt.Errorf("writing image: %w", waitErr)
	}

	return nil
}
```

- [ ] **Step 2: Commit**

```bash
git add go/internal/cli/commands/disklister_linux.go
git commit -m "feat: writeImageToDisk on Linux reads from io.Reader instead of file path"
```

---

## Task 8: Update `writeImageToDisk` — Windows

**Files:**
- Modify: `go/internal/cli/commands/disklister_windows.go`

- [ ] **Step 1: Change signature and remove `os.Open`**

In `writeImageToDisk` (line 252), make these two changes:

**Change the signature** from:
```go
func writeImageToDisk(imagePath string, d drive, progressFn func(written int64)) error {
```
to:
```go
func writeImageToDisk(r io.Reader, totalSize int64, d drive, progressFn func(written int64)) error {
```

**Remove the `os.Open` block** (lines 296–300):
```go
imgFile, err := os.Open(imagePath)
if err != nil {
    return fmt.Errorf("opening image: %w", err)
}
defer imgFile.Close()
```

**Replace `imgFile.Read(buf)` with `r.Read(buf)`** in the write loop (line 334):
```go
n, readErr := r.Read(buf)
```

**Also replace the error message** on the read error line from `"reading image"` — keep as-is, it still makes sense.

- [ ] **Step 2: Verify imports** — `os` is still used (`os.NewFile`, `os.File`). No import changes needed. `io.Reader` is already imported via the existing `io.EOF` usage.

- [ ] **Step 3: Commit**

```bash
git add go/internal/cli/commands/disklister_windows.go
git commit -m "feat: writeImageToDisk on Windows reads from io.Reader instead of file path"
```

---

## Task 9: Update `installLinuxImage` and `runOSInstallDirect` call sites

**Files:**
- Modify: `go/internal/cli/commands/os_install.go`

- [ ] **Step 1: Update `installLinuxImage`** — replace the block starting at "Step 5: Resolve image" (around lines 484–530)

Replace:
```go
// Step 5: Resolve image (cached or download).
fmt.Printf("\nPreparing %s %s image...\n", device.Name, selectedVersion)
imgInfo, err := getImageInfo(device.Manifest, selectedVersion)
if err != nil {
    return fmt.Errorf("getting image info: %w", err)
}

imagePath, err := resolveOSImage(deviceKey, imgInfo)
if err != nil {
    return fmt.Errorf("resolving OS image: %w", err)
}

// Get image size for progress tracking.
imgStat, err := os.Stat(imagePath)
if err != nil {
    return fmt.Errorf("stat image: %w", err)
}
totalSize := imgStat.Size()

// Step 5: Write image to drive with progress bar.
fmt.Printf("Writing image to %s...\n", targetDrive.DevicePath)
writeProg := tui.NewProgress(fmt.Sprintf("Writing to %s...", targetDrive.DevicePath))
wp := tea.NewProgram(writeProg)

go func() {
    writeErr := writeImageToDisk(imagePath, targetDrive, func(written int64) {
        if totalSize > 0 {
            wp.Send(tui.ProgressUpdateMsg{
                Percent: float64(written) / float64(totalSize),
                Written: written,
                Total:   totalSize,
            })
        }
    })
    wp.Send(tui.ProgressDoneMsg{Err: writeErr})
}()
```

With:
```go
// Step 5: Resolve image (cached or download) and open streaming reader.
fmt.Printf("\nPreparing %s %s image...\n", device.Name, selectedVersion)
imgInfo, err := getImageInfo(device.Manifest, selectedVersion)
if err != nil {
    return fmt.Errorf("getting image info: %w", err)
}

r, totalSize, err := openOSImageStream(deviceKey, imgInfo)
if err != nil {
    return fmt.Errorf("opening OS image: %w", err)
}
defer r.Close()

// Step 6: Write image to drive with progress bar.
fmt.Printf("Writing image to %s...\n", targetDrive.DevicePath)
writeProg := tui.NewProgress(fmt.Sprintf("Writing to %s...", targetDrive.DevicePath))
wp := tea.NewProgram(writeProg)

go func() {
    writeErr := writeImageToDisk(r, totalSize, targetDrive, func(written int64) {
        if totalSize > 0 {
            wp.Send(tui.ProgressUpdateMsg{
                Percent: float64(written) / float64(totalSize),
                Written: written,
                Total:   totalSize,
            })
        }
    })
    wp.Send(tui.ProgressDoneMsg{Err: writeErr})
}()
```

- [ ] **Step 2: Update `runOSInstallDirect`** — replace the write call (around line 178)

Replace:
```go
fmt.Printf("Writing image to %s...\n", targetDrive.DevicePath)
if err := writeImageToDisk(imagePath, *targetDrive, nil); err != nil {
    return fmt.Errorf("writing image: %w", err)
}
```

With:
```go
r, size, err := openLocalImageStream(imagePath)
if err != nil {
    return fmt.Errorf("opening image: %w", err)
}
defer r.Close()

fmt.Printf("Writing image to %s...\n", targetDrive.DevicePath)
if err := writeImageToDisk(r, size, *targetDrive, nil); err != nil {
    return fmt.Errorf("writing image: %w", err)
}
```

- [ ] **Step 3: Remove unused `os.Stat` import usage** — `os.Stat` is still used elsewhere in the file (e.g. in `resolveOSImage`), so no import change needed. Verify with `go build`.

- [ ] **Step 4: Build to verify all call sites compile**

```bash
cd go && go build ./... 2>&1 | head -30
```

Expected: no errors.

- [ ] **Step 5: Run all os_install tests**

```bash
cd go && go test ./internal/cli/commands/ -run 'TestOsCached|TestStream|TestResolve|TestOpen|TestNewOSInstall|TestParseWiFi|TestResolveWiFi|TestConfirm|TestProbe|TestDownload' -v 2>&1 | tail -30
```

Expected: all PASS.

- [ ] **Step 6: Commit**

```bash
git add go/internal/cli/commands/os_install.go
git commit -m "feat: update installLinuxImage and runOSInstallDirect to use openOSImageStream"
```

---

## Task 10: Update `os_download.go`

**Files:**
- Modify: `go/internal/cli/commands/os_download.go`

- [ ] **Step 1: Update the cache check to look for `.zip` first, then legacy `.img`**

Replace the current cache check block (lines 53–77):
```go
// Check if already cached.
cached, err := osCachedImagePath(selectedKey, version)
if err != nil {
    return err
}

if info, statErr := os.Stat(cached); statErr == nil && info.Size() > 0 {
    sizeMB := float64(info.Size()) / (1024 * 1024)
    cliLogln("\nImage already cached: %s (%.1f MB)", cached, sizeMB)

    if !overwrite {
        confirmed, err := tui.Confirm("Re-download and overwrite?")
        if err != nil {
            return err
        }
        if !confirmed {
            cliLogln("Keeping existing cached image.")
            return nil
        }
    }

    // Remove stale cache entry before re-downloading.
    if err := os.Remove(cached); err != nil {
        return fmt.Errorf("removing cached image: %w", err)
    }
}
```

With:
```go
// Check if already cached — zip format (new) or legacy extracted img.
cached := ""
if zipPath, zpErr := osCachedZipPath(selectedKey, version); zpErr == nil {
    if info, statErr := os.Stat(zipPath); statErr == nil && info.Size() > 0 {
        cached = zipPath
    }
}
if cached == "" {
    if imgPath, ipErr := osCachedImagePath(selectedKey, version); ipErr == nil {
        if info, statErr := os.Stat(imgPath); statErr == nil && info.Size() > 0 {
            cached = imgPath
        }
    }
}

if cached != "" {
    info, _ := os.Stat(cached)
    sizeMB := float64(info.Size()) / (1024 * 1024)
    cliLogln("\nImage already cached: %s (%.1f MB)", cached, sizeMB)

    if !overwrite {
        confirmed, err := tui.Confirm("Re-download and overwrite?")
        if err != nil {
            return err
        }
        if !confirmed {
            cliLogln("Keeping existing cached image.")
            return nil
        }
    }

    if err := os.Remove(cached); err != nil {
        return fmt.Errorf("removing cached image: %w", err)
    }
}
```

- [ ] **Step 2: Update the `Long` description** — change `"Download (and extract) a WendyOS image for a supported device."` to `"Download a WendyOS image for a supported device."`

- [ ] **Step 3: Build and test**

```bash
cd go && go build ./... && go test ./internal/cli/commands/ -v 2>&1 | grep -E 'PASS|FAIL|ok' | tail -10
```

Expected: build succeeds, all tests PASS.

- [ ] **Step 4: Commit**

```bash
git add go/internal/cli/commands/os_download.go
git commit -m "feat: os download checks zip cache first, updates long description"
```

---

## Task 11: Full test run and PR

- [ ] **Step 1: Run the full test suite**

```bash
cd go && go test ./... 2>&1 | tail -20
```

Expected: all packages PASS.

- [ ] **Step 2: Push branch**

```bash
git push -u origin jo/stream-zip-to-disk
```

- [ ] **Step 3: Create PR**

```bash
gh pr create \
  --title "feat: stream zip→disk to eliminate 59 GB extracted image temp file" \
  --body "$(cat <<'EOF'
## Summary

- `writeImageToDisk` now accepts `io.Reader + int64` instead of a file path on all three platforms (macOS, Linux, Windows)
- New `streamZipImageEntry` opens a zip and returns a streaming reader over the first `.img/.raw/.wic` entry — no temp file
- `resolveOSImage` now caches the compressed `.zip` (~5.5 GB) instead of the extracted `.img` (~59 GB); legacy `.img` caches are still read as a fallback
- New `openOSImageStream` / `openLocalImageStream` bridge the cache layer to the writer
- `extractImageFromZipWithProgress` deleted — extraction now happens inline during the write
- `os download` checks the zip cache first, then falls back to legacy img cache

## Disk usage

| Scenario | Before | After |
|---|---|---|
| First install (peak) | zip + 59 GB extracted | zip only (~5.5 GB) |
| Cache at rest | 59 GB `.img` | ~5.5 GB `.zip` |

## Test plan
- [ ] `go test ./internal/cli/commands/ -run 'TestOsCachedZipPath|TestStreamZipImageEntry|TestResolveOSImage|TestOpenOSImageStream'` — all pass
- [ ] `go test ./...` — all pass
- [ ] Manual smoke: `wendy os install --device-type raspberry-pi-5 --drive <sd> --force` on macOS/Linux — no "Extracting image…" step, writes successfully

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```
