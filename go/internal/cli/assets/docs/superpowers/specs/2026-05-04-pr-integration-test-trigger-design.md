# PR Integration Test Trigger Design

**Date:** 2026-05-04  
**Status:** Approved

## Goal

Run macOS hardware integration tests automatically on any PR that touches `.github/**`, and make the check required before merging.

## Architecture

A single new workflow file `.github/workflows/pr-integration-tests.yml` with a `pull_request` trigger path-filtered to `.github/**`. It delegates to the existing `integration-tests.yml` via `workflow_call`. No existing workflows are modified.

## Components

### New file: `.github/workflows/pr-integration-tests.yml`

```yaml
name: PR Integration Tests

on:
  pull_request:
    paths:
      - '.github/**'

jobs:
  integration-tests:
    name: Integration Tests (macOS)
    uses: ./.github/workflows/integration-tests.yml
    with:
      platform: macos
      jobs: integration
```

- **Trigger:** `pull_request` on `.github/**` — fires on open, reopen, and synchronize events
- **Inputs:** `platform: macos`, `jobs: integration` — macOS only, full default test suite, auto-discover all LAN devices
- **No `if` condition** — unlike the stable release gate, every qualifying PR runs unconditionally

### Required status check (manual step post-merge)

After this workflow runs once on a PR (or is manually registered via GitHub API), add `Integration Tests (macOS)` as a required status check under **Settings → Branches → main → Require status checks to pass**. This is a one-time manual step and cannot be configured via workflow YAML.

## Behavior

| Scenario | Result |
|---|---|
| PR touches `.github/**` | Integration tests run on macOS hardware |
| PR does not touch `.github/**` | Workflow not triggered |
| Tests pass | PR check passes, merge allowed |
| Tests fail | PR check fails, merge blocked |
| No devices discovered | `integration-tests.yml` hard-fails, PR check fails |

## What is not changing

- `integration-tests.yml` — untouched, remains a pure reusable callable workflow
- `build.yml` — untouched
