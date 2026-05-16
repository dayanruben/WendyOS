# Wendy Agent — AI Assistant Guide

Wendy is a CLI and agent platform for developing and deploying applications on
WendyOS edge devices (Raspberry Pi, NVIDIA Jetson, x86 SBCs, and more).

## Setup

Install the CLI:

```sh
curl -fsSL https://install.wendy.sh/cli.sh | bash
```

Configure the MCP server for your AI coding tool:

```sh
wendy mcp setup
```

Supports: Claude Code, Claude Desktop, Cursor, Windsurf, Codex.

## Quick Start (with MCP)

1. Call `wendy_status` — returns connection state and a suggested next step.
2. Call `device_list` (optionally `scan: true`) to find available devices.
3. Call `device_connect` or `cloud_connect` to connect.
4. Use container, WiFi, hardware, telemetry, and OS tools.

To build and deploy a local project to any device (direct or cloud):

```sh
wendy run --device <name>
```

Or via the `run` MCP tool with `project_path`.

## Connection Model

Most MCP tools require an active device connection. `wendy_status` always
tells you where you are and what to do next. `run` and `cloud_run` work
without a prior connection.

## Authentication

Log in to Wendy Cloud:

```sh
wendy auth login
```

Discover cloud-enrolled devices:

```sh
wendy cloud discover
```
