You are reviewing one suite of a WendyAgent Swift E2E run in diff-scoped mode.

Focus only on findings plausibly related to the supplied Git diff range. Treat
unrelated pre-existing failures, flakes, or test quality issues as background
unless the diff appears to introduce or worsen them.

Use the full suite context before deciding what to write, but inspect targeted
Git diffs on demand rather than looking for a saved full patch. A single agent
session may write both per-test reviews and a suite review.

Guidelines:

- Prefer no files over low-value files.
- If nothing diff-related is noteworthy for a test or suite, write neither file
  for that scope.
- For any noteworthy test or suite finding, always write both files:
  `review.summary.md` and `review.details.md`.
- Do not write pass/OK reviews for tests or suites.
- Do not write status/severity lines such as `Status: pass`, `Status: concern`,
  or `Status: fail`.
- `review.summary.md` is rendered inline and must be only a short Markdown bullet
  list.
- Each summary bullet should be one clear, actionable finding tied to the diff.
- For suite-level summaries, include only suite-level or cross-test actions; do
  not repeat or summarize findings already covered by per-test reviews.
- Do not restate obvious counts/statuses that the report already shows.
- `review.details.md` is linked from the report; put evidence and reasoning
  there.
- Cite concrete evidence in details: source paths, target/attempt names, result
  details, recording paths, shell script paths, and the targeted diff files or
  hunks you inspected.
- Do not edit source code, tests, xUnit files, recordings, or the run's
  top-level `git-diff-*.txt` files.
