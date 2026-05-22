# OTel Disk Buffer & Cloud Flush

**Date:** 2026-05-22
**Status:** Approved

## Problem

The `TelemetryBroadcaster` is a pure in-memory pub-sub. It caches only 20 log batches and
the latest-per-key metric value. Any telemetry produced when no CLI client is connected, or
when the network is unavailable, is silently dropped. This means:

- `wendy device logs` shows no history when first connected or when using a reversed tunnel.
- Network outages lose telemetry permanently.
- Nothing is ever forwarded to cloud today, despite `RemoteLoggingService.WriteLogEntries`
  existing in the proto.

## Goals

1. Buffer all OTel signals (logs, metrics, traces) to disk on the device.
2. Flush buffered data to cloud continuously (write-ahead log model); disk absorbs outages.
3. `wendy device logs` (and the equivalent metrics/traces streams) can request recent history
   via a `last_n` parameter so reconnecting clients and reversed-tunnel sessions see context.
4. A network outage must not lose telemetry; data is safe until the cloud confirms receipt.

## Non-Goals

- Cloud upload of metrics and traces (logs only in this iteration; metrics/traces are buffered
  to disk but cloud upload is deferred).
- Time-based retention (size-based only).
- Compression of segment files.

---

## Architecture

```
OTEL SDK / containers / agent logger
          │
          ▼
  TelemetryBuffer  ──────────  segment files on disk
     (new)                     /var/lib/wendy-agent/telemetry/
          │                    logs-000001.bin  …
          │                    metrics-000001.bin
          │                    traces-000001.bin
          │                    cursor.json
          ▼
  TelemetryBroadcaster
  (unchanged internal fan-out)
          │
     ┌────┴────┐
     ▼         ▼
 CLI subs   CloudFlusher (new)
               │
               ▼
          RemoteLoggingService.WriteLogEntries
```

`TelemetryBuffer` wraps `TelemetryBroadcaster`. Every `Publish*` call writes to disk first,
then calls through to the broadcaster. All existing code that calls `broadcaster.Publish*`
is updated to call `buffer.Publish*` instead.

---

## Segment File Format

**Directory:** `/var/lib/wendy-agent/telemetry/` (overridable via `WENDY_TELEMETRY_DIR`)

**File naming:** `<signal>-<seq6>.bin`, e.g. `logs-000001.bin`, `metrics-000003.bin`.
Sequence numbers are zero-padded to six digits and increment monotonically per signal type.

**Entry encoding:** each entry is a length-prefixed protobuf frame:

```
[4 bytes big-endian uint32: payload length][payload bytes]
```

The payload is the serialised protobuf for that signal type:
- logs   → `opentelemetry.proto.collector.logs.v1.ExportLogsServiceRequest`
- metrics → `opentelemetry.proto.collector.metrics.v1.ExportMetricsServiceRequest`
- traces  → `opentelemetry.proto.collector.trace.v1.ExportTraceServiceRequest`

**Cursor file:** `cursor.json` alongside the segment files:

```json
{
  "logs":    {"file": "logs-000003.bin",    "offset": 8192},
  "metrics": {"file": "metrics-000001.bin", "offset": 0},
  "traces":  {"file": "traces-000001.bin",  "offset": 0}
}
```

The cursor records the byte offset in the named file up to which data has been confirmed
delivered to cloud. Everything from that offset onward is pending.

---

## TelemetryBuffer

**File:** `internal/agent/services/telemetry_buffer.go`

```go
type TelemetryBufferConfig struct {
    Dir            string // default /var/lib/wendy-agent/telemetry
    MaxTotalBytes  int64  // default 100 MB
    SegmentBytes   int64  // default 4 MB
}

type TelemetryBuffer struct { ... }

func NewTelemetryBuffer(cfg TelemetryBufferConfig, broadcaster *TelemetryBroadcaster) (*TelemetryBuffer, error)

func (b *TelemetryBuffer) PublishLogs(req *otelpb.ExportLogsServiceRequest)
func (b *TelemetryBuffer) PublishMetrics(req *otelpb.ExportMetricsServiceRequest)
func (b *TelemetryBuffer) PublishTraces(req *otelpb.ExportTraceServiceRequest)

// ReadLastN reads up to n entries of the given signal type from the newest
// segments backwards. Used to replay history to a new StreamLogs subscriber.
func (b *TelemetryBuffer) ReadLastN(signal SignalType, n int) ([]proto.Message, error)
```

**Startup behaviour:**
1. Create directory if absent.
2. Scan all segment files; sum sizes.
3. Evict oldest segments (by sequence number) until total size ≤ `MaxTotalBytes`.
4. Open the highest-sequence segment of each type for appending (or create sequence 000001).
5. Load `cursor.json` if present; initialise to `{file: "", offset: 0}` per signal otherwise.

**Write path (per `Publish*`):**
1. Serialise the protobuf to bytes.
2. Under a per-signal write mutex:
   a. If `active_size + 4 + len(bytes) > SegmentBytes`, seal the current segment and open
      the next sequence number. If total dir size now exceeds cap, evict the oldest.
   b. Write `[uint32 big-endian len][bytes]` to the active segment file.
3. Call `broadcaster.Publish*` unconditionally (even if step 2 failed).

**Disk write failures** are logged and silently dropped; they must not block the publish
path or crash the agent.

**ReadLastN:**
- Walk segment files for the signal from newest to oldest.
- For each file, scan all entries into a slice (newest file read from EOF backwards using
  a two-pass approach: first pass collects offsets, second reads payloads).
- Collect until n entries accumulated, then return in ascending time order.

---

## CloudFlusher

**File:** `internal/agent/services/cloud_flusher.go`

```go
type CloudFlusher struct { ... }

func NewCloudFlusher(
    logger *zap.Logger,
    buffer *TelemetryBuffer,
    broadcaster *TelemetryBroadcaster,
    provisioningSvc *ProvisioningService,
) *CloudFlusher

func (f *CloudFlusher) Run(ctx context.Context)
```

**Lifecycle:**

1. On `Run`, wait until the device is provisioned (polls `provisioningSvc.ProvisioningInfo()`).
2. Establish mTLS connection to `RemoteLoggingService` using the provisioned credentials.
3. **Catch-up + live phase (unified read loop):** the flusher always reads from the segment
   files starting at the cursor position. It reads in batches (max 200 entries per RPC),
   calls `WriteLogEntries`, and advances the cursor on each confirmed response. When it
   reaches the end of the latest segment it sleeps briefly (100 ms) before polling again —
   `TelemetryBuffer` will have written new frames by then. This keeps the cursor strictly
   tied to file offsets and avoids any correlation between in-memory channel positions and
   on-disk positions.
4. On any RPC error: back off (1s → 2s → 4s → … capped at 60s), then restart the read
   loop from the cursor position (which has not advanced on failure).

**Metrics and traces:** the flusher writes them to disk via `TelemetryBuffer` but does not
upload them to cloud in this iteration. The cursor for metrics/traces remains at zero.

**Eviction-during-read:** if a segment file is evicted while the flusher is reading it,
`os.ErrNotExist` is caught and the cursor is advanced to the start of the next segment.

---

## Protocol Changes

### `StreamLogsRequest` (and metrics/traces equivalents)

Add to `Proto/wendy/agent/services/v1/wendy_agent_v1_telemetry_service.proto`:

```protobuf
message StreamLogsRequest {
    optional string service_name = 1;
    optional int32  min_severity = 2;
    optional string app_name     = 3;
    optional int32  last_n       = 4;  // replay last N log batches before live stream
}

message StreamLogsResponse {
    opentelemetry.proto.collector.logs.v1.ExportLogsServiceRequest logs = 1;
    bool is_history = 2;  // true for replayed records, false for live
}
```

Same `last_n` / `is_history` additions to `StreamMetricsRequest/Response` and
`StreamTracesRequest/Response`. Identical changes in the v2 proto.

### `TelemetryService.StreamLogs` implementation change

```
if req.LastN > 0 {
    entries := buffer.ReadLastN(SignalLogs, int(req.LastN))
    for _, e := range entries {
        stream.Send(&StreamLogsResponse{Logs: e, IsHistory: true})
    }
}
// then subscribe to broadcaster and stream live as before
```

### CLI change

`wendy device logs` gains `--tail N` (default 0). When N > 0, sets `last_n = N` in the
request. The CLI prints a dim separator line between history and live records.

---

## Error Handling Summary

| Failure | Behaviour |
|---|---|
| Disk write fails | Log warning, drop write, continue broadcast |
| Segment dir missing/unwritable | Fall back to in-memory broadcaster, log warning, agent starts |
| Cloud RPC fails | Back off + retry; cursor does not advance |
| Segment evicted during flush | Detect `ErrNotExist`, advance cursor to next segment |
| Partial final frame on open | Skip incomplete frame, start writing from last complete entry |

---

## Testing

### Unit

- `TelemetryBuffer`
  - Write N entries, read back with `ReadLastN(N)` — verify round-trip.
  - Write past `SegmentBytes` threshold — verify new segment created.
  - Write past `MaxTotalBytes` — verify oldest segment evicted.
  - Open a segment with a partial trailing frame — verify it is skipped.
  - Disk write failure — verify broadcast still fires.

- `CloudFlusher`
  - Normal flush: fake cloud client confirms; cursor advances.
  - RPC error: cursor does not advance; flusher retries after backoff.
  - Eviction during read: `ErrNotExist` handled gracefully.
  - Catch-up then live: entries written before and after connect all arrive at fake cloud.

- `TelemetryService.StreamLogs`
  - `last_n = 0`: no history records sent (current behaviour preserved).
  - `last_n = 10`: history records arrive with `is_history = true`, then live records follow.

### Integration

Start full agent stack (buffer + broadcaster + flusher + fake `RemoteLoggingService`):

1. Publish 50 log entries.
2. Disconnect fake cloud.
3. Publish 50 more log entries.
4. Reconnect fake cloud.
5. Assert all 100 entries arrive in order with correct timestamps.

---

## File Inventory

| File | Change |
|---|---|
| `internal/agent/services/telemetry_buffer.go` | New |
| `internal/agent/services/telemetry_buffer_test.go` | New |
| `internal/agent/services/cloud_flusher.go` | New |
| `internal/agent/services/cloud_flusher_test.go` | New |
| `internal/agent/services/telemetry_service.go` | Add `last_n` history replay |
| `internal/agent/services/telemetry_service_test.go` | Extend for `last_n` |
| `internal/agent/services/telemetry_service_v2.go` | Mirror `last_n` support |
| `cmd/wendy-agent/main.go` | Wire `TelemetryBuffer`, start `CloudFlusher` |
| `Proto/wendy/agent/services/v1/wendy_agent_v1_telemetry_service.proto` | Add `last_n`, `is_history` |
| `Proto/wendy/agent/services/v2/telemetry_service.proto` | Same additions |
| `internal/cli/commands/device.go` | Add `--tail N` flag |
