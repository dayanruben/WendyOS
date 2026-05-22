# VSCode Extension PR Bot Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a GitHub Actions workflow to wendy-agent that opens a proposal PR to `wendylabsinc/wendy-vscode` whenever a CLI command file is merged, containing both Claude-generated TypeScript changes and a human-readable description.

**Architecture:** A single `vscode-update.yml` workflow modelled on `docs-update.yml`. On merged PRs touching `go/internal/cli/commands/**`, it fetches the diff, checks out `wendylabsinc/wendy-vscode`, calls Claude with the diff + relevance-ranked extension source files, parses `<description>` and `<file>` output blocks, writes file changes into the checkout, then opens a proposal PR gated on an env flag set by the Python script.

**Tech Stack:** GitHub Actions, Python 3.12, `anthropic==0.97.0`, `peter-evans/create-pull-request@v8`

---

### Task 1: Scaffold the workflow with trigger and diff fetch

**Files:**
- Create: `.github/workflows/vscode-update.yml`

- [ ] **Step 1: Create the workflow file**

```yaml
name: VSCode Extension Update

on:
  pull_request:
    types: [closed]
    branches: [main]
    paths:
      - 'go/internal/cli/commands/**'

jobs:
  vscode-update:
    name: Propose update to wendylabsinc/wendy-vscode
    if: github.event.pull_request.merged == true && !contains(github.event.pull_request.labels.*.name, 'ai-suggestion')
    runs-on: ubuntu-latest
    permissions:
      contents: read
      pull-requests: read
    steps:
      - name: Get PR diff
        env:
          GH_TOKEN: ${{ github.token }}
        run: |
          gh pr diff ${{ github.event.pull_request.number }} \
            --repo ${{ github.repository }} > pr.diff
```

- [ ] **Step 2: Install actionlint and validate**

```bash
brew install actionlint
actionlint .github/workflows/vscode-update.yml
```

Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add .github/workflows/vscode-update.yml
git commit -m "ci: scaffold vscode-update workflow with trigger and diff fetch"
```

---

### Task 2: Add wendy-vscode checkout and Python setup

**Files:**
- Modify: `.github/workflows/vscode-update.yml`

- [ ] **Step 1: Append three steps after `Get PR diff`**

```yaml
      - name: Check out wendy-vscode
        uses: actions/checkout@v6
        with:
          repository: wendylabsinc/wendy-vscode
          token: ${{ secrets.WENDY_TEMPLATE_SYNC_TOKEN }}
          path: vscode

      - name: Set up Python
        uses: actions/setup-python@v6
        with:
          python-version: "3.12"

      - name: Install Anthropic SDK
        run: pip install --quiet anthropic==0.97.0
```

- [ ] **Step 2: Validate**

```bash
actionlint .github/workflows/vscode-update.yml
```

Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add .github/workflows/vscode-update.yml
git commit -m "ci: add wendy-vscode checkout and Python setup"
```

---

### Task 3: Add the Python analysis script step

**Files:**
- Modify: `.github/workflows/vscode-update.yml`

- [ ] **Step 1: Append the analysis step after `Install Anthropic SDK`**

The Python script: collects and ranks VSCode extension source files by keyword relevance to the diff, calls Claude, parses `<description>` and `<file>` output, writes changed files, and sets `HAS_CHANGES` in `$GITHUB_ENV`.

```yaml
      - name: Analyse diff and generate VSCode proposal
        env:
          ANTHROPIC_API_KEY: ${{ secrets.ANTHROPIC_API_KEY }}
          PR_TITLE: ${{ github.event.pull_request.title }}
          PR_BODY: ${{ github.event.pull_request.body }}
          PR_NUMBER: ${{ github.event.pull_request.number }}
          REPO_NAME: ${{ github.event.repository.name }}
        run: |
          python3 << 'PYEOF'
          import anthropic
          import os
          import re
          import pathlib
          import sys

          client = anthropic.Anthropic()

          with open('pr.diff') as f:
              diff = f.read()

          if not diff.strip():
              print('Empty diff — skipping')
              sys.exit(0)

          vscode_root = pathlib.Path('vscode').resolve()
          repo_name = os.environ['REPO_NAME']
          pr_title = os.environ['PR_TITLE']
          pr_body = os.environ.get('PR_BODY', '')
          pr_number = os.environ['PR_NUMBER']

          PROTECTED_FILES = {'package-lock.json'}
          PROTECTED_DIRS = {'dist', 'node_modules', '.git'}
          ALLOWED_EXTENSIONS = {'.ts', '.json', '.md'}

          def _keywords(text):
              return set(re.findall(r'[a-zA-Z][a-zA-Z0-9_]{3,}', text.lower()))

          pr_keywords = _keywords(pr_title) | _keywords(diff[:8000])

          def _relevance(doc):
              path_hits = len(_keywords(doc['path']) & pr_keywords) * 3
              body_hits = len(_keywords(doc['content'][:1500]) & pr_keywords)
              return path_hits + body_hits

          FILE_BUDGET = 50000
          candidates = []
          for f in sorted(vscode_root.rglob('*')):
              if not f.is_file():
                  continue
              rel = f.relative_to(vscode_root)
              if any(part in PROTECTED_DIRS or part.startswith('.') for part in rel.parts):
                  continue
              if f.suffix.lower() not in ALLOWED_EXTENSIONS:
                  continue
              if f.name in PROTECTED_FILES:
                  continue
              try:
                  content = f.read_text(errors='replace')
              except OSError:
                  continue
              if not content.strip():
                  continue
              candidates.append({'path': str(rel), 'content': content})

          candidates.sort(key=_relevance, reverse=True)

          selected = []
          total = 0
          for doc in candidates:
              if total + len(doc['content']) > FILE_BUDGET:
                  print(f'File budget reached at {total} chars')
                  break
              selected.append(doc)
              total += len(doc['content'])

          MAX_TREE_DEPTH = 3
          tree_lines = []
          for item in sorted(vscode_root.rglob('*')):
              rel = item.relative_to(vscode_root)
              if any(part.startswith('.') or part in PROTECTED_DIRS for part in rel.parts):
                  continue
              if len(rel.parts) > MAX_TREE_DEPTH:
                  continue
              indent = '  ' * (len(rel.parts) - 1)
              tree_lines.append(f'{indent}{rel.name}{"/" if item.is_dir() else ""}')
          file_tree = '\n'.join(tree_lines)

          files_context = '\n\n'.join(
              f'### {d["path"]}\n```\n{d["content"]}\n```' for d in selected
          )

          message = client.messages.create(
              model='claude-sonnet-4-6',
              max_tokens=16000,
              system=[
                  {
                      'type': 'text',
                      'text': (
                          'You are a VSCode extension maintainer for WendyOS.\n'
                          'Given a CLI change (PR diff) and the current VSCode extension '
                          'source, determine whether the extension needs to be updated to '
                          'expose, support, or reflect the CLI change.\n\n'
                          'Rules:\n'
                          '- Only propose changes directly caused by the CLI diff: new '
                          'commands, removed commands, changed flags, new output formats, '
                          'or changed behaviour that the extension wraps or surfaces.\n'
                          '- If the extension does not need updating, output nothing at all.\n'
                          '- If it does need updating, output a <description> block '
                          'explaining what changed in the CLI and what the extension should '
                          'do differently, followed by <file> blocks with full replacement '
                          'content for any files that need changing.\n'
                          '- Write TypeScript consistent with the existing extension style '
                          '(ESM imports, class-based providers, vscode API patterns).\n'
                          '- Never output: package-lock.json, dist/, node_modules/, or any '
                          'path outside the repo root.\n'
                          '- Only output .ts, .json, or .md files.\n'
                          '- Do not invent features absent from the diff.\n\n'
                          'Output format (use both if changes are needed, neither if not):\n'
                          '<description>\n'
                          'Human-readable explanation of what CLI change occurred and what '
                          'the extension should change and why.\n'
                          '</description>\n\n'
                          '<file path="src/example.ts">full file content here</file>'
                      ),
                      'cache_control': {'type': 'ephemeral'},
                  }
              ],
              messages=[{
                  'role': 'user',
                  'content': (
                      f'Source repo: {repo_name}\n'
                      f'PR #{pr_number}: {pr_title}\n\n'
                      f'{pr_body}\n\n'
                      f'## CLI Diff\n```diff\n{diff[:30000]}\n```\n\n'
                      f'## VSCode Extension File Tree\n```\n{file_tree}\n```\n\n'
                      f'## Extension Source (most relevant first)\n{files_context[:50000]}'
                  ),
              }],
          )

          response_text = message.content[0].text

          desc_match = re.search(r'<description>(.*?)</description>', response_text, re.DOTALL)
          description = desc_match.group(1).strip() if desc_match else ''

          if not description and '<file' not in response_text:
              print('No VSCode extension changes needed')
              sys.exit(0)

          updated = 0
          for match in re.finditer(
              r'<file path="([^"]+)">(.*?)</file>',
              response_text,
              re.DOTALL,
          ):
              rel_path = match.group(1)
              content = match.group(2).strip()

              fpath = (vscode_root / rel_path).resolve()
              try:
                  fpath.relative_to(vscode_root)
              except ValueError:
                  print(f'Skipping unsafe path: {rel_path}')
                  continue
              if fpath.suffix.lower() not in ALLOWED_EXTENSIONS:
                  print(f'Skipping disallowed extension: {rel_path}')
                  continue
              if fpath.name in PROTECTED_FILES:
                  print(f'Skipping protected file: {rel_path}')
                  continue
              rel = fpath.relative_to(vscode_root)
              if any(part in PROTECTED_DIRS for part in rel.parts):
                  print(f'Skipping protected directory: {rel_path}')
                  continue

              is_new = not fpath.exists()
              fpath.parent.mkdir(parents=True, exist_ok=True)
              fpath.write_text(content + '\n')
              print(f'{"Created" if is_new else "Updated"}: {rel_path}')
              updated += 1

          if not description and updated == 0:
              print('No valid file changes written')
              sys.exit(0)

          body_text = description or (
              f'Claude generated VSCode extension changes based on '
              f'CLI changes in PR #{pr_number}: {pr_title}.\n\n'
              f'Please review the file changes.'
          )
          pathlib.Path('pr_description.md').write_text(body_text + '\n')

          github_env = os.environ.get('GITHUB_ENV', '')
          if github_env:
              with open(github_env, 'a') as genv:
                  genv.write('HAS_CHANGES=true\n')

          print(f'Done: {updated} file(s) updated')
          PYEOF
```

- [ ] **Step 2: Extract the Python and check syntax**

```bash
python3 -c "
import re, pathlib
workflow = pathlib.Path('.github/workflows/vscode-update.yml').read_text()
match = re.search(r\"python3 << 'PYEOF'\n(.*?)\n          PYEOF\", workflow, re.DOTALL)
script = re.sub(r'(?m)^          ', '', match.group(1))
pathlib.Path('/tmp/vscode_update_test.py').write_text(script)
print('Extracted', len(script), 'chars')
"
python3 -m py_compile /tmp/vscode_update_test.py && echo "Syntax OK"
```

Expected: `Syntax OK`

- [ ] **Step 3: Run the script locally against a fake diff**

```bash
git clone git@github.com:wendylabsinc/wendy-vscode.git vscode

cat > pr.diff << 'EOF'
diff --git a/go/internal/cli/commands/run.go b/go/internal/cli/commands/run.go
index abc1234..def5678 100644
--- a/go/internal/cli/commands/run.go
+++ b/go/internal/cli/commands/run.go
@@ -42,6 +42,7 @@ func runCmd() *cobra.Command {
+       cmd.Flags().BoolVar(&opts.detach, "detach", false, "run in background and return immediately")
EOF

export PR_TITLE="feat: add --detach flag to wendy run"
export PR_BODY="Allows running apps in the background without blocking the terminal."
export PR_NUMBER="999"
export REPO_NAME="wendy-agent"
export GITHUB_ENV="/tmp/test_github_env"
> /tmp/test_github_env

python3 /tmp/vscode_update_test.py
echo "Exit code: $?"
echo "GITHUB_ENV contents:"; cat /tmp/test_github_env
ls pr_description.md 2>/dev/null && echo "pr_description.md written" || echo "no pr_description.md (no changes needed)"
```

Expected: script exits 0; if Claude finds changes needed, `HAS_CHANGES=true` appears in `/tmp/test_github_env` and `pr_description.md` is written.

- [ ] **Step 4: Clean up local test artifacts**

```bash
rm -rf vscode pr.diff pr_description.md /tmp/vscode_update_test.py /tmp/test_github_env
```

- [ ] **Step 5: Validate YAML**

```bash
actionlint .github/workflows/vscode-update.yml
```

Expected: no errors.

- [ ] **Step 6: Commit**

```bash
git add .github/workflows/vscode-update.yml
git commit -m "ci: add Claude analysis step to vscode-update workflow"
```

---

### Task 4: Add the PR creation step

**Files:**
- Modify: `.github/workflows/vscode-update.yml`

- [ ] **Step 1: Append the PR creation step after `Analyse diff`**

```yaml
      - name: Open PR to wendy-vscode
        if: env.HAS_CHANGES == 'true'
        uses: peter-evans/create-pull-request@v8
        with:
          token: ${{ secrets.WENDY_TEMPLATE_SYNC_TOKEN }}
          path: vscode
          branch: vscode-update/${{ github.event.repository.name }}-${{ github.event.pull_request.number }}
          title: "feat(vscode): proposal for ${{ github.event.repository.name }}#${{ github.event.pull_request.number }}"
          body-path: pr_description.md
          commit-message: "feat(vscode): proposal from ${{ github.repository }}#${{ github.event.pull_request.number }}"
          base: main
          labels: ai-suggestion
```

- [ ] **Step 2: Validate the complete workflow**

```bash
actionlint .github/workflows/vscode-update.yml
```

Expected: no errors.

- [ ] **Step 3: Verify the final structure of the workflow**

```bash
grep -n "name:" .github/workflows/vscode-update.yml
```

Expected output (six named items):
```
1:name: VSCode Extension Update
11:    name: Propose update to wendylabsinc/wendy-vscode
18:      - name: Get PR diff
24:      - name: Check out wendy-vscode
31:      - name: Set up Python
36:      - name: Install Anthropic SDK
39:      - name: Analyse diff and generate VSCode proposal
<line>:      - name: Open PR to wendy-vscode
```

- [ ] **Step 4: Commit**

```bash
git add .github/workflows/vscode-update.yml
git commit -m "ci: add PR creation step — vscode-update workflow complete"
```

---

### Task 5: Final checklist

- [ ] **Step 1: Review the complete workflow against the spec**

Check each spec requirement:

| Requirement | Where |
|---|---|
| Trigger on `go/internal/cli/commands/**` | `paths:` block |
| Skip `ai-suggestion`-labelled PRs | `if:` condition on job |
| Fetch PR diff via `gh pr diff` | `Get PR diff` step |
| Checkout `wendylabsinc/wendy-vscode` with `WENDY_TEMPLATE_SYNC_TOKEN` | `Check out wendy-vscode` step |
| Relevance-rank files, cap at 50k chars | `FILE_BUDGET = 50000` + sort |
| Prompt caching on system prompt | `cache_control: {'type': 'ephemeral'}` |
| Model `claude-sonnet-4-6` | `model='claude-sonnet-4-6'` |
| Parse `<description>` for PR body | `desc_match = re.search(...)` |
| Parse `<file>` blocks, validate paths | `for match in re.finditer(...)` |
| Protected file/dir blocklist | `PROTECTED_FILES`, `PROTECTED_DIRS` |
| Extension allowlist (`.ts`, `.json`, `.md`) | `ALLOWED_EXTENSIONS` |
| No PR if nothing to do | `sys.exit(0)` before setting `HAS_CHANGES` |
| PR gated on `HAS_CHANGES` env var | `if: env.HAS_CHANGES == 'true'` |
| Branch `vscode-update/<repo>-<pr-number>` | `branch:` param |
| Label `ai-suggestion` | `labels:` param |
| PR body from `pr_description.md` | `body-path: pr_description.md` |

- [ ] **Step 2: Run actionlint one final time**

```bash
actionlint .github/workflows/vscode-update.yml
```

Expected: no errors.
