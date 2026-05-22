# CLI Telemetry: Replace PostHog with self-hosted backend

**Date:** 2026-05-03  
**Status:** Approved

## Overview

Replace the CLI's PostHog analytics integration with a self-hosted HTTP endpoint on the Wendy Cloud backend. The CLI continues to send the same structured `command_executed` events (command path, success, error class, duration, OS/arch, CLI version) using the existing anonymous UUID already stored on disk. All call sites in the CLI remain unchanged.

## Scope

Two repos are involved:

- `wendy-agent` — CLI: swap the analytics backend, remove the PostHog dependency
- `cloud` — Backend: new Postgres table, HTTP handler, Envoy routing

## Data Model (cloud)

New migration `000024_create_cli_events_table`:

```sql
CREATE TABLE cli_events (
    id            BIGSERIAL    PRIMARY KEY,
    anonymous_id  TEXT         NOT NULL,
    event         TEXT         NOT NULL,
    command_name  TEXT         NOT NULL,
    command_root  TEXT         NOT NULL,
    duration_ms   BIGINT       NOT NULL,
    success       BOOLEAN      NOT NULL,
    error_class   TEXT,
    cli_version   TEXT         NOT NULL,
    os            TEXT         NOT NULL,
    arch          TEXT         NOT NULL,
    is_dev_build  BOOLEAN      NOT NULL,
    created_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);
CREATE INDEX cli_events_anonymous_id_idx ON cli_events(anonymous_id);
CREATE INDEX cli_events_created_at_idx   ON cli_events(created_at);
```

`anonymous_id` is the existing `analytics_id` UUID the CLI writes to `~/.config/wendy/analytics_id` on first run. No new identifier is introduced.

## Cloud Backend Changes

### Service layer

`internal/service/telemetry.go` — `TelemetryService` wraps a sqlc `InsertCLIEvent` query:

```go
type TelemetryService struct { q *sqlc.Queries }
func (s *TelemetryService) RecordEvent(ctx context.Context, params CLIEventParams) error
```

### HTTP handler

A `net/http` server starts on `HTTP_PORT` (default `8082`, read from env). Single route:

```
POST /v1/telemetry/events
Content-Type: application/json
```

Request body:

```json
{
  "anonymous_id": "...",
  "event": "command_executed",
  "command_name": "wendy device wifi connect",
  "command_root": "device",
  "duration_ms": 312,
  "success": true,
  "error_class": "",
  "cli_version": "1.2.3",
  "os": "linux",
  "arch": "arm64",
  "is_dev_build": false
}
```

Responses:
- `204 No Content` — accepted
- `400 Bad Request` — missing required fields (`anonymous_id`, `event`, `command_name`)
- No authentication required

Errors are logged via `slog`; all errors are non-fatal to the caller.

### Envoy routing

Both `envoy.yaml` and `envoy.prod.yaml` get:

1. A new cluster `http_service` using HTTP/1.1 (no `http2_protocol_options`) pointing to port `8082`
2. A new route matching prefix `/v1/telemetry` inserted **before** the existing catch-all `/` route, forwarding to `http_service`

This leaves all existing gRPC traffic unaffected.

### main.go wiring

`TelemetryService` is instantiated with the Postgres pool and passed to the HTTP handler. The HTTP server goroutine is started alongside the existing gRPC listeners.

## CLI Changes

### analytics package (`internal/cli/analytics/analytics.go`)

Public API is unchanged: `Init`, `Track`, `Close`, `Disable`, `Enabled`, `EnvOverride`.

Internal changes:
- PostHog client removed; `posthog-go` removed from `go.mod`/`go.sum`
- `Track()` marshals the event to JSON and launches a goroutine that calls the endpoint with a 5-second `http.Client` timeout. The goroutine is tracked via a `sync.WaitGroup`. Errors are discarded.
- `Init()` loads/creates the UUID and sets `enabled`; no client dial needed
- `Close()` calls `wg.Wait()` to drain the in-flight request before the process exits (preserving the flush behavior of the old PostHog `Close()`)
- Hardcoded endpoint: `https://cloud.wendy.sh/v1/telemetry/events`
- Overridable via `WENDY_TELEMETRY_HOST` env var (full base URL, e.g. `http://localhost:8082`) for dev and tests; the path `/v1/telemetry/events` is always appended

### Unchanged

- `wendy analytics enable/disable/status` commands
- `loadOrCreateID()` and the `analytics_id` file
- All `analytics.Track(...)` call sites in `main.go`
- `SetTrackHookForTesting` (test hook still works)

## Error Handling

- Backend handler: log and return `400` for bad input; log and return `500` for DB errors (DB errors should not surface as user-visible failures since this is a background telemetry path)
- CLI: any HTTP error (timeout, 4xx, 5xx, network failure) is silently dropped — telemetry must never degrade the CLI user experience

## Testing

- Cloud: unit test for `TelemetryService.RecordEvent` against a test DB (following existing `testutil` patterns)
- Cloud: HTTP handler test using `httptest.NewRecorder` — happy path, missing fields, oversized body
- CLI: existing `SetTrackHookForTesting` mechanism continues to work; add a test that verifies `Track` fires an HTTP request to a `httptest.NewServer` when enabled
