# Claude-on-device runs the Claude Code CLI inside an admin-entitled container
# so the device can operate and debug itself over the local agent socket.
#
# node:22-bookworm-slim is multi-arch; built for arm64 (the Jetson) via buildx.
FROM node:22-bookworm-slim

# git + ripgrep are used by Claude Code; ca-certificates for the Anthropic API.
RUN apt-get update \
  && apt-get install -y --no-install-recommends git ca-certificates ripgrep less \
  && rm -rf /var/lib/apt/lists/*

# Claude Code CLI.
RUN --mount=type=cache,target=/root/.npm npm install -g @anthropic-ai/claude-code

# The wendy CLI built from the claude-on-device branch (understands
# WENDY_AGENT_SOCKET). Stage the arm64 binary into the build context as
# `wendy-linux-arm64` before building — see README.md.
COPY wendy-linux-arm64 /usr/local/bin/wendy
RUN chmod +x /usr/local/bin/wendy

# First-run init: registers the Wendy MCP server with Claude Code on every start.
COPY init.sh /usr/local/bin/claude-on-device-init
RUN chmod +x /usr/local/bin/claude-on-device-init

# Runs as root (the default) on purpose: the admin entitlement bind-mounts the
# agent's root-owned 0660 control socket, so the process must be root to use it.
# /root (home: OAuth token, config, MCP setup) and /workspace are persisted via
# the `persist` entitlements in wendy.json.
WORKDIR /workspace

# init.sh runs `wendy mcp setup` then idles (sleep infinity) so the container
# stays up; `wendy device attach claude-on-device` execs `claude` with a PTY.
CMD ["/usr/local/bin/claude-on-device-init"]
