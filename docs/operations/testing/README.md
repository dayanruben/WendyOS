# Integration Testing

This document describes the CI integration test pipeline for WendyOS and wendy-agent.

## Overview

Integration tests run against real hardware devices discovered on the CI LAN. The pipeline has three jobs:

| Job | Runner | Description |
|-----|--------|-------------|
| `discover` | ubuntu-latest | Discovers LAN devices via `wendy discover` |
| `integration-test-macos` | macos | Builds `wendy` and runs the full test suite on discovered devices |
| `integration-test-linux` | ubuntu-latest | Builds `wendy` and runs the Linux-compatible subset of the test suite |

> **Note:** The `summary` job was removed. Pass/fail status is determined directly from the `integration-test-macos` and `integration-test-linux` job results.

## Test cases

The following named test cases are registered in the pipeline:

| Test | macOS | Linux | Description |
|------|-------|-------|-------------|
| `swift-hello` | ✅ | ❌ | Basic Swift app smoke test |
| `swift-network` | ✅ | ❌ | Swift app with network entitlement |
| `swift-bluetooth` | ✅ | ❌ | Swift app with Bluetooth entitlement |
| `swift-resources` | ✅ | ❌ | Swift app verifying SwiftPM resource bundle syncing |
| `python-hello` | ✅ | ✅ | Basic Python app smoke test |
| `python-network` | ✅ | ✅ | Python app with network entitlement |
| `python-gpu` | ✅ | ✅ | Python app with GPU entitlement |
| `python-bluetooth` | ✅ | ✅ | Python app with Bluetooth entitlement |
| `python-no-network` | ✅ | ✅ | Python app with networking disabled |
| `python-no-bluetooth` | ✅ | ✅ | Python app with Bluetooth disabled |
| `python-no-ptrace` | ✅ | ✅ | Verifies `ptrace` syscall is blocked by the default seccomp profile (WDY-1099) |
| `python-no-unshare` | ✅ | ✅ | Verifies `unshare` syscall is blocked by the default seccomp profile (WDY-1099) |
| `python-multiservice` | ✅ | ✅ | Multi-service `wendy.json`: parallel build, dependency-ordered creation, and `--service` filtering |
| `otel-localhost-only` | ✅ | ✅ | Verifies OTEL receivers bind to 127.0.0.1 only (WDY-1097, WDY-1100) |

Swift tests are excluded from the default Linux suite because Swift container builds require macOS. They can still be run manually on Linux by specifying them via the `INPUT_TESTS` workflow input — the Linux runner skips any `swift-*` test when it appears in `INPUT_TESTS`.

## Device discovery

The `discover` job runs `wendy discover --json --timeout 10s` to find devices on the LAN. Discovery failures are non-fatal (`|| true`); if no devices are found the test jobs exit cleanly with a success status.

## Running tests

### Default (all registered tests)

Trigger the workflow without inputs. The macOS job runs all tests; the Linux job runs all tests except `swift-*`.

### Selective runs (`INPUT_TESTS`)

Provide a comma-separated list of test names via the `INPUT_TESTS` workflow input:

```
python-hello, otel-localhost-only
```

Each name is trimmed of whitespace. If all provided test names resolve to an empty set (e.g. no devices were found), the job exits successfully without running anything.

## Test script

Tests are executed via `scripts/test-ci.sh`. The script is invoked once per discovered device:

```sh
scripts/test-ci.sh $TEST_ARGS -h "$device"
```

where `TEST_ARGS` contains one `-t <name>` flag per test case and `-w <absolute-path-to-wendy-binary>`.

The wendy binary path is resolved to an absolute path at the start of the job to avoid working-directory-relative path issues.

## `python-no-ptrace` and `python-no-unshare` tests

Added in wendy-agent#603 to provide regression coverage for [WDY-1099](https://github.com/wendylabsinc/wendy-agent/pull/600). These are **negative tests** — they assert that certain dangerous syscalls are denied inside Wendy app containers by the default seccomp profile.

`python-no-ptrace` calls `ptrace(PTRACE_TRACEME, 0, 0, 0)` via `ctypes` and expects `EPERM`/`EACCES`. A successful call would indicate the seccomp profile was not applied.

`python-no-unshare` calls `unshare(CLONE_NEWUSER)` via `ctypes` and expects failure. `unshare(CLONE_NEWUSER)` is the first step in a common unprivileged container-escape chain; denial confirms the seccomp profile blocks the `unshare` syscall entirely.

Both tests exit `0` on a correct denial and `1` if the syscall unexpectedly succeeds.

See [OCI Entitlements — Seccomp profile](../../wendy-agent/oci/entitlements.md#seccomp-profile) for the full profile spec.

## `python-multiservice` test

Provides integration coverage for parallel multi-service build in `wendy run` (WDY-892). The test fixture is a two-service `wendy.json` project with a `db` service and an `api` service that declares `"dependsOn": ["db"]`.

The test runs three sub-cases:

1. **Full deploy** — all services are built and created via `wendy run --deploy`.
2. **`--service` filtering** — only `api` (and its dependency `db`) are built and created via `wendy run --deploy --service api`.
3. **Unknown service error** — `wendy run --deploy --service ghost` must exit non-zero and include the unknown name in its error output.

## `otel-localhost-only` test

Added in PR #598 to provide regression coverage for [WDY-1097 and WDY-1100](https://github.com/wendylabsinc/wendy-agent/pull/597). This test verifies that the OpenTelemetry (OTEL) receivers inside wendy-agent bind exclusively to `127.0.0.1` and are not reachable on external interfaces. It runs on both macOS and Linux CI runners.

See [wendy-agent/otel.md](../../wendy-agent/otel.md) for documentation on the OTEL integration.

## Planned changes (not yet shipped)

> The items below describe **planned** investments in test infrastructure. Today's CI pipeline does **not** include the Raspberry Pi 3/4 lanes or an end-to-end AI automation layer described here.

- **Raspberry Pi 3 and Pi 4 in the CI device pool** — _planned, not yet shipped_ ([WDY-1230](https://linear.app/wendylabsinc/issue/WDY-1230)). Support for Raspberry Pi 3 and Pi 4 ships in WendyOS, but the lab grid does not currently include those boards. New testing hardware needs to be sourced and added to the discoverable LAN before automated regression coverage resumes; the `discover` job will pick them up once they are online.
- **End-to-end AI-driven test automation** — _planned, not yet shipped_ ([WDY-1233](https://linear.app/wendylabsinc/issue/WDY-1233)). The intent is to replace manual smoke-testing across Mac, Windows, and Linux daemons with an AI-driven E2E framework. An internal demo of the automation layer is planned for the coming weeks. The framework, once shipped, will live alongside the existing `wendy discover` → per-test-case pipeline rather than replacing it outright.

Internal CI work is tracked as **projects** in Linear (not company-wide initiatives) so it stays distinct from customer-facing deliverables; see [WDY-1227](https://linear.app/wendylabsinc/issue/WDY-1227) for the CI infrastructure project parent.
