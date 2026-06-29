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

exec sleep infinity
