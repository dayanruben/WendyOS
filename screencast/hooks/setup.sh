#!/usr/bin/env bash
set -euo pipefail

ROOT="${SCREENCAST_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)}"
REPO_ROOT="$(cd "$ROOT/.." && pwd)"
TARGET="mac-mini.local:50051"
TEMP_CONTAINER_DEMO="/tmp/wendy-mac-beta-container-demo"
YES="${SCREENCAST_YES:-0}"
DRY_RUN="${SCREENCAST_DRY_RUN:-0}"

run() {
  printf '==> %s\n' "$*"
  if [[ "$DRY_RUN" -eq 0 ]]; then
    "$@"
  fi
}

run_shell() {
  printf '==> %s\n' "$*"
  if [[ "$DRY_RUN" -eq 0 ]]; then
    bash -c "$*"
  fi
}

cat <<EOF
This setup hook resets state for the Wendy Agent for Mac Beta VHS recordings.

Target Mac mini: $TARGET
Repo:            $REPO_ROOT

It will:
  - list/stop/remove all Wendy-managed apps on $TARGET
  - quit local WendyAgentMac if running
  - reset local WendyAgentMac settings and TCC permissions
  - uninstall the local Wendy Agent cask
  - remove local $TEMP_CONTAINER_DEMO

It will not reset or uninstall WendyAgentMac on the Mac mini. The Mac mini agent
is assumed to already be running; the local cask is only for the install/setup
recording.
EOF

if [[ "$YES" -eq 0 && "$DRY_RUN" -eq 0 ]]; then
  read -r -p "Continue setup hook? [y/N] " answer
  case "$answer" in
    y|Y|yes|YES) ;;
    *) echo "aborted"; exit 1 ;;
  esac
fi

remove_managed_apps_on_target() {
  local json app
  json="$(mktemp)"

  echo "==> listing managed apps on target $TARGET"
  if [[ "$DRY_RUN" -eq 1 ]]; then
    echo "dry-run: wendy --json --device $TARGET device apps list"
    return
  fi

  if ! wendy --json --device "$TARGET" device apps list >"$json"; then
    echo "error: could not list apps on $TARGET" >&2
    echo "       Make sure the Mac mini agent is running and reachable." >&2
    rm -f "$json"
    exit 1
  fi

  mapfile -t apps < <(jq -r '.[]?.name // empty' "$json")
  rm -f "$json"

  if [[ "${#apps[@]}" -eq 0 ]]; then
    echo "==> no managed apps found on target"
    return
  fi

  for app in "${apps[@]}"; do
    [[ -n "$app" ]] || continue
    run wendy --device "$TARGET" device apps stop "$app" || true
    run wendy --device "$TARGET" device apps remove --force "$app"
  done

  echo "==> verifying target app list"
  wendy --json --device "$TARGET" device apps list | jq .
}

remove_managed_apps_on_target

run_shell 'pkill -x WendyAgentMac >/dev/null 2>&1 || true'
run_shell "cd '$REPO_ROOT/swift' && make agent-reset"
run_shell 'brew uninstall --cask --force wendy-agent || true'

if [[ -e "$TEMP_CONTAINER_DEMO" || "$DRY_RUN" -eq 1 ]]; then
  echo "==> removing local temporary container demo directory"
  if [[ "$DRY_RUN" -eq 1 ]]; then
    echo "dry-run: rm -rf '$TEMP_CONTAINER_DEMO'"
  elif ! rm -rf "$TEMP_CONTAINER_DEMO" 2>/dev/null; then
    echo "==> normal remove failed; requesting sudo for $TEMP_CONTAINER_DEMO"
    sudo rm -rf "$TEMP_CONTAINER_DEMO"
  fi
fi

cat <<'EOF'

Setup complete. When tape 01 installs and opens local WendyAgentMac, complete any
local macOS setup/permission prompts promptly. Later tapes target the Mac mini
at the hardcoded --device address.
EOF
