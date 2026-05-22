---
name: run-e2e-tests-and-analyze
description: Run WendyAgent Swift E2E attempts, aggregate them into a run, review // AI: comments, and render the HTML report.
---

# Run Swift E2E Tests and Analyze the Run

Use this skill in the `wendy-agent` repository when asked to run Swift E2E tests,
inspect E2E recordings, evaluate `// AI:` comments, or produce AI review results.

Terminology:

- **attempt**: one raw target execution produced by the test harness.
- **run**: the canonical merged result directory produced from one or more
  attempts.
- **report**: `index.html` rendered inside the run directory.

## Common Local Workflow

From `swift/WendyE2ETests`:

```bash
make e2e-test
make e2e-analyze
```

Or run the stages separately:

```bash
make e2e-aggregate
make e2e-review
make e2e-report
```

The analysis step aggregates matching attempts into a run, reviews the run, and
renders the report.

## Direct Script Workflow

From `swift/`:

```bash
Scripts/E2ETest.sh --run-id <attempt-id>
Scripts/E2EAnalyze.sh --run-id <run-id>
```

`Scripts/E2EAnalyze.sh` discovers attempt directories, aggregates them into the
matching run directory, runs AI review, and renders `index.html`.

## AI Review

`Scripts/E2EReview.sh --run-dir <run-dir>` invokes `swift-e2e-testing review`.
The reviewer reads source `// AI:` comments, failed attempt observations,
recordings, xUnit results, and existing run artifacts. It writes paired review
files only for actionable findings:

```text
<run>/review.summary.md
<run>/review.details.md
<run>/<suite>/review.summary.md
<run>/<suite>/review.details.md
<run>/<suite>/<test>/review.summary.md
<run>/<suite>/<test>/review.details.md
```

Passing or purely informational items should not produce review files.

## Report

`Scripts/E2EReport.sh --run-dir <run-dir>` renders the Swift E2E HTML report at:

```text
<run>/index.html
```

The report shows run, suite, and test review summaries inline and links to the
corresponding details files.

## Manual Inspection

A run is laid out as:

```text
<run>/<suite>/<test>/<target>/<attempt>/recording.md
<run>/<suite>/<test>/<target>/<attempt>/recording.sh.txt
<run>/<suite>/<test>/<target>/<attempt>/test-results.xml
```

Use `recording.md` for human-readable evidence and `recording.sh.txt` to replay
captured shell commands manually.
