# Swift E2E run/report plan

This package treats a Swift E2E result as two layers:

- **attempt**: one concrete target/platform execution produced by the test
  harness.
- **run**: the canonical merged result directory produced from one or more
  attempts.

The `aggregate` command is the transition from attempts to a run. `review` and
`report` operate on the run.

## Attempt IDs

Attempt directories use this shape:

```text
<workflow-name>.<run-id>.<target-name>.<attempt>
```

- `workflow-name`: stable workflow slug, for example `swift-e2e-tests`
- `run-id`: CI run ID or local evaluation ID
- `target-name`: matrix target such as `ubuntu-raspberry-pi-5`
- `attempt`: four-digit attempt number such as `0001`

## Run layout

Runs are test-first:

```text
<workflow-name>.<run-id>/
  info.json
  index.html
  review.summary.md      # optional run-level review summary
  review.details.md      # optional run-level review details
  <suite-key>/
    review.summary.md    # optional suite-level review summary
    review.details.md    # optional suite-level review details
    <test-key>/
      review.summary.md  # optional test-level review summary
      review.details.md  # optional test-level review details
      <target-name>/
        <attempt>/
          recording.md
          recording.sh.txt
          test-results.xml
          test-results.raw.xml  # optional sanitized original
          info.json
          cli/
          agent/
```

No product binaries are stored in the run. Reports are rendered from the run
directory.

## Commands

### `test`

Produces one attempt directory. It does not review or render reports.

### `aggregate`

Aggregates one or more attempt directories into the canonical run layout:

```bash
swift-e2e-testing aggregate --output-dir /tmp/wendy <attempt-dir>...
```

The command writes `<output>/<workflow-name>.<run-id>/info.json` with
`kind: "swift-e2e-run"`.

### `review`

Reviews a run directory:

```bash
swift-e2e-testing review --run-dir /tmp/wendy/<workflow-name>.<run-id> \
  --suite-review-prompt Support/e2e-review-suite.prompt.md \
  --report-review-prompt Support/e2e-review-report.prompt.md
```

Review runs in two stages:

1. Suite-scoped review may write paired review files at suite or test scope.
2. Run-level review may write paired review files at the run root.

Review files are written only for actionable findings. Passing or purely
informational observations should leave review files absent.

### `report`

Renders the HTML report for a run:

```bash
swift-e2e-testing report --run-dir /tmp/wendy/<workflow-name>.<run-id>
```

The report reads attempt recordings/results and review summary/details files,
then writes `<run>/index.html`.

## Target outcome model

For each test and target:

- all attempts passed → target is `passed`
- some attempts passed and some failed → target is `flaked`
- all attempts failed → target is `failed`
- all attempts skipped → target is `skipped`
- missing or mixed unknown observations → target is `unknown`

A concrete attempt can only render as passed, failed, skipped, or unknown.
`Flaked` is derived at the target level.

## Local workflow

```text
test attempt(s) → aggregate run → review run → render report
```

The convenience scripts and make targets preserve these explicit stages:

```bash
make e2e-test
make e2e-analyze
```

or:

```bash
make e2e-aggregate
make e2e-review
make e2e-report
```
