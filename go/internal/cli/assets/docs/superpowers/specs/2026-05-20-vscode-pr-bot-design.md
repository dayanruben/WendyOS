# VSCode Extension PR Bot Design

**Date:** 2026-05-20  
**Status:** Approved

## Overview

A GitHub Actions workflow in `wendy-agent` that triggers when CLI command files are merged, uses Claude to analyze whether the VSCode extension needs updating, and opens a proposal PR to `wendylabsinc/wendy-vscode` containing both generated code changes and a human-readable description.

Modelled directly on the existing `docs-update.yml` workflow.

## Trigger & Scope

- **Event:** `pull_request` closed + merged to `main`
- **Path filter:** `go/internal/cli/commands/**` — skips PRs that don't touch CLI commands
- **Loop guard:** skips PRs labelled `ai-suggestion` (prevents bot-generated PRs from triggering another run)

## Context Fed to Claude

1. **PR diff** — raw output of `gh pr diff <number>` (capped at 30k chars)
2. **VSCode extension source files** — ranked by keyword relevance against the diff + PR title, capped at 50k chars total. Candidates: `src/wendy-cli/wendy-cli.ts`, `src/tasks/WendyTaskProvider.ts`, `src/sidebar/*.ts`, `src/extension.ts`, `package.json`
3. **File tree** — compact 3-level tree of the extension repo so Claude knows what exists

Relevance scoring: keyword overlap between diff/title and file path + first 1500 chars of content (same algorithm as docs bot, path hits weighted 3×).

## Claude's Output Format

Two output types, both optional:

### `<description>` block
Human-readable explanation used verbatim as the PR body. Covers:
- What CLI change occurred
- Whether the VSCode extension needs updating and why
- What specifically should change (which files, what behaviour)

If Claude outputs no `<description>`, the workflow exits cleanly with no PR opened.

### `<file path="...">content</file>` blocks
Full replacement content for files that need changing. Same format as the docs bot. Can be TypeScript source or `package.json`. Protected from modification:
- `package-lock.json`
- `dist/**`
- `node_modules/**`
- Any path outside the repo root
- Any non-`.ts`/`.json`/`.md` file

## Workflow Mechanics

- **Permissions (wendy-agent side):** `contents: read`, `pull-requests: read`
- **Cross-repo access:** `WENDY_TEMPLATE_SYNC_TOKEN` secret — same token used by docs bot
- **Checkout:** `wendylabsinc/wendy-vscode` into a `vscode` working directory
- **Empty diff guard:** exits early if diff is empty
- **Nothing to do:** if Claude outputs neither block, prints "no VSCode changes needed" and exits without opening a PR
- **PR branch:** `vscode-update/<repo-name>-<pr-number>`
- **PR label:** `ai-suggestion`
- **PR deduplication:** handled by `peter-evans/create-pull-request@v8` — updates existing branch if present
- **Model:** `claude-sonnet-4-6` with prompt caching on the system prompt

## System Prompt (Summary)

Claude is instructed to act as a VSCode extension maintainer for WendyOS. Rules:
- Only propose changes directly caused by the CLI diff
- Output `<description>` if the extension needs work; omit it if nothing is needed
- Output `<file>` blocks for any TypeScript or JSON changes; omit if no code changes are warranted
- Never invent features absent from the diff
- Never modify protected files
- Write TypeScript consistent with the existing extension style (imports, class structure, vscode API usage)

## File Structure

```
.github/workflows/vscode-update.yml   ← new workflow (wendy-agent repo)
```

No changes to the VSCode extension repo itself — all changes arrive via PRs.
