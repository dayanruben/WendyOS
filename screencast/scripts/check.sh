#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

require_tool() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "error: required tool not found: $1" >&2
    exit 2
  fi
}

require_tool bash
require_tool python3

if [[ -n "${SCREENCAST_ALLOW_UNSAFE_URLS:-}" ]]; then
  echo "error: SCREENCAST_ALLOW_UNSAFE_URLS is obsolete and must not be set" >&2
  exit 1
fi

for script in "$SCRIPT_DIR"/*.sh; do
  [[ -e "$script" ]] || continue
  bash -n "$script"
done
for hook_dir in "$PROJECT_DIR/hooks" "$PROJECT_DIR/template/hooks"; do
  if [[ -d "$hook_dir" ]]; then
    for hook in "$hook_dir"/*.sh; do
      [[ -e "$hook" ]] || continue
      bash -n "$hook"
    done
  fi
done

if command -v node >/dev/null 2>&1; then
  while IFS= read -r script; do
    node --check "$script" >/dev/null
  done < <(find "$SCRIPT_DIR" -type f \( -name '*.mjs' -o -name 'render-slide' -o -name 'render-tape' -o -name 'render-voice' -o -name 'stitch' \) -print)
else
  echo "warning: node not found; skipped JavaScript syntax checks" >&2
fi

if command -v npm >/dev/null 2>&1 && [[ -f "$PROJECT_DIR/package-lock.json" ]]; then
  (cd "$PROJECT_DIR" && npm audit --audit-level=high >/dev/null)
  if [[ -d "$PROJECT_DIR/node_modules" ]]; then
    (cd "$PROJECT_DIR" && npm ls devframe >/dev/null)
  fi
else
  echo "warning: npm not found; skipped npm audit and dependency checks" >&2
fi

python3 - "$PROJECT_DIR" <<'PY'
import json
import sys
from pathlib import Path
root = Path(sys.argv[1])
json.loads((root / "package.json").read_text(encoding="utf-8"))
if not (root / "scenes").exists() and not (root / "template" / "scenes").exists():
    raise SystemExit("missing scenes/ or template/scenes/")
PY

if command -v git >/dev/null 2>&1 && git -C "$PROJECT_DIR" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  tracked_generated="$({
    git -C "$PROJECT_DIR" ls-files -- output build 2>/dev/null || true
    git -C "$PROJECT_DIR" ls-files -- 'scenes/*/*.mp4' 'scenes/*/*.webm' 'scenes/*/*.gif' 'scenes/*/*.mp3' 2>/dev/null || true
    git -C "$PROJECT_DIR" ls-files -- 'template/scenes/*/*.mp4' 'template/scenes/*/*.webm' 'template/scenes/*/*.gif' 'template/scenes/*/*.mp3' 2>/dev/null || true
  } | grep -Ev '(^|/)\.gitkeep$' || true)"
  if [[ -n "$tracked_generated" ]]; then
    echo "error: generated media/build outputs are tracked:" >&2
    echo "$tracked_generated" >&2
    exit 1
  fi

  secret_hits="$(git -C "$PROJECT_DIR" grep -n -E 'sk-(proj-)?[A-Za-z0-9_-]{20,}' -- . 2>/dev/null || true)"
  if [[ -n "$secret_hits" ]]; then
    echo "error: possible OpenAI API key found in tracked screencast files:" >&2
    echo "$secret_hits" >&2
    exit 1
  fi
fi

echo "screencast checks passed"
