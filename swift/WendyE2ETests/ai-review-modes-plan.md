# Swift E2E AI review modes plan

## Goal

Support two AI review modes without creating separate review commands:

- **full review**: review the whole Swift E2E run and repository state.
- **diff review**: review only findings plausibly related to a supplied Git diff
  range.

`review` should infer the mode from the presence of `--diff`:

```bash
swift-e2e-testing review --run-dir <run-dir>                  # full
swift-e2e-testing review --run-dir <run-dir> --diff A...B     # diff
```

The wrapper should mirror this:

```bash
Scripts/E2EReview.sh --run-dir <run-dir>
Scripts/E2EReview.sh --run-dir <run-dir> --diff origin/main...HEAD
```

## Terminology

- **attempt**: one concrete target/platform execution produced by the test
  harness.
- **run**: the canonical merged result directory produced from one or more
  attempts.
- **report**: `index.html` rendered inside the run directory.
- **diff review**: review constrained to changes in a Git diff range.
- **full review**: unconstrained review of the whole run/repo state.

## CLI changes

Add an optional argument to `swift-e2e-testing review`:

```swift
@Option(name: .long, help: "Git diff range for diff-scoped review, for example origin/main...HEAD.")
var diff: String?
```

No separate `--mode` flag is needed:

- `diff == nil` -> full mode
- `diff != nil` -> diff mode

Pass the option through wrappers:

- `swift/Scripts/E2EReview.sh`
- `swift/Scripts/E2EReview.ps1`
- any analyze/CI call sites that need diff review

`Scripts/E2EAnalyze.*` can accept and forward extra args, or add a dedicated
`--diff` option if that is clearer.

## GitHub Actions usage

For PR/diff-style CI reviews, use:

```yaml
- uses: actions/checkout@v6
  with:
    fetch-depth: 0

- name: Review Swift E2E run
  working-directory: swift
  run: |
    bash ./Scripts/E2EReview.sh \
      --run-dir "${{ steps.e2e-run.outputs.run_dir }}" \
      --diff "origin/main...HEAD"
```

`git` is available on standard GitHub-hosted runners. The important part is
ensuring enough history exists for the diff range / merge base, either with
`fetch-depth: 0` or an explicit fetch of the base ref.

## Review context files

Do **not** write the full patch by default; it may be huge.

Create lightweight context under the run directory only when `--diff` is given:

```text
<run>/git-diff-name-only.txt
<run>/git-diff-stat.txt
```

Do not write a separate mode manifest. Full mode writes no diff context files.

Generate diff context mechanically before invoking the agent:

```bash
git diff --name-only "$DIFF" > "$RUN_DIR/git-diff-name-only.txt"
git diff --stat "$DIFF" > "$RUN_DIR/git-diff-stat.txt"
```

If those commands fail, fail review early with a clear error explaining the diff
range could not be resolved.

## Prompt files

Keep mode-specific prompt files on disk:

```text
swift/WendyE2ETests/Support/
  e2e-review-suite.full.prompt.md
  e2e-review-report.full.prompt.md
  e2e-review-suite.diff.prompt.md
  e2e-review-report.diff.prompt.md
```

Current prompts can become the `full` prompts. The `diff` prompts should add the
constraint:

> Only report findings plausibly related to the supplied Git diff range. Treat
> unrelated pre-existing failures, flakes, or test quality issues as background
> unless the diff appears to introduce or worsen them.

The generated agent prompt should include:

- review mode
- for diff mode, paths to `<run>/git-diff-name-only.txt` and
  `<run>/git-diff-stat.txt`
- recommended targeted commands:

```bash
git diff --stat <range>
git diff --name-only <range>
git diff <range> -- <specific-file>
git diff <range> -U80 -- <specific-file>
```

The agent should not be asked to read a full saved patch by default.

## Review output files

Keep current Markdown outputs for report rendering:

```text
<run>/review.summary.md
<run>/review.details.md
<run>/<suite>/review.summary.md
<run>/<suite>/review.details.md
<run>/<suite>/<test>/review.summary.md
<run>/<suite>/<test>/review.details.md
```

Future automation can add structured output:

```text
<run>/review.findings.json
```

Do not implement side effects in `review` itself. Keep diagnosis and actions
separate.

## Action layer, later

A later command/script can consume review outputs and mode context:

```bash
swift-e2e-testing review-actions --run-dir <run-dir>
# or
Scripts/E2EReviewActions.sh --run-dir <run-dir>
```

Policy examples:

- diff mode: PR/diff comments, check annotations, optional stacked fix PR
- full mode: Slack alert, GitHub issue, optional fix PR to main

## Implementation steps

1. Add `diff: String?` to `ReviewCommand`.
2. Add a small `ReviewMode` model derived from `diff`.
3. In diff mode, write top-level `git-diff-name-only.txt` and
   `git-diff-stat.txt` files.
4. Generate those files mechanically with `git diff`.
5. Split current prompt files into `.full.prompt.md` equivalents.
6. Add `.diff.prompt.md` prompt files with diff-scoped instructions.
7. Select prompt files based on mode unless explicitly overridden.
8. Include review context paths and diff commands in generated suite/report
   prompts.
9. Pass `--diff` through `E2EReview.sh` and `E2EReview.ps1`.
10. Update CI call sites that should run diff review.
11. Validate:

```bash
cd swift/WendyE2ETests
swift build
swift run swift-e2e-testing review --help
swift run swift-e2e-testing review --run-dir <run> --provider none
swift run swift-e2e-testing review --run-dir <run> --diff origin/main...HEAD --provider none
```

Also run shell syntax checks:

```bash
bash -n swift/Scripts/E2EReview.sh swift/Scripts/E2EAnalyze.sh
```
