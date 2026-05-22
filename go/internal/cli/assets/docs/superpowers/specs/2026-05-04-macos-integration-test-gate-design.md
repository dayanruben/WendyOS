# macOS Integration Test Gate for Stable Releases

**Date:** 2026-05-04  
**Status:** Approved

## Goal

Require macOS hardware integration tests to pass before any stable release (`publish=true`) is allowed to proceed. Nightly prerelease builds are unaffected.

## Architecture

No new workflows are introduced. The existing `integration-tests.yml` already supports `workflow_call` and already hard-fails when no LAN devices are discovered. The change is entirely within `build.yml`.

## Components

### New job: `integration-tests`

Added to `build.yml`. Calls `integration-tests.yml` via `workflow_call`.

- **Condition:** `github.event_name == 'workflow_dispatch' && inputs.publish == true`
- **Inputs passed:** `platform: macos`, `jobs: integration`
- **Inputs omitted:** `hostname` (auto-discover), `tests` (full default suite)
- **Result on no devices found:** hard-fail (existing behavior in `integration-tests.yml`)

### Updated job: `release`

- Adds `integration-tests` to `needs`
- Updates `if` condition to allow `skipped` result (so nightly builds still release):

  ```
  (push || publish==true) && (integration-tests == success || integration-tests == skipped)
  ```

  Nightly path: `integration-tests` skipped → `release` runs.  
  Stable path: `integration-tests` must succeed → `release` runs. Failure blocks release.

### Updated job: `publish-linux-repos`

- Adds `integration-tests` to `needs`
- No `if` change needed: on nightly, this job was already skipped by its own `is_release == 'true'` guard; on stable, the integration-tests result gates it naturally.

### Unchanged jobs: `publish-aur`, `publish-winget`

Both depend on `release`, so they are automatically gated. No direct changes needed.

## Data Flow

```
workflow_dispatch (publish=true)
  ├── integration-tests  ←── must pass
  ├── build jobs (parallel)
  └── release            ←── needs integration-tests + build jobs
        ├── publish-linux-repos  ←── needs integration-tests independently
        ├── publish-aur
        └── publish-winget
```

## Error Handling

- No hardware available → `integration-tests` job fails → `release` is skipped → stable release blocked.
- Integration test failures → same outcome.
- Nightly push to main → `integration-tests` is skipped → `release` `if` condition allows `skipped` → nightly unaffected.

## Testing

After the change, validate by triggering `workflow_dispatch` with `publish=false` (nightly path) and confirming `integration-tests` is skipped and `release` still runs. A dry-run with `publish=true` against available hardware confirms the gate.
