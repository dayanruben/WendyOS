# CLI Telemetry: Replace PostHog with Self-Hosted Backend — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the CLI's PostHog analytics with a self-hosted HTTP endpoint on the Wendy Cloud backend, storing `command_executed` events in Postgres.

**Architecture:** The cloud backend gains a plain `net/http` listener (port 8082) behind a new Envoy route. The CLI's `analytics` package swaps the PostHog client for a fire-and-forget `http.Post` goroutine tracked by a `sync.WaitGroup`. The public analytics API (`Init`, `Track`, `Close`, `Disable`, `Enabled`) is unchanged so no call sites need updating.

**Tech Stack:** Go 1.23, PostgreSQL (pgx/v5), sqlc v1.30, Envoy, cobra (CLI)

---

## File Map

### Cloud backend (`/path/to/cloud/services`)

| Action | Path | Purpose |
|--------|------|---------|
| Create | `migrations/000024_create_cli_events_table.up.sql` | Create `cli_events` table |
| Create | `migrations/000024_create_cli_events_table.down.sql` | Drop `cli_events` table |
| Create | `sqlc/queries/cli_events.sql` | sqlc source query for insert |
| Auto-generated | `internal/db/sqlc/cli_events.sql.go` | sqlc output — do not edit manually |
| Modify | `internal/config/config.go` | Add `HTTPPort int` field |
| Create | `internal/service/telemetry.go` | `TelemetryService.RecordEvent()` |
| Create | `internal/service/telemetry_test.go` | Integration test against real DB |
| Create | `internal/httphandler/telemetry.go` | HTTP handler + `NewTelemetryMux()` |
| Create | `internal/httphandler/telemetry_test.go` | Unit tests via `httptest` (no DB) |
| Modify | `cmd/server/main.go` | Wire service, start HTTP listener |
| Modify | `envoy.yaml` | Add `http_service` cluster + route |
| Modify | `envoy.prod.yaml` | Same change for production |

### CLI (`/path/to/wendy-agent/go`)

| Action | Path | Purpose |
|--------|------|---------|
| Modify | `internal/cli/analytics/analytics.go` | Replace PostHog with HTTP client |
| Modify | `internal/cli/analytics/analytics_test.go` | Update tests, add HTTP delivery test |
| Modify | `go.mod` + `go.sum` | Remove `posthog-go` |

---

## Task 1: Migration — create cli_events table

**Files:**
- Create: `cloud/services/migrations/000024_create_cli_events_table.up.sql`
- Create: `cloud/services/migrations/000024_create_cli_events_table.down.sql`

- [ ] **Step 1: Write the up migration**

```sql
-- cloud/services/migrations/000024_create_cli_events_table.up.sql
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

- [ ] **Step 2: Write the down migration**

```sql
-- cloud/services/migrations/000024_create_cli_events_table.down.sql
DROP TABLE IF EXISTS cli_events;
```

- [ ] **Step 3: Commit**

```bash
git add migrations/000024_create_cli_events_table.up.sql \
        migrations/000024_create_cli_events_table.down.sql
git commit -m "feat(db): add cli_events table for CLI telemetry"
```

---

## Task 2: sqlc query + generate

**Files:**
- Create: `cloud/services/sqlc/queries/cli_events.sql`
- Auto-generated: `cloud/services/internal/db/sqlc/cli_events.sql.go`

- [ ] **Step 1: Write the query**

```sql
-- cloud/services/sqlc/queries/cli_events.sql

-- name: InsertCLIEvent :exec
INSERT INTO cli_events (
    anonymous_id, event, command_name, command_root,
    duration_ms, success, error_class, cli_version,
    os, arch, is_dev_build
) VALUES (
    $1, $2, $3, $4,
    $5, $6, $7, $8,
    $9, $10, $11
);
```

- [ ] **Step 2: Run sqlc generate**

From `cloud/services/sqlc`:
```bash
cd cloud/services/sqlc && sqlc generate
```

Expected: no errors; file `cloud/services/internal/db/sqlc/cli_events.sql.go` is created.

Verify it contains:
```bash
grep "InsertCLIEvent\|InsertCLIEventParams" cloud/services/internal/db/sqlc/cli_events.sql.go
```
Expected output:
```
const insertCLIEvent = ...
type InsertCLIEventParams struct {
func (q *Queries) InsertCLIEvent(...
```

- [ ] **Step 3: Commit**

```bash
git add sqlc/queries/cli_events.sql internal/db/sqlc/cli_events.sql.go
git commit -m "feat(db): add InsertCLIEvent sqlc query"
```

---

## Task 3: TelemetryService

**Files:**
- Create: `cloud/services/internal/service/telemetry.go`
- Create: `cloud/services/internal/service/telemetry_test.go`

- [ ] **Step 1: Write the failing test**

```go
// cloud/services/internal/service/telemetry_test.go
package service_test

import (
    "context"
    "testing"

    "github.com/wendylabsinc/cloud/services/internal/service"
    "github.com/wendylabsinc/cloud/services/internal/testutil"
)

func TestTelemetryRecordEvent(t *testing.T) {
    pool := testutil.TestPool(t)
    svc := service.NewTelemetryService(pool)
    ctx := context.Background()

    err := svc.RecordEvent(ctx, service.CLIEventParams{
        AnonymousID: "test-uuid-1234",
        Event:       "command_executed",
        CommandName: "wendy device list",
        CommandRoot: "device",
        DurationMS:  123,
        Success:     true,
        ErrorClass:  "",
        CLIVersion:  "1.2.3",
        OS:          "linux",
        Arch:        "amd64",
        IsDevBuild:  false,
    })
    if err != nil {
        t.Fatalf("RecordEvent: %v", err)
    }

    // Verify row was written
    var count int
    err = pool.QueryRow(ctx,
        `SELECT COUNT(*) FROM cli_events WHERE anonymous_id = $1`,
        "test-uuid-1234",
    ).Scan(&count)
    if err != nil {
        t.Fatalf("query: %v", err)
    }
    if count != 1 {
        t.Errorf("expected 1 row, got %d", count)
    }
}
```

- [ ] **Step 2: Run test — expect failure**

```bash
cd cloud/services && go test ./internal/service/... -run TestTelemetryRecordEvent -v
```
Expected: compile error — `service.NewTelemetryService` undefined.

- [ ] **Step 3: Implement TelemetryService**

```go
// cloud/services/internal/service/telemetry.go
package service

import (
    "context"

    "github.com/jackc/pgx/v5/pgxpool"
    "github.com/wendylabsinc/cloud/services/internal/db/sqlc"
)

type TelemetryService struct {
    q *sqlc.Queries
}

func NewTelemetryService(pool *pgxpool.Pool) *TelemetryService {
    return &TelemetryService{q: sqlc.New(pool)}
}

type CLIEventParams struct {
    AnonymousID string
    Event       string
    CommandName string
    CommandRoot string
    DurationMS  int64
    Success     bool
    ErrorClass  string
    CLIVersion  string
    OS          string
    Arch        string
    IsDevBuild  bool
}

func (s *TelemetryService) RecordEvent(ctx context.Context, p CLIEventParams) error {
    return s.q.InsertCLIEvent(ctx, sqlc.InsertCLIEventParams{
        AnonymousID: p.AnonymousID,
        Event:       p.Event,
        CommandName: p.CommandName,
        CommandRoot: p.CommandRoot,
        DurationMs:  p.DurationMS,
        Success:     p.Success,
        ErrorClass:  &p.ErrorClass,
        CliVersion:  p.CLIVersion,
        Os:          p.OS,
        Arch:        p.Arch,
        IsDevBuild:  p.IsDevBuild,
    })
}
```

> **Note on generated field names:** sqlc derives Go field names from SQL column names. `duration_ms` → `DurationMs`, `cli_version` → `CliVersion`, `os` → `Os`, `arch` → `Arch`, `is_dev_build` → `IsDevBuild`, `error_class` → `ErrorClass`. Verify after running `sqlc generate` in Task 2 by reading `cli_events.sql.go`; adjust if the generated names differ. The `error_class` column is nullable TEXT, so sqlc may generate it as `pgtype.Text` or `*string` — match whatever is generated.

- [ ] **Step 4: Run test — expect pass**

```bash
cd cloud/services && go test ./internal/service/... -run TestTelemetryRecordEvent -v
```
Expected: `PASS`

- [ ] **Step 5: Commit**

```bash
git add internal/service/telemetry.go internal/service/telemetry_test.go
git commit -m "feat(service): add TelemetryService for CLI event recording"
```

---

## Task 4: HTTP handler

**Files:**
- Create: `cloud/services/internal/httphandler/telemetry.go`
- Create: `cloud/services/internal/httphandler/telemetry_test.go`

- [ ] **Step 1: Write the failing tests**

```go
// cloud/services/internal/httphandler/telemetry_test.go
package httphandler_test

import (
    "bytes"
    "context"
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "testing"

    "github.com/wendylabsinc/cloud/services/internal/httphandler"
    "github.com/wendylabsinc/cloud/services/internal/service"
)

// fakeRecorder is a test double for TelemetryService.
type fakeRecorder struct {
    calls []service.CLIEventParams
    err   error
}

func (f *fakeRecorder) RecordEvent(_ context.Context, p service.CLIEventParams) error {
    f.calls = append(f.calls, p)
    return f.err
}

func validBody() []byte {
    b, _ := json.Marshal(map[string]any{
        "anonymous_id": "uuid-abc",
        "event":        "command_executed",
        "command_name": "wendy device list",
        "command_root": "device",
        "duration_ms":  int64(100),
        "success":      true,
        "cli_version":  "1.0.0",
        "os":           "linux",
        "arch":         "amd64",
        "is_dev_build": false,
    })
    return b
}

func TestTelemetryHandler_HappyPath(t *testing.T) {
    rec := &fakeRecorder{}
    mux := httphandler.NewTelemetryMux(rec)

    req := httptest.NewRequest(http.MethodPost, "/v1/telemetry/events",
        bytes.NewReader(validBody()))
    req.Header.Set("Content-Type", "application/json")
    w := httptest.NewRecorder()
    mux.ServeHTTP(w, req)

    if w.Code != http.StatusNoContent {
        t.Errorf("status = %d, want %d", w.Code, http.StatusNoContent)
    }
    if len(rec.calls) != 1 {
        t.Fatalf("expected 1 RecordEvent call, got %d", len(rec.calls))
    }
    if rec.calls[0].AnonymousID != "uuid-abc" {
        t.Errorf("anonymous_id = %q, want %q", rec.calls[0].AnonymousID, "uuid-abc")
    }
    if rec.calls[0].CommandName != "wendy device list" {
        t.Errorf("command_name = %q", rec.calls[0].CommandName)
    }
}

func TestTelemetryHandler_MissingAnonymousID(t *testing.T) {
    rec := &fakeRecorder{}
    mux := httphandler.NewTelemetryMux(rec)

    body, _ := json.Marshal(map[string]any{
        "event":        "command_executed",
        "command_name": "wendy device list",
    })
    req := httptest.NewRequest(http.MethodPost, "/v1/telemetry/events",
        bytes.NewReader(body))
    req.Header.Set("Content-Type", "application/json")
    w := httptest.NewRecorder()
    mux.ServeHTTP(w, req)

    if w.Code != http.StatusBadRequest {
        t.Errorf("status = %d, want 400", w.Code)
    }
    if len(rec.calls) != 0 {
        t.Error("RecordEvent must not be called on bad input")
    }
}

func TestTelemetryHandler_MissingEvent(t *testing.T) {
    rec := &fakeRecorder{}
    mux := httphandler.NewTelemetryMux(rec)

    body, _ := json.Marshal(map[string]any{
        "anonymous_id": "uuid-abc",
        "command_name": "wendy device list",
    })
    req := httptest.NewRequest(http.MethodPost, "/v1/telemetry/events",
        bytes.NewReader(body))
    req.Header.Set("Content-Type", "application/json")
    w := httptest.NewRecorder()
    mux.ServeHTTP(w, req)

    if w.Code != http.StatusBadRequest {
        t.Errorf("status = %d, want 400", w.Code)
    }
}

func TestTelemetryHandler_MissingCommandName(t *testing.T) {
    rec := &fakeRecorder{}
    mux := httphandler.NewTelemetryMux(rec)

    body, _ := json.Marshal(map[string]any{
        "anonymous_id": "uuid-abc",
        "event":        "command_executed",
    })
    req := httptest.NewRequest(http.MethodPost, "/v1/telemetry/events",
        bytes.NewReader(body))
    req.Header.Set("Content-Type", "application/json")
    w := httptest.NewRecorder()
    mux.ServeHTTP(w, req)

    if w.Code != http.StatusBadRequest {
        t.Errorf("status = %d, want 400", w.Code)
    }
}

func TestTelemetryHandler_MethodNotAllowed(t *testing.T) {
    rec := &fakeRecorder{}
    mux := httphandler.NewTelemetryMux(rec)

    req := httptest.NewRequest(http.MethodGet, "/v1/telemetry/events", nil)
    w := httptest.NewRecorder()
    mux.ServeHTTP(w, req)

    if w.Code != http.StatusMethodNotAllowed {
        t.Errorf("status = %d, want 405", w.Code)
    }
}

func TestTelemetryHandler_OversizedBody(t *testing.T) {
    rec := &fakeRecorder{}
    mux := httphandler.NewTelemetryMux(rec)

    big := make([]byte, 1<<16) // 64 KB — well above limit
    req := httptest.NewRequest(http.MethodPost, "/v1/telemetry/events",
        bytes.NewReader(big))
    req.Header.Set("Content-Type", "application/json")
    w := httptest.NewRecorder()
    mux.ServeHTTP(w, req)

    if w.Code != http.StatusBadRequest {
        t.Errorf("status = %d, want 400 (oversized body)", w.Code)
    }
}
```

- [ ] **Step 2: Run tests — expect failure**

```bash
cd cloud/services && go test ./internal/httphandler/... -v
```
Expected: compile error — `httphandler` package not found.

- [ ] **Step 3: Implement the handler**

```go
// cloud/services/internal/httphandler/telemetry.go
package httphandler

import (
    "context"
    "encoding/json"
    "io"
    "log/slog"
    "net/http"

    "github.com/wendylabsinc/cloud/services/internal/service"
)

const maxBodyBytes = 8 * 1024 // 8 KB

type eventRecorder interface {
    RecordEvent(ctx context.Context, params service.CLIEventParams) error
}

type cliEventRequest struct {
    AnonymousID string `json:"anonymous_id"`
    Event       string `json:"event"`
    CommandName string `json:"command_name"`
    CommandRoot string `json:"command_root"`
    DurationMS  int64  `json:"duration_ms"`
    Success     bool   `json:"success"`
    ErrorClass  string `json:"error_class"`
    CLIVersion  string `json:"cli_version"`
    OS          string `json:"os"`
    Arch        string `json:"arch"`
    IsDevBuild  bool   `json:"is_dev_build"`
}

// NewTelemetryMux returns an http.ServeMux with the telemetry route registered.
func NewTelemetryMux(svc eventRecorder) *http.ServeMux {
    mux := http.NewServeMux()
    mux.HandleFunc("/v1/telemetry/events", func(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodPost {
            http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
            return
        }

        body, err := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes+1))
        if err != nil {
            http.Error(w, "read error", http.StatusBadRequest)
            return
        }
        if len(body) > maxBodyBytes {
            http.Error(w, "request body too large", http.StatusBadRequest)
            return
        }

        var req cliEventRequest
        if err := json.Unmarshal(body, &req); err != nil {
            http.Error(w, "invalid JSON", http.StatusBadRequest)
            return
        }

        if req.AnonymousID == "" || req.Event == "" || req.CommandName == "" {
            http.Error(w, "missing required fields", http.StatusBadRequest)
            return
        }

        if err := svc.RecordEvent(r.Context(), service.CLIEventParams{
            AnonymousID: req.AnonymousID,
            Event:       req.Event,
            CommandName: req.CommandName,
            CommandRoot: req.CommandRoot,
            DurationMS:  req.DurationMS,
            Success:     req.Success,
            ErrorClass:  req.ErrorClass,
            CLIVersion:  req.CLIVersion,
            OS:          req.OS,
            Arch:        req.Arch,
            IsDevBuild:  req.IsDevBuild,
        }); err != nil {
            slog.Error("failed to record CLI event", "error", err)
            http.Error(w, "internal error", http.StatusInternalServerError)
            return
        }

        w.WriteHeader(http.StatusNoContent)
    })
    return mux
}
```

- [ ] **Step 4: Run tests — expect pass**

```bash
cd cloud/services && go test ./internal/httphandler/... -v
```
Expected: all tests `PASS`.

- [ ] **Step 5: Commit**

```bash
git add internal/httphandler/telemetry.go internal/httphandler/telemetry_test.go
git commit -m "feat(http): add telemetry HTTP handler"
```

---

## Task 5: Config + main.go wiring

**Files:**
- Modify: `cloud/services/internal/config/config.go`
- Modify: `cloud/services/cmd/server/main.go`

- [ ] **Step 1: Add HTTPPort to config**

In `cloud/services/internal/config/config.go`, add to `Config` struct (after `GRPCPort`):

```go
HTTPPort int
```

In the `Load()` function, add to the returned `&Config{...}` (after `GRPCPort`):

```go
HTTPPort: envInt("HTTP_PORT", 8082),
```

- [ ] **Step 2: Wire TelemetryService and start HTTP server in main.go**

In `cloud/services/cmd/server/main.go`, add the import:
```go
"github.com/wendylabsinc/cloud/services/internal/httphandler"
```

After the existing service instantiations (around line 100, after `mapSvc`), add:
```go
telemetrySvc := service.NewTelemetryService(pool)
```

After the mTLS listener setup and before the final `<-ctx.Done()` wait, add the HTTP server goroutine. Find the block that waits on `ctx.Done()` (at the end of `main`) and insert this before it:

```go
// HTTP server for unauthenticated endpoints (telemetry)
httpAddr := fmt.Sprintf("%s:%d", cfg.ServerHost, cfg.HTTPPort)
httpLis, err := net.Listen("tcp", httpAddr)
if err != nil {
    slog.Error("failed to listen on HTTP port", "address", httpAddr, "error", err)
    os.Exit(1)
}
go func() {
    slog.Info("HTTP server starting", "address", httpAddr)
    if err := http.Serve(httpLis, httphandler.NewTelemetryMux(telemetrySvc)); err != nil && err != http.ErrServerClosed {
        slog.Error("HTTP server failed", "error", err)
    }
}()
```

Also add `"net/http"` to the imports if not already present.

- [ ] **Step 3: Verify the build compiles**

```bash
cd cloud/services && go build ./cmd/server/...
```
Expected: no errors.

- [ ] **Step 4: Commit**

```bash
git add internal/config/config.go cmd/server/main.go
git commit -m "feat(server): wire TelemetryService and start HTTP listener on HTTP_PORT"
```

---

## Task 6: Envoy routing

**Files:**
- Modify: `cloud/envoy.yaml`
- Modify: `cloud/envoy.prod.yaml`

Both files need the same two changes: a new cluster and a new route. Apply to each file independently.

- [ ] **Step 1: Update envoy.yaml**

**Add a new cluster** at the bottom of the `clusters:` list (after the existing `grpc_service` cluster):

```yaml
  - name: http_service
    connect_timeout: 0.25s
    type: LOGICAL_DNS
    dns_lookup_family: V4_ONLY
    lb_policy: ROUND_ROBIN
    load_assignment:
      cluster_name: http_service
      endpoints:
      - lb_endpoints:
        - endpoint:
            address:
              socket_address:
                address: services
                port_value: 8082
```

**Add a route** inside `virtual_hosts[0].routes`, *before* the existing catch-all `prefix: "/"` route:

```yaml
              - match:
                  prefix: "/v1/telemetry"
                route:
                  cluster: http_service
                  timeout: 5s
```

- [ ] **Step 2: Update envoy.prod.yaml**

**Add a new cluster** at the bottom of the `clusters:` list:

```yaml
  - name: http_service
    connect_timeout: 5s
    type: STATIC
    lb_policy: ROUND_ROBIN
    load_assignment:
      cluster_name: http_service
      endpoints:
      - lb_endpoints:
        - endpoint:
            address:
              socket_address:
                address: 127.0.0.1
                port_value: 8082
```

**Add a route** inside `virtual_hosts[0].routes`, *before* the existing catch-all `prefix: "/"` route:

```yaml
              - match:
                  prefix: "/v1/telemetry"
                route:
                  cluster: http_service
                  timeout: 5s
```

- [ ] **Step 3: Commit**

```bash
git add envoy.yaml envoy.prod.yaml
git commit -m "feat(envoy): route /v1/telemetry to HTTP service on port 8082"
```

---

## Task 7: CLI — replace PostHog with HTTP client

**Files:**
- Modify: `wendy-agent/go/internal/cli/analytics/analytics.go`
- Modify: `wendy-agent/go/go.mod` and `go.sum`

- [ ] **Step 1: Rewrite analytics.go**

Replace the entire file with:

```go
// Package analytics provides anonymous usage tracking.
package analytics

import (
    "bytes"
    "encoding/json"
    "fmt"
    "io"
    "net/http"
    "os"
    "path/filepath"
    "runtime"
    "strconv"
    "strings"
    "sync"
    "time"

    "github.com/google/uuid"
    "github.com/wendylabsinc/wendy/internal/shared/config"
    "github.com/wendylabsinc/wendy/internal/shared/env"
    "github.com/wendylabsinc/wendy/internal/shared/version"
)

const defaultTelemetryBase = "https://cloud.wendy.sh"

var (
    enabled    bool
    distinctID string
    wg         sync.WaitGroup
    httpClient = &http.Client{Timeout: 5 * time.Second}

    // trackHook is set by tests to intercept events before HTTP dispatch.
    // It is never set in production code.
    trackHook func(event string, properties map[string]string)
)

// SetTrackHookForTesting installs a hook that receives every Track call.
// Pass nil to clear. Intended for tests only.
func SetTrackHookForTesting(fn func(event string, properties map[string]string)) {
    trackHook = fn
}

// Init initializes analytics. Returns true if this is the first run (config.Analytics
// was nil) AND the env var does not override, so the caller can display a notice.
//
// CI environments are hard-disabled regardless of WENDY_ANALYTICS or stored config.
func Init(cfg *config.Config) (firstRun bool) {
    if env.IsCI() {
        Disable()
        return false
    }

    if !env.Analytics() {
        Disable()
        return false
    }

    if cfg.Analytics == nil {
        firstRun = true
        enabled = true
    } else {
        enabled = cfg.Analytics.Enabled
    }

    if !enabled {
        Disable()
        return firstRun
    }

    var err error
    distinctID, err = loadOrCreateID()
    if err != nil {
        Disable()
        return firstRun
    }

    return firstRun
}

// Track sends a command_executed analytics event to the Wendy Cloud backend.
// The HTTP call is made in a goroutine; errors are silently discarded.
// The test hook (if any) always fires regardless of enabled state.
func Track(event string, properties map[string]string) {
    if trackHook != nil {
        trackHook(event, properties)
    }
    if !enabled {
        return
    }

    payload := buildPayload(event, properties)
    body, err := json.Marshal(payload)
    if err != nil {
        return
    }

    wg.Add(1)
    go func() {
        defer wg.Done()
        endpoint := telemetryEndpoint()
        resp, err := httpClient.Post(endpoint, "application/json", bytes.NewReader(body))
        if err != nil {
            return
        }
        _, _ = io.Copy(io.Discard, resp.Body)
        _ = resp.Body.Close()
    }()
}

// Close waits for any in-flight telemetry request to complete.
func Close() {
    wg.Wait()
}

// Disable turns off analytics for the current process.
func Disable() {
    enabled = false
}

// Enabled reports whether analytics is currently enabled.
func Enabled() bool {
    return enabled
}

// EnvOverride reports whether the WENDY_ANALYTICS env var is set to "false".
func EnvOverride() bool {
    return !env.Analytics()
}

func telemetryEndpoint() string {
    base := os.Getenv("WENDY_TELEMETRY_HOST")
    if base == "" {
        base = defaultTelemetryBase
    }
    return base + "/v1/telemetry/events"
}

type cliEventPayload struct {
    AnonymousID string `json:"anonymous_id"`
    Event       string `json:"event"`
    CommandName string `json:"command_name"`
    CommandRoot string `json:"command_root"`
    DurationMS  int64  `json:"duration_ms"`
    Success     bool   `json:"success"`
    ErrorClass  string `json:"error_class,omitempty"`
    CLIVersion  string `json:"cli_version"`
    OS          string `json:"os"`
    Arch        string `json:"arch"`
    IsDevBuild  bool   `json:"is_dev_build"`
}

func buildPayload(event string, properties map[string]string) cliEventPayload {
    durationMS, _ := strconv.ParseInt(properties["duration_ms"], 10, 64)
    success, _ := strconv.ParseBool(properties["success"])
    isDevBuild, _ := strconv.ParseBool(properties["is_dev_build"])
    return cliEventPayload{
        AnonymousID: distinctID,
        Event:       event,
        CommandName: properties["command_name"],
        CommandRoot: properties["command_root"],
        DurationMS:  durationMS,
        Success:     success,
        ErrorClass:  properties["error_class"],
        CLIVersion:  version.Version,
        OS:          runtime.GOOS,
        Arch:        runtime.GOARCH,
        IsDevBuild:  isDevBuild,
    }
}

func loadOrCreateID() (string, error) {
    dir, err := config.ConfigDir()
    if err != nil {
        return "", err
    }

    idPath := filepath.Join(dir, "analytics_id")
    data, err := os.ReadFile(idPath)
    if err == nil {
        id := strings.TrimSpace(string(data))
        if id != "" {
            return id, nil
        }
    }

    id := uuid.NewString()
    if err := os.WriteFile(idPath, []byte(id), 0o600); err != nil {
        return "", fmt.Errorf("writing analytics ID: %w", err)
    }
    return id, nil
}
```


- [ ] **Step 2: Remove posthog-go dependency**

```bash
cd wendy-agent/go && go mod tidy
```

Expected: `posthog-go` removed from `go.mod` and `go.sum`. Verify:
```bash
grep posthog go.mod
```
Expected: no output.

- [ ] **Step 3: Verify build**

```bash
cd wendy-agent/go && go build ./...
```
Expected: no errors.

- [ ] **Step 4: Commit**

```bash
git add internal/cli/analytics/analytics.go go.mod go.sum
git commit -m "feat(analytics): replace PostHog with self-hosted HTTP telemetry"
```

---

## Task 8: Update CLI analytics tests

**Files:**
- Modify: `wendy-agent/go/internal/cli/analytics/analytics_test.go`

The existing tests reference `client` (the old PostHog client variable) which no longer exists. Two tests need updating: `TestInitDisabledInCI` and `TestTrackHookFiresEvenWhenDisabled`. A new test for HTTP delivery is also added.

- [ ] **Step 1: Run existing tests — expect failures**

```bash
cd wendy-agent/go && go test ./internal/cli/analytics/... -v
```
Expected: compile errors on `client` references.

- [ ] **Step 2: Update analytics_test.go**

Replace the full file:

```go
package analytics

import (
    "io"
    "net/http"
    "net/http/httptest"
    "strings"
    "testing"

    "github.com/wendylabsinc/wendy/internal/shared/config"
    "github.com/wendylabsinc/wendy/internal/shared/env"
)

func clearCIEnv(t *testing.T) {
    t.Helper()
    for _, key := range env.CIEnvVars {
        t.Setenv(key, "")
    }
}

func TestDisabledViaEnvVar(t *testing.T) {
    clearCIEnv(t)
    t.Setenv("WENDY_ANALYTICS", "false")
    t.Setenv("HOME", t.TempDir())

    cfg := &config.Config{
        Analytics: &config.AnalyticsConfig{Enabled: true},
    }
    Init(cfg)

    if Enabled() {
        t.Error("expected analytics to be disabled via env var")
    }
}

func TestDisabledViaConfig(t *testing.T) {
    clearCIEnv(t)
    t.Setenv("WENDY_ANALYTICS", "")
    t.Setenv("HOME", t.TempDir())

    cfg := &config.Config{
        Analytics: &config.AnalyticsConfig{Enabled: false},
    }
    Init(cfg)

    if Enabled() {
        t.Error("expected analytics to be disabled via config")
    }
}

func TestEnabledByDefaultWhenNil(t *testing.T) {
    clearCIEnv(t)
    t.Setenv("WENDY_ANALYTICS", "")
    t.Setenv("HOME", t.TempDir())

    cfg := &config.Config{
        Analytics: nil,
    }
    firstRun := Init(cfg)

    if !firstRun {
        t.Error("expected firstRun to be true when Analytics is nil")
    }
}

func TestEnvOverride(t *testing.T) {
    t.Setenv("WENDY_ANALYTICS", "false")
    if !EnvOverride() {
        t.Error("expected EnvOverride to return true")
    }

    t.Setenv("WENDY_ANALYTICS", "")
    if EnvOverride() {
        t.Error("expected EnvOverride to return false")
    }
}

func TestTrackNoOpWhenDisabled(t *testing.T) {
    clearCIEnv(t)
    t.Setenv("WENDY_ANALYTICS", "false")
    t.Setenv("HOME", t.TempDir())

    cfg := &config.Config{}
    Init(cfg)

    Track("test_event", map[string]string{"key": "value"})
    Close()
}

// TestInitDisabledInCI asserts that the CI hard-kill switch prevents analytics
// even when the user has explicitly opted in via env var and stored config.
func TestInitDisabledInCI(t *testing.T) {
    for _, ciKey := range env.CIEnvVars {
        t.Run(ciKey, func(t *testing.T) {
            clearCIEnv(t)
            t.Setenv(ciKey, "1")
            t.Setenv("WENDY_ANALYTICS", "true")
            t.Setenv("HOME", t.TempDir())

            cfg := &config.Config{
                Analytics: &config.AnalyticsConfig{Enabled: true},
            }
            firstRun := Init(cfg)

            if firstRun {
                t.Errorf("Init must return firstRun=false in CI (%s set)", ciKey)
            }
            if Enabled() {
                t.Errorf("analytics must not be enabled in CI (%s set)", ciKey)
            }
        })
    }
}

// TestTrackHookFiresEvenWhenDisabled documents that the test hook fires on
// every Track call regardless of enabled state.
func TestTrackHookFiresEvenWhenDisabled(t *testing.T) {
    clearCIEnv(t)
    t.Setenv("WENDY_ANALYTICS", "false")
    t.Setenv("HOME", t.TempDir())

    Init(&config.Config{})
    if Enabled() {
        t.Fatal("test setup: Init should have left analytics disabled")
    }

    var got []string
    SetTrackHookForTesting(func(event string, _ map[string]string) {
        got = append(got, event)
    })
    t.Cleanup(func() { SetTrackHookForTesting(nil) })

    Track("synthetic", map[string]string{"k": "v"})
    if len(got) != 1 || got[0] != "synthetic" {
        t.Errorf("hook must fire when disabled; got %v", got)
    }
}

// TestTrackSendsHTTPRequest verifies that Track dispatches an HTTP POST to the
// telemetry endpoint when analytics is enabled.
func TestTrackSendsHTTPRequest(t *testing.T) {
    clearCIEnv(t)
    t.Setenv("WENDY_ANALYTICS", "")
    t.Setenv("HOME", t.TempDir())

    var received []byte
    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodPost {
            t.Errorf("unexpected method: %s", r.Method)
        }
        received, _ = io.ReadAll(r.Body)
        w.WriteHeader(http.StatusNoContent)
    }))
    defer srv.Close()
    t.Setenv("WENDY_TELEMETRY_HOST", srv.URL)

    Init(&config.Config{Analytics: &config.AnalyticsConfig{Enabled: true}})
    if !Enabled() {
        t.Fatal("expected analytics to be enabled")
    }

    Track("command_executed", map[string]string{
        "command_name": "wendy device list",
        "command_root": "device",
        "duration_ms":  "50",
        "success":      "true",
        "is_dev_build": "false",
    })
    Close() // wait for goroutine

    if len(received) == 0 {
        t.Fatal("expected HTTP request body to be non-empty")
    }
    if !strings.Contains(string(received), "command_executed") {
        t.Errorf("expected event name in body; got: %s", received)
    }
    if !strings.Contains(string(received), "wendy device list") {
        t.Errorf("expected command_name in body; got: %s", received)
    }

    // Cleanup global state
    Disable()
    t.Cleanup(func() { enabled = false; distinctID = "" })
}
```

- [ ] **Step 3: Run tests — expect pass**

```bash
cd wendy-agent/go && go test ./internal/cli/analytics/... -v
```
Expected: all tests `PASS`. The CI tests will skip if run outside CI; the HTTP test will spin up a local server.

- [ ] **Step 4: Run the full CLI test suite**

```bash
cd wendy-agent/go && go test ./... 2>&1 | tail -20
```
Expected: no new failures.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/analytics/analytics_test.go
git commit -m "test(analytics): update tests for HTTP-based telemetry, remove PostHog references"
```

---

## Post-Implementation Checklist

- [ ] `grep -r posthog wendy-agent/go --include="*.go"` — should return empty
- [ ] `grep posthog wendy-agent/go/go.mod` — should return empty
- [ ] Cloud services build: `cd cloud/services && go build ./...`
- [ ] Cloud services tests: `cd cloud/services && go test ./internal/service/... ./internal/httphandler/...`
- [ ] CLI tests: `cd wendy-agent/go && go test ./...`
- [ ] Smoke test: start the cloud backend locally with `HTTP_PORT=8082`, send a `curl -X POST http://localhost:8082/v1/telemetry/events -H 'Content-Type: application/json' -d '{"anonymous_id":"test","event":"command_executed","command_name":"wendy device list","command_root":"device","duration_ms":100,"success":true,"cli_version":"dev","os":"linux","arch":"amd64","is_dev_build":true}'` — expect `204`
