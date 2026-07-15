# WIP — WDY-1823 Security Review Style

## Task

- **Issue:** WDY-1823 — [Make AI security review stateful, silenceable, and glanceable](https://linear.app/wendylabsinc/issue/WDY-1823/make-ai-security-review-stateful-silenceable-and-glanceable)
- **Project / PM:** AI Workflow (`/Volumes/Projects/WendyLabs/WIP/ai-workflow/PM.md`)
- **Goal:** Make `.github/workflows/security-review.yml` match the glanceable AI Docs/E2E review style while preserving prior findings, supporting explicit risk acceptance, and blocking only open unsilenced HIGH/CRITICAL findings.

## Development lane

- **Branch:** `kb.security-review-style`
- **Worktree:** `/Volumes/Projects/WendyLabs/Worktrees/ai-workflow/kb.security-review-style/WendyOS`
- **Base branch:** `main`
- **Draft PR:** [#1436 — Make AI security review stateful, silenceable, and glanceable](https://github.com/wendylabsinc/WendyOS/pull/1436)
- **Developer workspace:** `AI Workflow > security-review-style`
- **Developer pane:** `agent`

## Scope

- Render one glanceable entry per security finding with details in a per-finding disclosure.
- Carry previous bot-comment findings into re-review and show `open`, `addressed`, `cancelled`, and `silenced` state.
- Support nearby `SECURITY: <reason>` developer comments as explicit, visible, non-blocking risk acceptance.
- Fail the check only for open unsilenced HIGH/CRITICAL findings.
- Keep one stable bot-owned PR comment and update it in place.
- Document the changed contributor-facing workflow behavior.

## Non-goals

- Redesign other AI review workflows.
- Change the security/compliance frameworks reviewed by Claude.
- Add a general-purpose workflow framework or move embedded Python into a separate package.

## Decisions and rationale

- Use structured JSON model output so finding status and gate behavior do not depend on parsing free-form Markdown.
- Preserve a stable `<!-- ai-security-review:v1 -->` marker and edit the existing issue comment rather than posting review noise on each push.
- Treat PR content, prior review state, and repair input as untrusted prompt sections.
- Keep prior state as a model hint, but sanitize it and require current-diff evidence before changing status.
- Detect nearby `SECURITY:` comments in the diff in the renderer as well as the prompt, making silencing deterministic even if the model misses the comment.
- Isolate `GH_TOKEN` to state-fetch and comment-posting steps so model-facing code does not receive it explicitly.
- Repair malformed model output once, then fail closed rather than accidentally clearing unresolved state.
- Keep the PR draft until PM/Konstantin review; remove this temporary file before marking ready for review.

## Implementation status

Implementation is complete and pushed. Meaningful commits after the adopted PM checkpoint include:

- `8f19c95e` — harden state handling and make statuses visible.
- `3d01eeb5` — add one-shot repair for non-JSON model responses.
- `a1c26aa4` — isolate token use and validate API inputs.
- `a4b14ffd` — clean rendering, escaping, metadata, and byte truncation edge cases.
- `4bd62ef5` — enforce nearby `SECURITY:` silencing in the renderer.
- `42219c81` — document the new workflow behavior.
- `f1b04a36` — prevent duplicate silencing reasons across re-reviews.

The original adopted checkpoint commits are `ef13f5c7` and `5a7a5654`.

## Validation and CI

- Compiled all four embedded Python heredocs locally.
- Ran local fake-Anthropic harnesses covering:
  - direct and repaired JSON parsing;
  - glanceable open/silenced rendering;
  - open HIGH/CRITICAL gate failure state;
  - silenced HIGH/CRITICAL non-blocking state;
  - deterministic nearby `SECURITY:` detection;
  - malformed-after-repair fail-closed behavior.
- `actionlint` is not installed locally.
- Latest GitHub checks on PR #1436 are green, including Claude Security Review, CodeQL, Go tests/vet/format, Swift tests/build/lint, integration tests, docs build/deploy, Docs Review, and Integration Test Review.
- PR #1436 currently reports a clean merge state and remains draft.

## Blockers and unknowns

- No implementation or CI blocker.
- Linear CLI was unavailable in this environment; issue details were taken from the kickoff handoff and PM context.
- The current bot comment retains non-blocking informational findings and explicitly silenced findings by design.

## Next steps

1. PM/Konstantin reviews draft PR #1436 and its rendered security comment.
2. Address requested review changes, if any, and keep this file current while the lane remains active.
3. Before marking the PR ready for review, move any remaining durable context to the PR/PM/docs and delete `WIP.md` in a final cleanup commit.

## Handoff / recovery

Start in the worktree above, confirm branch `kb.security-review-style`, read this file and PR #1436, then inspect `.github/workflows/security-review.yml` and `go/internal/cli/assets/docs/development/contributing.md`. The branch is pushed and clean at the latest checkpoint. Re-run embedded-Python compilation and the relevant GitHub checks after workflow changes. Do not mark the PR ready while this temporary `WIP.md` is tracked.
