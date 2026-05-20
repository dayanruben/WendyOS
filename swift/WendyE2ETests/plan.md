# Swift E2E aggregate plan

## Current run ID shape

Individual E2E runs use this logical ID shape:

```text
<workflow-name>.<run-id>.<target-name>.<attempt>
```

Examples:

```text
swift-e2e-tests.local260520.macos-26-to-jetson-orin-nano.0001
swift-e2e-tests.gh1234567890.macos-26-to-jetson-orin-nano.0001
```

Parts:

- `workflow-name`: the spec/workflow family, currently `swift-e2e-tests`
- `run-id`: `local<yy><mm><dd>` locally, or `gh<github-run-id>` in GitHub Actions
- `target-name`: one concrete E2E target, such as `macos-26-to-jetson-orin-nano`
- `attempt`: four-digit attempt number, such as `0001`

## Raw individual run shape

A raw individual run remains records-only:

```text
<run-id>/
  info.json
  test-results.xml
  tests/
    <suite-key>/
      <test-key>/
        recording.md
        recording.sh.txt
        review.md        # optional
        cli/
        agent/
```

No binaries are stored in the run directory. Reports are rendered only from aggregate directories.

## Canonical aggregate shape

The aggregate layout transposes individual runs into a test-first hierarchy:

```text
<workflow-name>.<run-id>/
  info.json
  index.html
  review.md              # aggregate-level review/actions

  <suite-key>/
    info.json
    review.md            # optional suite-level review

    <test-key>/
      info.json
      review.md          # cross-target/cross-attempt test review

      <target-name>/
        info.json
        review.md        # optional target-specific review for this test

        <attempt>/
          info.json
          review.md      # optional exact-observation review
          recording.md
          recording.sh.txt
          cli/
          agent/
```

Example:

```text
swift-e2e-tests.local260520/
  wendy-completion-zsh/
    prints-command-help/
      macos-26-to-jetson-orin-nano/
        0001/
          info.json
          recording.md
          recording.sh.txt
          cli/
            home/
            tmp/
```

This is isomorphic with a target-first view:

```text
<workflow-name>.<run-id>/<target-name>/<attempt>/<suite-key>/<test-key>
```

but the canonical aggregate is test-first because it is better for review:

- root `review.md`: whole run/matrix summary
- suite `review.md`: suite-wide concerns
- test `review.md`: cross-target/cross-attempt test concerns
- target `review.md`: target-specific behavior for that test
- attempt `review.md`: exact recorded observation concerns

## Integration rule

Given a raw run ID:

```text
<workflow-name>.<run-id>.<target-name>.<attempt>
```

and a raw test directory:

```text
<raw-run>/tests/<suite-key>/<test-key>/
```

integrate it into the aggregate at:

```text
<workflow-name>.<run-id>/<suite-key>/<test-key>/<target-name>/<attempt>/
```

The raw files copied/moved into that observation directory are:

```text
recording.md
recording.sh.txt
review.md        # if present
cli/
agent/
```

Run-level files such as `info.json` and `test-results.xml` should be folded into scoped `info.json` files at the aggregate root, suite, test, target, or attempt level as appropriate.

## Command model

The E2E workflow should be split into four explicit commands/steps:

1. `test`
   - Runs deterministic Swift E2E tests.
   - Produces one raw individual run directory.
   - Does not review, aggregate, or render reports.

2. `aggregate`
   - Reads one or more raw individual run directories.
   - Parses each run ID into `<workflow-name>`, `<run-id>`, `<target-name>`, and `<attempt>`.
   - Transposes raw run artifacts into the canonical aggregate layout.
   - Writes deterministic `info.json` files at the aggregate, suite, test, target, and attempt levels.

3. `review`
   - Reads an aggregate directory.
   - Writes scoped `review.md` files at the appropriate levels.
   - Keeps review separate from deterministic test execution.

4. `report`
   - Reads an aggregate directory.
   - Writes the single aggregate `index.html`.
   - Covers both one-run and multi-run aggregates.

`make` targets and CI jobs should compose these steps rather than combining their responsibilities inside the test step.

## Implementation iterations

### Iteration 1: transpose existing per-run artifacts — done

The first implementation maps the current raw run output onto the aggregate hierarchy without changing the per-run review and report behavior.

`aggregate` should be fully implemented for the current artifact set:

- Read one or more raw run directories.
- Parse each raw run ID into `<workflow-name>`, `<run-id>`, `<target-name>`, and `<attempt>`.
- Create the aggregate root `<workflow-name>.<run-id>/`.
- Place each raw run under the test-first aggregate path:

  ```text
  <workflow-name>.<run-id>/<suite-key>/<test-key>/<target-name>/<attempt>/
  ```

- Preserve the current per-run files in that mapped location, including `recording.md`, `recording.sh.txt`, `review.md`, `cli/`, and `agent/`.

`review` is disabled while the aggregate report flow is being reshaped:

- Do not write per-observation `review.md` files from aggregate runs for now.
- Reintroduce review later as cross-target/cross-attempt review, not per-observation review.

`report` is now aggregate-first:

- Render a single top-level aggregate `index.html` under `<workflow-name>.<run-id>/`.
- Keep the suite-grouped report UI with filters and search.
- Each test row links to `<suite-key>/<test-key>/index.html` instead of expanding inline.
- Per-test pages are placeholders that say `Not implemented yet` until the detailed test view is designed.
- Per-test Shell, Record, and Report buttons are not rendered.

This gives us a complete aggregate command and makes the aggregate report the canonical report surface.

Completed pieces:

- `swift-e2e-testing aggregate` writes the test-first aggregate hierarchy.
- `Scripts/E2EAggregate.sh` wraps the aggregate command.
- Aggregate AI review is disabled for now.
- `Scripts/E2EReport.sh` renders the aggregate `index.html` directly.
- Local `make e2e-run*` composes test → aggregate → review → report.
- CI matrix jobs upload raw runs, and the aggregate job downloads, aggregates, reviews, reports, and uploads the aggregate.

## Open plumbing questions

We still need to decide implementation details:

1. Whether `aggregate`, `review`, and `report` live entirely in `swift-e2e-testing`, shell wrappers, or both.
2. Whether raw CI artifacts are uploaded as individual runs, aggregate only, or both during transition.
3. How local `make` targets should expose each step and composed workflows such as `e2e-run`.
