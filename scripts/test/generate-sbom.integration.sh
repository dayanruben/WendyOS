#!/usr/bin/env bash
# Integration test: requires real syft + go on PATH. Skips if syft is absent.
set -euo pipefail
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$HERE/../.." && pwd)"
SCRIPT="$HERE/../generate-sbom.sh"
command -v syft >/dev/null 2>&1 || { echo "SKIP: syft not installed"; exit 0; }
TMP="$(mktemp -d)"; trap 'rm -rf "$TMP"' EXIT

# Build a real wendy binary to catalog. Mirror CI: macOS build-go-macos job
# uses CGO_ENABLED=1 (ble_darwin.go / bluetooth_darwin.go need cgo), Linux
# uses CGO_ENABLED=0.
if [[ "$(uname -s)" == "Darwin" ]]; then CGO="1"; else CGO="0"; fi
( cd "$ROOT/go" && CGO_ENABLED="$CGO" go build -o "$TMP/wendy" ./cmd/wendy )

bash "$SCRIPT" binary "$TMP/wendy" "$TMP/wendy.spdx.json"
jq -e '.spdxVersion and (.packages | length > 0)' "$TMP/wendy.spdx.json" >/dev/null \
  || { echo "FAIL: binary SBOM missing packages"; exit 1; }

bash "$SCRIPT" source "$TMP/src.spdx.json" --repo-root "$ROOT"
jq -e '.spdxVersion' "$TMP/src.spdx.json" >/dev/null || { echo "FAIL: source SBOM invalid"; exit 1; }
# Excludes honored: no Examples/ file paths leak in.
if jq -e '[.. | .fileName? // empty] | map(select(startswith("Examples/"))) | length > 0' \
     "$TMP/src.spdx.json" >/dev/null 2>&1; then
  echo "FAIL: Examples/ not excluded"; exit 1
fi
echo "ok - integration"
