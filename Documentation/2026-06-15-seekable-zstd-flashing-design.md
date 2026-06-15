# Seekable-zstd flashing: skip holes without decompressing them

Date: 2026-06-15
Branch: `jo/bmap-flash` (PR #1036)
Status: Design — pending review

## Problem

Flashing a WendyOS image is slow on zero-heavy ("holey") images even when the
block map (bmap) is engaged. bmap already eliminates the *write* I/O for holes:
the writer only `WriteAt`s mapped ranges. But the parent process still
gzip-decompresses the **entire** uncompressed image — holes included — because
it streams the decompressed bytes through a pipe to the writer, and a gzip pipe
is not seekable. To advance the stream to the next mapped range, every hole byte
must be produced by the (single-threaded) gzip decoder and then discarded.

For a typical Jetson image (~19 GB uncompressed, ~4 GB of real mapped data),
that means decoding ~15 GB of zeros we immediately throw away. Single-threaded
gzip decode (~150–250 MB/s) of the full image is the wall-clock bottleneck.

**Root cause:** holes cost decompression, not write I/O, and a gzip pipe cannot
skip them.

## Goal

Make holes genuinely free on every flash (including the first) by decoding
**only the bytes that bmap marks as mapped**, never materializing hole bytes.

Non-goals: changing the bmap format; changing the non-bmap (`dd`) path; keeping
the published image flashable by generic third-party tools (Etcher/dd/RPi
Imager) — the optimized artifact is wendy-specific.

## Approach: seekable zstd

Publish the image compressed with **zstd in the seekable format** (a sequence of
independent ~4 MB-uncompressed frames plus an embedded seek table). The seek
table makes the *decompressed* image randomly addressable via `io.ReaderAt`. The
CLI then decodes only the frames that overlap mapped ranges and skips pure-hole
frames entirely.

Why this and not alternatives (recorded for posterity):

- **Plain zstd, keep streaming:** ~4× faster decode but still decodes holes.
  Rejected as the end state (kept conceptually as the codec half of this work).
- **Raw decompressed cache + `ReadAt`:** makes holes free on *repeat* flashes
  only, costs a full ~16–20 GB/image on disk, first flash unchanged. Rejected.
- **Drop compression (raw artifact):** decode of the *mapped* data is only ~5 s;
  shipping it raw multiplies download (~4 GB vs ~1.5 GB) and cache disk (~10×)
  to save those seconds. Net slower once download/disk are counted. Rejected.
- **Raw + bmap-driven HTTP Range GETs:** simpler (no codec), skips holes on the
  network too, but downloads mapped data uncompressed; a win only on fast/LAN
  links. Rejected as default; noted as a possible future option.

Expected result for the example image: decode ~4 GB instead of ~19 GB, at zstd
speed — roughly **15–20× faster wall-clock** on holey images. Download stays
~1.5 GB; cache disk stays ~1.5 GB.

## Architecture

### Build side (meta-wendyos)

- Compress each published image as seekable zstd → `<image>.img.zst`, frame size
  **4 MiB uncompressed** (balances seek-table size against worst-case wasted
  decode per range; tunable).
- Continue publishing the existing `.bmap` unchanged.
- The seek table is embedded in the `.zst` file (a trailing skippable frame), so
  the artifact is self-contained — no separate index sidecar.
- A small in-repo Go encoder (`cmd/`, reusing the same seekable library as the
  reader so writer/reader cannot drift) produces the artifact; the Yocto
  publish step invokes it. Build-side details (recipe wiring) are out of scope
  for this CLI spec beyond "publish `.img.zst` + `.bmap`".

### Manifest

- Add a per-version field `zst_path` (sibling to `path`/`bmap_path`). When
  present, the CLI derives `ZstURL = gcsBaseURL + "/" + zst_path`.
- Absent `zst_path` → CLI uses the existing gzip + bmap/`dd` path unchanged.
  This is the rollout/back-compat seam: old manifests and partially-published
  versions keep working.

### CLI side

Data flow when `zst_path` is present and `--no-bmap` is not set:

1. Download `.img.zst` and `.bmap` (existing atomic-download + traversal-safe
   cache machinery; `.img.zst` cached like other images).
2. Open a **seekable reader** exposing `ReadAt(p, off)` and `Size()` over the
   decompressed image (wrapping `klauspost/compress/zstd` + the seekable seek
   table). `Size()` and the bmap's `ImageSize` must agree; mismatch → fall back
   (same spirit as the existing size-mismatch guard).
3. Run the **frame-walk** (below) to write only mapped ranges.

If `zst_path` is absent, or any step fails before writing begins, fall back to
the current path. Once writing to the device has begun, a failure is fatal (no
silent fallback that would leave a half-written disk).

## Components / where the code lands

All paths under `go/internal/cli/commands/` unless noted.

- `seekable_zstd.go` (new): the seekable reader — `ReaderAt`/`Size` over the
  decompressed image, plus frame iteration (`forEachFrame`/offset→frame lookup).
- `bmap.go`: replace `applyBmap`'s stream-and-discard loop with the frame-walk
  driven by an `io.ReaderAt` source. Keep `parseBmap` and per-range SHA256
  verification. `discard()` becomes unused for this path and is removed if no
  other caller remains.
- `bmap_writer.go` / `root.go`: `__bmap-write` gains `--source <.img.zst>`; the
  helper opens the seekable reader and runs the frame-walk itself as root,
  emitting a newline-delimited cumulative-bytes-written counter on stdout.
  The stdin pipe for this path is removed. **Add path validation** for
  `--device` and `--source` (the defense-in-depth item now load-bearing).
- `disklister_{linux,darwin}.go`: `writeImageWithBmap` re-execs the helper with
  `--source` and scans the helper's stdout for progress instead of feeding
  stdin.
- `disklister_windows.go`: no helper — run the frame-walk in-process against the
  locked disk handle, source = seekable reader over the cached `.img.zst`.
- `os_install.go`: prefer `zst_path`; thread the seekable source through;
  **skip the one-time gzip "measure size" pass when a bmap is present** — the
  bmap's `ImageSize` already gives the exact uncompressed total for the bar.
- `manifest.go`: add `zst_path` parsing and `ZstURL` on `imageInfo`.

### Dependency

Add `github.com/SaveTheRbtz/zstd-seekable-format-go` (reader + writer), layered
on the already-vendored `github.com/klauspost/compress`. Used by both the CLI
reader and the in-repo build encoder.

## Frame-walk algorithm

Input: ordered mapped ranges from the bmap (byte offsets, derived from
block-index ranges × block size, clamped to `ImageSize`); an `io.ReaderAt`
seekable source; the device `io.WriterAt`; `progressFn`.

```
for each frame [fstart, fend) in increasing offset order:
    if frame overlaps no mapped range:
        skip            # never decode — the hole savings
        continue
    decode frame once into buf
    for each mapped sub-span [s, e) within this frame:
        dst.WriteAt(buf[s-fstart : e-fstart], s)
        feed buf[s-fstart : e-fstart] into the current range's SHA256
        progressFn(cumulative mapped bytes written)
    finalize SHA256 for any range that ends within this frame; mismatch -> abort
```

Properties:

- Each needed frame is decoded exactly once, in order (no redundant re-decodes
  from many small seeks); pure-hole frames are never decoded.
- A bmap range may span multiple frames; its running hash carries across frame
  boundaries and is finalized when the range completes — preserving today's
  per-range verification guarantee exactly.
- Progress reflects cumulative mapped bytes written (matches the existing bar's
  semantics; total = sum of mapped range sizes, known up front from the bmap).

## Error handling & fallback

- Missing `zst_path`, seekable-open failure, or `Size()` ≠ bmap `ImageSize`
  *before* writing begins → print a `Note:` and fall back to the existing
  gzip + bmap/`dd` path (consistent with current fallback style).
- `--no-bmap` forces the legacy path (a `.img.zst` with no usable bmap offers no
  benefit; we do not add a hole-less seekable mode).
- Checksum mismatch, short write, decode error, or helper non-zero exit *during*
  writing → fatal, surfaced with the helper's stderr (as today). No silent
  fallback mid-flash.
- Helper path validation: reject a `--device` that is not a block/char device
  and a `--source` that escapes the expected cache root.

## Testing

- `bmap_test.go`: extend with frame-walk cases against an in-memory
  `ReaderAt` — holes at start/middle/end, ranges spanning frame boundaries,
  range exactly on a frame boundary, single-block ranges, checksum-mismatch
  abort, full-hole image, fully-mapped image. Shrink frame size in tests to
  exercise multi-frame ranges (mirrors the existing `bmapChunkSize` test hook).
- `seekable_zstd_test.go` (new): round-trip — encode a known buffer seekable,
  then `ReadAt` random offsets/lengths and assert bytes; frame-boundary reads;
  `Size()` correctness.
- Manifest test: `zst_path` present → `ZstURL` set; absent → empty, legacy path.
- An end-to-end test that flashes a small synthetic holey image to a temp file
  (not a real device) via the frame-walk and byte-compares against the expected
  reconstruction.

## Rollout

1. Land CLI support gated on `zst_path` (no-op until manifests carry it).
2. Build publishes `.img.zst` for new image versions.
3. CLI prefers `.img.zst` when present; older versions keep using gzip.
No flag day; the manifest field is the switch.
