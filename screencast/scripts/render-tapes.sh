#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCREENCAST_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
HOOK_DIR="$SCREENCAST_DIR/hooks"
RUN_PREFLIGHT=1
RUN_SETUP=0
RUN_TEARDOWN=0
YES=0
DRY_RUN=0

usage() {
  cat <<'EOF'
usage: render-tapes.sh [options] [tape ...]

Validate and render VHS tapes. If no tape paths are provided, renders every
checked-in tape under screencast/tapes/ and screencast/scenes/*/.

Options:
  --with-hooks      Run setup and teardown hooks around rendering.
  --setup           Run hooks/setup.sh before rendering.
  --teardown        Run hooks/teardown.sh after rendering.
  --skip-preflight  Do not run hooks/preflight.sh.
  --yes, -y         Skip confirmation prompts.
  --dry-run         Print what would run without executing reset or vhs.
EOF
}

tapes=()
while [[ $# -gt 0 ]]; do
  case "$1" in
    --with-hooks)
      RUN_SETUP=1
      RUN_TEARDOWN=1
      shift
      ;;
    --setup)
      RUN_SETUP=1
      shift
      ;;
    --teardown)
      RUN_TEARDOWN=1
      shift
      ;;
    --skip-preflight)
      RUN_PREFLIGHT=0
      shift
      ;;
    --yes|-y)
      YES=1
      shift
      ;;
    --dry-run)
      DRY_RUN=1
      shift
      ;;
    --help|-h)
      usage
      exit 0
      ;;
    --)
      shift
      tapes+=("$@")
      break
      ;;
    -*)
      echo "error: unknown argument: $1" >&2
      usage >&2
      exit 2
      ;;
    *)
      tapes+=("$1")
      shift
      ;;
  esac
done

require() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "error: required command not found: $1" >&2
    exit 2
  }
}

run_hook() {
  local name="$1"
  local hook="$HOOK_DIR/$name.sh"
  if [[ ! -f "$hook" ]]; then
    return
  fi
  if [[ ! -x "$hook" ]]; then
    echo "error: hook exists but is not executable: hooks/$name.sh" >&2
    exit 1
  fi

  echo "==> hook: $name"
  SCREENCAST_ROOT="$SCREENCAST_DIR" \
  SCREENCAST_YES="$YES" \
  SCREENCAST_DRY_RUN="$DRY_RUN" \
    "$hook"
}

require vhs

if [[ "${#tapes[@]}" -eq 0 ]]; then
  while IFS= read -r tape; do
    tapes+=("$tape")
  done < <({
    find "$SCREENCAST_DIR/tapes" -maxdepth 1 -type f -name '*.tape' -print 2>/dev/null || true
    find "$SCREENCAST_DIR/scenes" -mindepth 2 -maxdepth 2 -type f -name '*.tape' -print 2>/dev/null || true
  } | sort)
fi

if [[ "${#tapes[@]}" -eq 0 ]]; then
  echo "error: no tapes found" >&2
  exit 1
fi

cat <<EOF
This will render ${#tapes[@]} VHS tape(s).

Working directory: $SCREENCAST_DIR
Preflight hook:    $([[ "$RUN_PREFLIGHT" -eq 1 ]] && echo yes || echo no)
Setup hook:        $([[ "$RUN_SETUP" -eq 1 ]] && echo yes || echo no)
Teardown hook:     $([[ "$RUN_TEARDOWN" -eq 1 ]] && echo yes || echo no)
Dry run:           $([[ "$DRY_RUN" -eq 1 ]] && echo yes || echo no)

Tapes:
EOF
printf '  - %s\n' "${tapes[@]}"

if [[ "$YES" -eq 0 && "$DRY_RUN" -eq 0 ]]; then
  cat <<'EOF'

Rendering tapes executes the commands inside them. Confirm only when the local
recording machine and the target device are in the expected state.
EOF
  read -r -p "Continue? [y/N] " answer
  case "$answer" in
    y|Y|yes|YES) ;;
    *) echo "aborted"; exit 1 ;;
  esac
fi

if [[ "$RUN_PREFLIGHT" -eq 1 ]]; then
  run_hook preflight
fi
if [[ "$RUN_SETUP" -eq 1 ]]; then
  run_hook setup
fi

for tape in "${tapes[@]}"; do
  case "$tape" in
    /*) tape_path="$tape" ;;
    *) tape_path="$SCREENCAST_DIR/$tape" ;;
  esac

  if [[ ! -f "$tape_path" ]]; then
    echo "error: tape not found: $tape" >&2
    exit 1
  fi

  tape_cwd="$SCREENCAST_DIR"
  if [[ "$tape_path" == "$SCREENCAST_DIR/template/"* ]]; then
    tape_cwd="$SCREENCAST_DIR/template"
  fi

  echo "==> validating ${tape_path#$SCREENCAST_DIR/}"
  vhs validate "$tape_path"

  output="$(awk '$1 == "Output" { print $2; exit }' "$tape_path")"
  if [[ -n "$output" ]]; then
    echo "==> output $output"
    if [[ "$DRY_RUN" -eq 0 ]]; then
      rm -f "$tape_cwd/$output"
    else
      echo "dry-run: rm -f '$tape_cwd/$output'"
    fi
  fi

  echo "==> rendering ${tape_path#$SCREENCAST_DIR/}"
  if [[ "$DRY_RUN" -eq 1 ]]; then
    echo "dry-run: (cd '$tape_cwd' && vhs '$tape_path')"
  else
    (cd "$tape_cwd" && vhs "$tape_path")
  fi
done

if [[ "$RUN_TEARDOWN" -eq 1 ]]; then
  run_hook teardown
fi

cat <<'EOF'

Done. Review generated clips in their scene folders or under deck/public/videos/
before rendering the final Slidev timeline.
EOF
