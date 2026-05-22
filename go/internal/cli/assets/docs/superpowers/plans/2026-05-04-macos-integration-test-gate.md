# macOS Integration Test Gate Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Gate stable releases (`publish=true`) on macOS hardware integration tests passing, leaving nightly prereleases unaffected.

**Architecture:** Three edits to `.github/workflows/build.yml` only — add a new `integration-tests` job that calls the existing `integration-tests.yml` workflow via `workflow_call`, then add it to the `needs` of `release` and `publish-linux-repos`. The `release` job's `if` condition is updated to allow the skipped result so nightly builds continue to work.

**Tech Stack:** GitHub Actions YAML (`workflow_call`, job `needs`, job `if` with result checks)

---

### Task 1: Add `integration-tests` gate job to `build.yml`

**Files:**
- Modify: `.github/workflows/build.yml:647` (insert new job before `release:`)

This job calls the existing `integration-tests.yml` workflow. It only runs when `publish == true`. It passes `platform: macos` and `jobs: integration` so only the macOS integration tests run (not the discover job, not the Linux integration job). No `hostname` or `tests` inputs means auto-discover all LAN devices and run the full default test suite.

- [ ] **Step 1: Open `.github/workflows/build.yml` and locate the `release:` job** (currently near line 647). Insert the following block immediately before it (preserve the two-space indentation that all other jobs use):

```yaml
  integration-tests:
    name: Integration Tests (macOS, stable gate)
    if: github.event_name == 'workflow_dispatch' && inputs.publish == true
    uses: ./.github/workflows/integration-tests.yml
    with:
      platform: macos
      jobs: integration
```

- [ ] **Step 2: Verify the YAML is valid**

```bash
cd /Users/joannisorlandos/git/wendy/wendy-agent
python3 -c "import yaml; yaml.safe_load(open('.github/workflows/build.yml'))" && echo "YAML OK"
```

Expected: `YAML OK`

---

### Task 2: Update the `release` job to require the gate

**Files:**
- Modify: `.github/workflows/build.yml` — `release` job `needs` and `if` fields

The `release` job currently has:
```yaml
    needs: [determine-version, build, build-go-macos, build-agent-macos-app, package-linux, package-windows, test-templates]
    if: github.event_name == 'push' || (github.event_name == 'workflow_dispatch' && inputs.publish == true)
```

- [ ] **Step 1: Update the `release` job's `needs` line** to include `integration-tests`:

```yaml
    needs: [determine-version, build, build-go-macos, build-agent-macos-app, package-linux, package-windows, test-templates, integration-tests]
```

- [ ] **Step 2: Update the `release` job's `if` condition** to allow a skipped `integration-tests` result (so nightly builds still release when the gate job is skipped):

```yaml
    if: |
      (github.event_name == 'push' || (github.event_name == 'workflow_dispatch' && inputs.publish == true)) &&
      (needs.integration-tests.result == 'success' || needs.integration-tests.result == 'skipped')
```

- [ ] **Step 3: Verify the YAML is valid**

```bash
python3 -c "import yaml; yaml.safe_load(open('.github/workflows/build.yml'))" && echo "YAML OK"
```

Expected: `YAML OK`

---

### Task 3: Update `publish-linux-repos` to require the gate

**Files:**
- Modify: `.github/workflows/build.yml` — `publish-linux-repos` job `needs` field

`publish-linux-repos` runs independently of `release` (parallel path), so it needs its own gate. It currently has:
```yaml
    needs: [determine-version, package-linux, test-templates]
    if: needs.determine-version.outputs.is_release == 'true' && needs.determine-version.outputs.is_prerelease == 'false'
```

On nightly builds, `integration-tests` is skipped and `is_release == 'false'`, so this job was already being skipped — adding `integration-tests` to `needs` does not change nightly behavior. No `if` change needed.

- [ ] **Step 1: Update the `publish-linux-repos` job's `needs` line** to include `integration-tests`:

```yaml
    needs: [determine-version, package-linux, test-templates, integration-tests]
```

- [ ] **Step 2: Verify the YAML is valid**

```bash
python3 -c "import yaml; yaml.safe_load(open('.github/workflows/build.yml'))" && echo "YAML OK"
```

Expected: `YAML OK`

- [ ] **Step 3: Commit all changes**

```bash
git add .github/workflows/build.yml
git commit -m "ci: gate stable releases on macOS integration tests"
```

---

## Verification Checklist

After the commit, confirm the logic by reading the final state of `build.yml`:

| Scenario | `integration-tests` result | `release` runs? | `publish-linux-repos` runs? |
|---|---|---|---|
| Push to main (nightly) | skipped | yes (skipped → allowed) | no (is_release=false) |
| `workflow_dispatch` publish=false | skipped | yes (skipped → allowed) | no (is_release=false) |
| `workflow_dispatch` publish=true, tests pass | success | yes | yes |
| `workflow_dispatch` publish=true, tests fail | failure | no (blocked) | no (blocked) |
| `workflow_dispatch` publish=true, no devices | failure | no (blocked) | no (blocked) |
