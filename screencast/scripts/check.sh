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

for script in "$SCRIPT_DIR"/*.sh; do
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

shopt -s nullglob
python_files=("$SCRIPT_DIR"/*.py)
if [[ "${#python_files[@]}" -gt 0 ]]; then
  python3 - "${python_files[@]}" <<'PY'
import sys
from pathlib import Path
for name in sys.argv[1:]:
    path = Path(name)
    compile(path.read_text(encoding="utf-8"), str(path), "exec")
PY
fi

if command -v node >/dev/null 2>&1; then
  for script in "$SCRIPT_DIR"/*.mjs; do
    node --check "$script" >/dev/null
  done
else
  echo "warning: node not found; skipped JavaScript syntax checks" >&2
fi

python3 - "$PROJECT_DIR" <<'PY'
import json
import sys
from pathlib import Path
root = Path(sys.argv[1])
for rel in ["package.json", "timeline.schema.json"]:
    json.loads((root / rel).read_text(encoding="utf-8"))
if (root / "timeline.json").exists():
    json.loads((root / "timeline.json").read_text(encoding="utf-8"))
for rel in ["deck/slides.md", "deck/style.css", "deck/public/videos/.gitkeep", "deck/public/images/.gitkeep"]:
    path = root / rel
    if not path.exists():
        raise SystemExit(f"missing required source: {rel}")
if not (root / "scenes").exists() and not (root / "tapes").exists() and not (root / "timeline.json").exists():
    raise SystemExit("missing scene, tape, or timeline source")

def check_timeline(path: Path, base: Path) -> None:
    timeline = json.loads(path.read_text(encoding="utf-8"))
    deck = base / timeline["deck"]
    if not deck.exists():
        raise SystemExit(f"timeline deck does not exist: {path}: {timeline['deck']}")
    for step in timeline["steps"]:
        if "id" not in step or "target" not in step or "minSeconds" not in step:
            raise SystemExit(f"invalid timeline step in {path}: {step!r}")

if (root / "timeline.json").exists():
    check_timeline(root / "timeline.json", root)
template_timeline = root / "template" / "timeline.json"
if template_timeline.exists():
    check_timeline(template_timeline, template_timeline.parent)
PY

if command -v git >/dev/null 2>&1 && git -C "$PROJECT_DIR" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  tracked_generated="$({
    git -C "$PROJECT_DIR" ls-files -- deck/public/videos deck/public/images voiceover/mp3 output build 2>/dev/null || true
  } | grep -Ev '(^|/)\.gitkeep$' || true)"
  if [[ -n "$tracked_generated" ]]; then
    echo "error: generated media/build outputs are tracked:" >&2
    echo "$tracked_generated" >&2
    exit 1
  fi
fi

echo "screencast checks passed"
