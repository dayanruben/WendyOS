# WendyOS Image Download Speed Improvement

**Date:** 2026-05-05  
**Status:** Approved

## Problem

`downloadImage` in `os_install.go` uses a single sequential HTTP GET stream. For large WendyOS images (multi-GB), a single TCP connection cannot saturate a fast network connection, making downloads slow on all platforms — particularly noticeable on Windows.

The download server (GCS) supports HTTP `Range` requests and `Accept-Ranges: bytes`.

## Goal

Reduce image download time by parallelising the HTTP download into multiple concurrent range requests, without changing any user-facing CLI flags or output.

## Architecture

The change is entirely within `downloadImage` in `go/internal/cli/commands/os_install.go`. No new files, no new dependencies.

### Probe phase

Issue a `HEAD` request to the download URL to obtain `Content-Length` and confirm the `Accept-Ranges: bytes` response header. If `Accept-Ranges` is absent, fall through to the existing single-stream GET path unchanged. For the content length, prefer the `HEAD` response value; fall back to `img.ImageSize` from the manifest if the response omits it. If neither is available (≤ 0), fall through to single-stream.

### Allocation phase

`os.Create` the temp file (same `osCacheDir()` location as today), then `file.Truncate(contentLength)` to pre-allocate the full file size. Pre-allocation lets each goroutine write to its own byte range via `WriteAt` without coordination.

### Download phase

Spawn `parallelDownloadWorkers = 8` goroutines (package-level constant, not a user flag). Each goroutine:

1. Computes its byte range: `start = i * chunkSize`, `end = start + chunkSize - 1` (last worker uses `totalSize - 1` as end to absorb the remainder).
2. Issues `GET` with `Range: bytes=start-end`.
3. Expects a `206 Partial Content` response; any other status is an error.
4. Reads in 1 MiB chunks and writes via `file.WriteAt(buf[:n], offset)`.
5. Atomically increments a shared `downloaded int64` via `atomic.AddInt64` after each chunk and calls the throttled progress sender.

### Progress

The existing `throttledProgress` helper and Bubble Tea progress TUI are reused without modification. Progress updates are driven by each worker's writes and aggregate naturally via the atomic counter.

### Fallback

If the probe `HEAD` reveals no range support, `downloadImage` falls through to the original single-stream implementation. The caller's existing cache and cleanup logic is unaffected in both paths.

## Error Handling

- Non-206 response from any worker → worker sends error to a buffered error channel.
- After all workers finish (via `sync.WaitGroup`), the main goroutine drains the error channel and returns the first error encountered.
- On any error: temp file is closed and removed, same as the current single-stream path.
- Chunk boundary calculation: `Range` header end is inclusive; the last chunk's end is always `totalSize - 1` regardless of division remainder.

## Constants

```go
const parallelDownloadWorkers = 8
```

Not configurable via flags. 8 concurrent connections is a sensible default that saturates typical broadband without overwhelming the server.

## Testing

New tests in `go/internal/cli/commands/os_install_test.go` using `httptest.Server`:

1. **Parallel path**: server responds to `HEAD` with `Content-Length` and `Accept-Ranges: bytes`; responds to range `GET`s with `206 Partial Content` slices of a known fixture. Assert output file matches fixture exactly.
2. **Fallback path**: server responds to `HEAD` without `Accept-Ranges` (or returns `200` for range requests). Assert output file still matches fixture via the single-stream path.

No changes to existing tests.

## Files Changed

- `go/internal/cli/commands/os_install.go` — rewrite `downloadImage` to add parallel path with fallback
- `go/internal/cli/commands/os_install_test.go` — add two new test cases for parallel and fallback paths
