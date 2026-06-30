#!/bin/sh
# claude-on-device first-run init: register the Wendy MCP server with Claude Code
# so Claude has device tools (info, containers, telemetry, run) over the local
# agent socket, then idle so `wendy device attach` can exec `claude` on demand.
#
# `wendy mcp setup` is idempotent — it re-applies the config on every start, so
# the MCP server is configured even on a fresh container (writable layer reset).
set -e

if [ -n "$WENDY_AGENT_SOCKET" ]; then
  wendy mcp setup || echo "warning: 'wendy mcp setup' failed (continuing without MCP)" >&2
else
  echo "warning: WENDY_AGENT_SOCKET unset — skipping MCP setup (admin entitlement missing?)" >&2
fi

# Start buildkitd (rootful; the container is root) so `wendy run` can build images
# on-device via buildctl. BUILDKIT_SNAPSHOTTER lets the operator force "native" if
# overlayfs-on-overlayfs is unavailable on the device kernel (default: auto).
mkdir -p /run/buildkit /var/lib/buildkit
buildkitd \
  ${BUILDKIT_SNAPSHOTTER:+--oci-worker-snapshotter="$BUILDKIT_SNAPSHOTTER"} \
  >/var/log/buildkitd.log 2>&1 &

# Wait briefly for the control socket so the first `wendy run` doesn't race it.
for _ in 1 2 3 4 5 6 7 8 9 10; do
  [ -S /run/buildkit/buildkitd.sock ] && break
  sleep 0.5
done
[ -S /run/buildkit/buildkitd.sock ] || echo "warning: buildkitd socket not up; on-device builds will fail until it is" >&2

exec sleep infinity
