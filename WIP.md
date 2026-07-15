# WDY-1823 — Make AI security review stateful, silenceable, and glanceable

- **Issue:** [WDY-1823](https://linear.app/wendylabsinc/issue/WDY-1823/make-ai-security-review-stateful-silenceable-and-glanceable)
- **Draft PR:** [WendyOS #1436](https://github.com/wendylabsinc/WendyOS/pull/1436)
- **Branch:** `kb.security-review-style`
- **Worktree:** `/Volumes/Projects/WendyLabs/Worktrees/ai-workflow/kb.security-review-style/WendyOS`

Konstantin requested that AI Security Review match the AI Docs/E2E review comment style: one glanceable line per issue, per-finding details disclosures, stateful re-review statuses, `// SECURITY: <reason>` silencing, and a gate that counts only open unsilenced HIGH/CRITICAL findings.

Keep PR #1436 draft until it is ready, and keep its title exactly `Make AI security review stateful, silenceable, and glanceable`. Konstantin chose Claude 4.8 for this workflow; Anthropic exposes it as `claude-opus-4-8`.

## Committed work

- `5a7a5654` — initial glanceable structured security-review output adopted from the PM checkpoint.
- `8f19c95e`, `3d01eeb5`, `a1c26aa4`, `a4b14ffd` — state handling, malformed-response repair, token isolation, and rendering hardening.
- `4bd62ef5`, `f1b04a36` — deterministic nearby `SECURITY:` silencing without repeated reasons.
- `42219c81` — contributor documentation for the stateful review format and gate.
