---
name: e2e-review-ci
description: "Fetch the latest completed Swift E2E CI run artifact, review // AI: comments and failed tests, and regenerate the run report."
---

# Review Swift E2E CI Artifacts

Use this skill in the `wendy-agent` repository when asked to review the latest
Swift E2E CI report or evaluate `// AI:` comments from CI artifacts.

## Goal

1. Determine the current git branch and associated PR, if any.
2. Fetch artifacts from the latest completed `Swift E2E Tests` workflow run for
   that branch/PR.
3. Review the Swift E2E **run** artifact with `swift/Scripts/E2EReview.sh`.
4. Regenerate `index.html` with `swift/Scripts/E2EReport.sh` so run, suite, and
   test review summaries render inline.

Terminology:

- **attempt**: one raw matrix target execution artifact.
- **run**: the canonical merged E2E result directory produced by aggregating
  attempts.
- **report**: `index.html` rendered inside the run directory.

## Fetch the Latest Completed CI Artifacts

Use the helper script in this skill directory:

```bash
.agents/skills/e2e-review-ci/fetch-latest-swift-e2e-artifacts.sh
```

The helper determines the current branch, looks up the associated PR when one is
visible to `gh`, finds the latest completed `swift-e2e-tests.yml` run for that
branch, and downloads Swift E2E run artifacts into:

```text
swift/Build/e2e-ci-review/run-<run-id>/artifacts/
```

It also writes run metadata to:

```text
swift/Build/e2e-ci-review/run-<run-id>/metadata.json
```

Useful overrides:

```bash
.agents/skills/e2e-review-ci/fetch-latest-swift-e2e-artifacts.sh \
  --repo wendylabsinc/wendy-agent \
  --branch kb.swift-e2e-tests
```

## Review and Render

Downloaded artifacts should contain the merged Swift E2E run directory. From
`swift/`, run:

```bash
cd swift
for run_dir in Build/e2e-ci-review/run-*/artifacts/swift-e2e-tests.*.run.*; do
  [ -f "$run_dir/info.json" ] || continue
  bash Scripts/E2EReview.sh --run-dir "$run_dir" --provider auto
  bash Scripts/E2EReport.sh --run-dir "$run_dir"
done
```

Use `--provider claude` with `ANTHROPIC_API_KEY`, or `--provider codex` with
`OPENAI_API_KEY`, to force a provider. Use `--overwrite` to replace existing
run/suite/test review outputs.

The reviewer writes paired Markdown files only for actionable findings:

```text
<run>/review.summary.md
<run>/review.details.md
<run>/<suite>/review.summary.md
<run>/<suite>/review.details.md
<run>/<suite>/<test>/review.summary.md
<run>/<suite>/<test>/review.details.md
```

The report renderer reads those files, tags reviewed scopes with the AI badge,
and displays summaries inline with details links.

## Final Response

Summarize:

- branch and PR reviewed
- workflow run URL and conclusion
- run artifact directories reviewed
- where regenerated `index.html` files were written
- any noteworthy review findings
