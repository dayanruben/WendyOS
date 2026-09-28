# Wendy Agent — AI Assistant Guide

Wendy is a CLI and agent platform for developing and deploying applications on
WendyOS edge devices (Raspberry Pi, NVIDIA Jetson, x86 SBCs, and more).

## Setup

Install the CLI:

```sh
curl -fsSL https://install.wendy.dev/cli.sh | bash
```

Configure the MCP server for your AI coding tool:

```sh
wendy mcp setup
```

Supports: Claude Code, Claude Desktop, Cursor, Windsurf, Codex.

## Quick Start (with MCP)

1. Call `wendy_status` to see current connection state and a suggested next step.
2. Call `device_list` (optionally `scan: true`) to find available devices.
   - For USB-C tethered devices on Linux, run `sudo wendy device usb-setup` first.
3. Call `device_connect` or `cloud_connect` to connect.
4. Use container, WiFi, hardware, telemetry, and OS tools.

To build and deploy a local project to any device (direct or cloud):

```sh
wendy run --device <name>
```

Use the `run` MCP tool to deploy from within an AI session.
It uses an explicit `device` selector or the current connection's target and
transport; legacy `device_name` selects cloud deployment. Detached success does
not verify readiness. Check container state, logs and actual app/ROS output.

## Connection Model

Most MCP tools require an active device connection. The `run` tool deploys to
the connected target or an explicit `device`; with neither it returns
`NOT_CONNECTED` rather than picking a device.

## Authentication

Log in to Wendy Cloud:

```sh
wendy auth login
```

Discover cloud-enrolled devices:

```sh
wendy cloud discover
```
