# Parallel Image Download Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the single-stream HTTP GET in `downloadImage` with parallel range requests to saturate fast network connections, with automatic fallback to single-stream when the server doesn't support ranges.

**Architecture:** A `HEAD` probe checks for `Accept-Ranges: bytes` and `Content-Length`; on success, 8 goroutines each fetch a byte range and write directly to a pre-allocated file via `WriteAt`; a shared atomic counter drives the existing progress TUI. If the probe fails, `downloadImage` falls back to its current single-stream GET path unchanged.

**Tech Stack:** Go stdlib (`net/http`, `sync`, `sync/atomic`), `os.File.WriteAt`, `httptest` for tests.

---

## File Structure

- **Modify:** `go/internal/cli/commands/os_install.go`
  - Add `const parallelDownloadWorkers = 8`
  - Add imports `"sync"` and `"sync/atomic"`
  - Add `probeRangeSupport(client *http.Client, img *imageInfo) (int64, bool)`
  - Add `downloadChunk(client, url, start, end, dst, downloaded, total, sendProgress)`
  - Add `downloadParallel(client, url, contentLength, dst, sendProgress) error`
  - Rewrite `downloadImage` to probe then dispatch to parallel or existing single-stream

- **Modify:** `go/internal/cli/commands/os_install_test.go`
  - Add `TestProbeRangeSupport` (3 sub-tests)
  - Add `TestDownloadParallel`

---

### Task 1: Add `probeRangeSupport` with tests (TDD)

**Files:**
- Modify: `go/internal/cli/commands/os_install_test.go`
- Modify: `go/internal/cli/commands/os_install.go`

- [ ] **Step 1: Add new imports to the test file**

Open `go/internal/cli/commands/os_install_test.go`. Replace the existing import block with:

```go
import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/wendylabsinc/wendy/internal/shared/version"
)
```

- [ ] **Step 2: Write failing tests for `probeRangeSupport`**

Append to `go/internal/cli/commands/os_install_test.go`:

```go
func TestProbeRangeSupport(t *testing.T) {
	t.Run("returns content length when server supports ranges", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodHead {
				t.Errorf("expected HEAD, got %s", r.Method)
			}
			w.Header().Set("Accept-Ranges", "bytes")
			w.Header().Set("Content-Length", "8192")
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()

		img := &imageInfo{DownloadURL: srv.URL + "/image.img"}
		cl, ok := probeRangeSupport(&http.Client{}, img)
		if !ok {
			t.Fatal("expected ok=true")
		}
		if cl != 8192 {
			t.Fatalf("expected contentLength=8192, got %d", cl)
		}
	})

	t.Run("returns false when Accept-Ranges header is absent", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Length", "8192")
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()

		img := &imageInfo{DownloadURL: srv.URL + "/image.img"}
		_, ok := probeRangeSupport(&http.Client{}, img)
		if ok {
			t.Fatal("expected ok=false when no Accept-Ranges header")
		}
	})

	t.Run("falls back to img.ImageSize when Content-Length is absent", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Accept-Ranges", "bytes")
			// No Content-Length header.
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()

		img := &imageInfo{DownloadURL: srv.URL + "/image.img", ImageSize: 4096}
		cl, ok := probeRangeSupport(&http.Client{}, img)
		if !ok {
			t.Fatal("expected ok=true with ImageSize fallback")
		}
		if cl != 4096 {
			t.Fatalf("expected contentLength=4096 from ImageSize, got %d", cl)
		}
	})
}
```

- [ ] **Step 3: Run the tests and confirm they fail**

```bash
cd go && go test ./internal/cli/commands/ -run TestProbeRangeSupport -v
```

Expected: `FAIL` — `probeRangeSupport` is not yet defined.

- [ ] **Step 4: Add `probeRangeSupport` to `os_install.go`**

Add these two new imports to the import block in `go/internal/cli/commands/os_install.go`:

```go
"sync"
"sync/atomic"
```

Then add the following function anywhere in `os_install.go` (e.g. just above `downloadImage`):

```go
// probeRangeSupport issues a HEAD request to check whether the server
// supports HTTP range requests. Returns the content length and true on
// success. Falls back to img.ImageSize if Content-Length is absent.
// Returns 0, false if ranges are unsupported or content length is unknown.
func probeRangeSupport(client *http.Client, img *imageInfo) (contentLength int64, ok bool) {
	resp, err := client.Head(img.DownloadURL)
	if err != nil {
		return 0, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, false
	}
	if resp.Header.Get("Accept-Ranges") != "bytes" {
		return 0, false
	}
	cl := resp.ContentLength
	if cl <= 0 && img.ImageSize > 0 {
		cl = img.ImageSize
	}
	if cl <= 0 {
		return 0, false
	}
	return cl, true
}
```

- [ ] **Step 5: Run the tests and confirm they pass**

```bash
cd go && go test ./internal/cli/commands/ -run TestProbeRangeSupport -v
```

Expected: all 3 sub-tests `PASS`.

- [ ] **Step 6: Commit**

```bash
git add go/internal/cli/commands/os_install.go go/internal/cli/commands/os_install_test.go
git commit -m "feat: add probeRangeSupport for parallel download probe"
```

---

### Task 2: Add `downloadChunk` and `downloadParallel` with tests (TDD)

**Files:**
- Modify: `go/internal/cli/commands/os_install_test.go`
- Modify: `go/internal/cli/commands/os_install.go`

- [ ] **Step 1: Add `bytes` to the test file imports**

In `go/internal/cli/commands/os_install_test.go`, add `"bytes"` to the import block:

```go
import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/wendylabsinc/wendy/internal/shared/version"
)
```

- [ ] **Step 2: Write the failing test for `downloadParallel`**

Append to `go/internal/cli/commands/os_install_test.go`:

```go
func TestDownloadParallel(t *testing.T) {
	// 8 KiB fixture — with 8 workers each gets a 1 KiB chunk.
	fixture := make([]byte, 8*1024)
	for i := range fixture {
		fixture[i] = byte(i % 251) // prime modulus gives a non-trivial pattern
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rangeHeader := r.Header.Get("Range")
		if rangeHeader == "" {
			http.Error(w, "range required", http.StatusBadRequest)
			return
		}
		var start, end int64
		if _, err := fmt.Sscanf(rangeHeader, "bytes=%d-%d", &start, &end); err != nil {
			http.Error(w, "bad range header", http.StatusBadRequest)
			return
		}
		if end >= int64(len(fixture)) {
			end = int64(len(fixture)) - 1
		}
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(fixture)))
		w.Header().Set("Content-Length", strconv.FormatInt(end-start+1, 10))
		w.WriteHeader(http.StatusPartialContent)
		w.Write(fixture[start : end+1]) //nolint:errcheck
	}))
	defer srv.Close()

	dir := t.TempDir()
	f, err := os.CreateTemp(dir, "wendy-test-*.img")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())

	contentLength := int64(len(fixture))
	if err := f.Truncate(contentLength); err != nil {
		t.Fatal(err)
	}

	var progressCalled bool
	err = downloadParallel(&http.Client{}, srv.URL+"/image.img", contentLength, f, func(downloaded, total int64) {
		progressCalled = true
	})
	if err != nil {
		t.Fatalf("downloadParallel: %v", err)
	}
	if !progressCalled {
		t.Error("progress callback was never called")
	}

	f.Close()

	got, err := os.ReadFile(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, fixture) {
		t.Errorf("content mismatch: got %d bytes, want %d bytes", len(got), len(fixture))
		for i := range fixture {
			if i >= len(got) || got[i] != fixture[i] {
				t.Errorf("first diff at byte %d: got %d, want %d", i, got[i], fixture[i])
				break
			}
		}
	}
}
```

- [ ] **Step 3: Run the test and confirm it fails**

```bash
cd go && go test ./internal/cli/commands/ -run TestDownloadParallel -v
```

Expected: `FAIL` — `downloadParallel` is not yet defined.

- [ ] **Step 4: Add `parallelDownloadWorkers`, `downloadChunk`, and `downloadParallel` to `os_install.go`**

Add after the existing `const` declarations (or at the top of the non-`const` area) in `go/internal/cli/commands/os_install.go`:

```go
const parallelDownloadWorkers = 8
```

Then add these two functions just above `downloadImage`:

```go
// downloadChunk fetches the byte range [start, end] from url, writes it to dst
// at the correct offset via WriteAt, and atomically increments *downloaded.
func downloadChunk(client *http.Client, url string, start, end int64, dst *os.File, downloaded *int64, total int64, sendProgress func(int64, int64)) error {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", start, end))

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("range request %d-%d: %w", start, end, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusPartialContent {
		return fmt.Errorf("range request %d-%d: expected 206, got %d", start, end, resp.StatusCode)
	}

	buf := make([]byte, 1*1024*1024)
	offset := start
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, writeErr := dst.WriteAt(buf[:n], offset); writeErr != nil {
				return fmt.Errorf("writing at offset %d: %w", offset, writeErr)
			}
			offset += int64(n)
			newTotal := atomic.AddInt64(downloaded, int64(n))
			sendProgress(newTotal, total)
		}
		if readErr == io.EOF {
			return nil
		}
		if readErr != nil {
			return fmt.Errorf("reading chunk %d-%d: %w", start, end, readErr)
		}
	}
}

// downloadParallel downloads url into dst using parallelDownloadWorkers concurrent
// range requests. dst must already be truncated to contentLength bytes.
func downloadParallel(client *http.Client, url string, contentLength int64, dst *os.File, sendProgress func(int64, int64)) error {
	chunkSize := (contentLength + parallelDownloadWorkers - 1) / parallelDownloadWorkers

	var wg sync.WaitGroup
	errCh := make(chan error, parallelDownloadWorkers)
	var downloaded int64

	for i := 0; i < parallelDownloadWorkers; i++ {
		start := int64(i) * chunkSize
		if start >= contentLength {
			break
		}
		end := start + chunkSize - 1
		if end >= contentLength {
			end = contentLength - 1
		}

		wg.Add(1)
		go func(start, end int64) {
			defer wg.Done()
			if err := downloadChunk(client, url, start, end, dst, &downloaded, contentLength, sendProgress); err != nil {
				errCh <- err
			}
		}(start, end)
	}

	wg.Wait()
	close(errCh)
	for err := range errCh {
		return err
	}
	return nil
}
```

- [ ] **Step 5: Run the test and confirm it passes**

```bash
cd go && go test ./internal/cli/commands/ -run TestDownloadParallel -v
```

Expected: `PASS`.

- [ ] **Step 6: Run the full test suite to confirm nothing is broken**

```bash
cd go && go test ./internal/cli/commands/ -v 2>&1 | tail -20
```

Expected: all existing tests still `PASS`.

- [ ] **Step 7: Commit**

```bash
git add go/internal/cli/commands/os_install.go go/internal/cli/commands/os_install_test.go
git commit -m "feat: add downloadChunk and downloadParallel helpers"
```

---

### Task 3: Rewrite `downloadImage` to use parallel path with fallback

**Files:**
- Modify: `go/internal/cli/commands/os_install.go`

- [ ] **Step 1: Replace `downloadImage` with the parallel-capable version**

In `go/internal/cli/commands/os_install.go`, find and replace the entire `downloadImage` function (lines 577–649 in the current file). The new version is:

```go
// downloadImage downloads an OS image to a temp file with a progress bar.
// If the server supports HTTP range requests, it downloads in parallel using
// parallelDownloadWorkers concurrent connections. Falls back to a single
// sequential stream otherwise.
func downloadImage(img *imageInfo) (string, error) {
	client := &http.Client{Timeout: 30 * time.Minute}

	// Write directly into the OS cache directory so we never land in /tmp
	// (which is often a size-limited tmpfs on Linux).
	cacheDir, err := osCacheDir()
	if err != nil {
		return "", fmt.Errorf("resolving cache dir: %w", err)
	}
	tmpFile, err := os.CreateTemp(cacheDir, "wendyos-*.img")
	if err != nil {
		return "", fmt.Errorf("creating temp file: %w", err)
	}

	prog := tui.NewProgress(fmt.Sprintf("Downloading %s...", img.Version))
	p := tea.NewProgram(prog)
	sendProgress := throttledProgress(p, 33*time.Millisecond)

	contentLength, supportsRanges := probeRangeSupport(client, img)

	if supportsRanges {
		if err := tmpFile.Truncate(contentLength); err != nil {
			tmpFile.Close()
			os.Remove(tmpFile.Name())
			return "", fmt.Errorf("pre-allocating: %w", err)
		}
		go func() {
			p.Send(tui.ProgressDoneMsg{Err: downloadParallel(client, img.DownloadURL, contentLength, tmpFile, sendProgress)})
		}()
	} else {
		go func() {
			resp, err := client.Get(img.DownloadURL)
			if err != nil {
				p.Send(tui.ProgressDoneMsg{Err: fmt.Errorf("downloading: %w", err)})
				return
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				p.Send(tui.ProgressDoneMsg{Err: fmt.Errorf("download returned status %d", resp.StatusCode)})
				return
			}
			total := resp.ContentLength
			if img.ImageSize > 0 {
				total = img.ImageSize
			}
			buf := make([]byte, 1*1024*1024)
			var downloaded int64
			for {
				n, readErr := resp.Body.Read(buf)
				if n > 0 {
					if _, writeErr := tmpFile.Write(buf[:n]); writeErr != nil {
						p.Send(tui.ProgressDoneMsg{Err: writeErr})
						return
					}
					downloaded += int64(n)
					sendProgress(downloaded, total)
				}
				if readErr == io.EOF {
					p.Send(tui.ProgressDoneMsg{})
					return
				}
				if readErr != nil {
					p.Send(tui.ProgressDoneMsg{Err: readErr})
					return
				}
			}
		}()
	}

	finalModel, err := p.Run()
	if err != nil {
		tmpFile.Close()
		os.Remove(tmpFile.Name())
		return "", fmt.Errorf("progress TUI: %w", err)
	}

	model := finalModel.(tui.ProgressModel)
	if model.Err() != nil {
		tmpFile.Close()
		os.Remove(tmpFile.Name())
		return "", model.Err()
	}

	tmpFile.Close()
	return tmpFile.Name(), nil
}
```

- [ ] **Step 2: Compile to confirm no errors**

```bash
cd go && go build ./internal/cli/commands/
```

Expected: exits with code 0, no output.

- [ ] **Step 3: Run the full test suite**

```bash
cd go && go test ./internal/cli/commands/ -v 2>&1 | tail -30
```

Expected: all tests `PASS`, including the new `TestProbeRangeSupport` and `TestDownloadParallel`.

- [ ] **Step 4: Commit**

```bash
git add go/internal/cli/commands/os_install.go
git commit -m "feat: parallel HTTP range downloads in downloadImage"
```
