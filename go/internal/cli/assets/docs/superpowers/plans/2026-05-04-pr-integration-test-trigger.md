# PR Integration Test Trigger Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Create a new workflow that runs macOS integration tests on every PR touching `.github/**`.

**Architecture:** A single new file `.github/workflows/pr-integration-tests.yml` with a `pull_request` trigger path-filtered to `.github/**`. It delegates entirely to the existing `integration-tests.yml` via `workflow_call`. No existing files are modified.

**Tech Stack:** GitHub Actions YAML (`pull_request` trigger, `paths` filter, `workflow_call`)

---

### Task 1: Create `pr-integration-tests.yml`

**Files:**
- Create: `.github/workflows/pr-integration-tests.yml`

- [ ] **Step 1: Create the workflow file**

Create `.github/workflows/pr-integration-tests.yml` with exactly this content:

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

- [ ] **Step 2: Validate YAML syntax**

```bash
python3 -c "import yaml; yaml.safe_load(open('.github/workflows/pr-integration-tests.yml'))" && echo "YAML OK"
```

Expected: `YAML OK`

- [ ] **Step 3: Verify the called workflow accepts these inputs**

```bash
python3 -c "
import yaml
wf = yaml.safe_load(open('.github/workflows/integration-tests.yml'))
inputs = wf['on']['workflow_call']['inputs']
assert 'platform' in inputs, 'missing platform input'
assert 'jobs' in inputs, 'missing jobs input'
print('Inputs OK:', list(inputs.keys()))
"
```

Expected output includes `platform` and `jobs` in the list.

- [ ] **Step 4: Commit**

```bash
git add .github/workflows/pr-integration-tests.yml
git commit -m "ci: run integration tests on PRs touching .github/**"
```

---

## Post-Merge Manual Step

After this PR is merged and the workflow has run at least once, add `Integration Tests (macOS)` as a required status check:

**GitHub UI:** Settings → Branches → Edit protection rule for `main` → "Require status checks to pass before merging" → search for `Integration Tests (macOS)` → Save.

This cannot be done via workflow YAML — it is a repository settings change.
