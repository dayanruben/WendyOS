# Stream Zip → Disk: Eliminate Extracted Image Temp File

**Date:** 2026-05-05  
**Branch:** jo/stream-zip-to-disk

## Problem

When installing WendyOS from a `.zip` image, the current flow extracts the compressed archive to a full-size `.img` file before flashing. A typical image is ~5.5 GB compressed and ~59 GB uncompressed. This means:

- The 59 GB extracted file must fit on the user's disk alongside the downloaded zip.
- The cache then permanently stores a 59 GB `.img` file.
- Users with less than ~65 GB free disk space cannot install at all.

## Goal

Eliminate the extracted `.img` file entirely. Zip decompression happens inline as bytes flow from the zip reader into the disk writer. Peak disk usage drops to the size of the compressed zip (~5.5 GB).

## Chosen Approach: Cache Zip, Stream on Each Use

- The compressed `.zip` replaces the extracted `.img` as the cached artifact.
- On flash: open the cached (or freshly downloaded) zip, locate the image entry (`.img`/`.raw`/`.wic`), stream its reader directly to the disk writer.
- The 59 GB image is never written to disk — it flows through memory.

## Architecture

### `writeImageToDisk` — all three platforms

**Current signature:** `writeImageToDisk(imagePath string, d drive, progressFn func(written int64)) error`

**New signature:** `writeImageToDisk(r io.Reader, totalSize int64, d drive, progressFn func(written int64)) error`

- **macOS / Linux:** Remove `if=<path>` from the `dd` argument list. Set `cmd.Stdin = reader`. `dd status=progress` continues reporting bytes written to stderr; progress parsing is unchanged. The total for the progress bar comes from `totalSize` (uncompressed size from zip metadata).
- **Windows:** Already pure-Go. Replace `os.Open(imagePath)` with the passed-in reader; write loop is otherwise identical.

### `resolveOSImage` → `openOSImageStream`

**New signature:** `openOSImageStream(deviceKey string, img *imageInfo) (io.ReadCloser, int64, error)`

Returns a reader positioned at the first byte of the image entry, plus the uncompressed size. The caller must `Close()` when done.

**Logic:**

1. **Legacy img cache hit** (`<device>-<version>.img` exists): open the file, return `(file, file.Size(), nil)`. Provides backward compatibility; old caches work until they age out.
2. **Zip cache hit** (`<device>-<version>.zip` exists): open zip, locate image entry, return entry reader + `f.UncompressedSize64`.
3. **Cache miss**: download zip to temp file → rename to `<device>-<version>.zip` cache path → open zip → return entry reader + uncompressed size.

Cache path helpers:
- `osCachedZipPath(deviceKey, version)` → `<cache>/os-images/<device>-<version>.zip`
- `osCachedImagePath` retained for the legacy fallback only.

### Non-zip downloads

If `img.DownloadURL` does not end in `.zip`, behaviour is unchanged: download the img directly, cache as `.img`, open as a reader.

### `extractImageFromZipWithProgress` — deleted

No longer needed. The zip entry is streamed directly to the disk writer.

### `downloadImage`

No signature change. It continues returning a temp file path. The caller (`openOSImageStream`) renames the temp to `<device>-<version>.zip` instead of extracting it. This is a one-line change at the call site.

### `runOSInstallDirect` (local file path mode)

The caller supplies an arbitrary local file path — not necessarily a cached zip. A new helper `streamZipImageEntry(zipPath string) (io.ReadCloser, int64, error)` opens a zip and returns the first `.img`/`.raw`/`.wic` entry reader + uncompressed size. `openOSImageStream` reuses this helper internally.

```
if strings.HasSuffix(strings.ToLower(path), ".zip") {
    r, size = streamZipImageEntry(path)
} else {
    f = os.Open(path); r, size = f, stat.Size()
}
writeImageToDisk(r, size, drive, progressFn)
```

## Progress Bars

| Phase | Before | After |
|---|---|---|
| Download | "Downloading…" progress bar | unchanged |
| Extract | "Extracting image…" progress bar | **removed** |
| Write | "Writing to disk…" progress bar | unchanged (total = uncompressed size from zip metadata) |

## Error Handling

- If the zip contains no `.img`/`.raw`/`.wic` entry: error before any write begins (same as current `extractImageFromZipWithProgress`).
- If the zip entry reader errors mid-write: `dd` / the Windows write loop returns an error; the partially-written drive is treated as a failed flash (same as today).
- Temp zip file is removed on download error (same as current temp img cleanup).

## Disk Usage Comparison

| Scenario | Before | After |
|---|---|---|
| First install (peak) | zip + 59 GB extracted | zip only (~5.5 GB) |
| Cache at rest | 59 GB `.img` | ~5.5 GB `.zip` |
| Re-flash (cache hit) | 59 GB read from disk | zip streamed (~5.5 GB read) |

## Files Changed

- `go/internal/cli/commands/os_install.go` — main logic: new `openOSImageStream`, updated `resolveOSImage` callsites, delete `extractImageFromZipWithProgress`, update `runOSInstallDirect`
- `go/internal/cli/commands/disklister_darwin.go` — `writeImageToDisk` accepts reader
- `go/internal/cli/commands/disklister_linux.go` — `writeImageToDisk` accepts reader
- `go/internal/cli/commands/disklister_windows.go` — `writeImageToDisk` accepts reader

## Out of Scope

- Parallel decompression (zip entries are not splittable for parallel reads).
- Cache eviction / size management.
- Streaming download directly into the zip reader without a cache file (would require re-downloading on every flash).
