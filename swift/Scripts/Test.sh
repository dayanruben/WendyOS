#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SWIFT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
SCRATCH_PATH="${SCRATCH_PATH:-$SWIFT_DIR/Build/SwiftPM}"

mkdir -p "$SCRATCH_PATH"
cd "$SWIFT_DIR/WendyAgentCore"
swift test --scratch-path "$SCRATCH_PATH"
