#!/usr/bin/env bash
set -euo pipefail

ROOT="${SCREENCAST_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)}"
REPO_ROOT="$(cd "$ROOT/.." && pwd)"
TARGET="mac-mini.local:50051"
DRY_RUN="${SCREENCAST_DRY_RUN:-0}"

require() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "error: required command not found: $1" >&2
    exit 2
  }
}

require brew
require jq
require make
require vhs
require wendy

for path in \
  "$REPO_ROOT/Examples/HelloMac" \
  "$REPO_ROOT/Examples/HelloMLX" \
  "$REPO_ROOT/Examples/HelloXcode" \
  "$REPO_ROOT/swift/Makefile"; do
  if [[ ! -e "$path" ]]; then
    echo "error: expected path missing: $path" >&2
    exit 1
  fi
done

if [[ "$DRY_RUN" -eq 1 ]]; then
  echo "dry-run: wendy --device $TARGET device info --json >/dev/null"
else
  echo "==> checking target reachability: $TARGET"
  wendy --device "$TARGET" device info --json >/dev/null
fi
