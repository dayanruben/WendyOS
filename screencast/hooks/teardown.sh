#!/usr/bin/env bash
set -euo pipefail

TEMP_CONTAINER_DEMO="/tmp/wendy-mac-beta-container-demo"
DRY_RUN="${SCREENCAST_DRY_RUN:-0}"

if [[ -e "$TEMP_CONTAINER_DEMO" || "$DRY_RUN" -eq 1 ]]; then
  echo "==> removing local temporary container demo directory"
  if [[ "$DRY_RUN" -eq 1 ]]; then
    echo "dry-run: rm -rf '$TEMP_CONTAINER_DEMO'"
  elif ! rm -rf "$TEMP_CONTAINER_DEMO" 2>/dev/null; then
    echo "==> normal remove failed; requesting sudo for $TEMP_CONTAINER_DEMO"
    sudo rm -rf "$TEMP_CONTAINER_DEMO"
  fi
fi
