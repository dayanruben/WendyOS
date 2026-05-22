# LLM Discoverability & CLI Unification

**Date:** 2026-05-16
**Status:** Approved

## Problem

Two distinct gaps make Wendy hard to use with LLMs (and confusing for humans too):

1. **Discovery/setup layer** — LLMs and users don't know Wendy exists or how to invoke `wendy mcp setup`.
2. **In-session guidance layer** — Once the MCP server is running, the LLM lands cold: terse tool descriptions, implicit connection prerequisites, and no workflow hints.

Additionally, `wendy run` vs `wendy cloud run` is a persistent source of confusion — users and LLMs must understand the transport distinction before they can do anything useful.

## Goals

- A single `wendy run` command that routes direct or via cloud automatically
- An MCP server that orients any LLM from a cold start without prior knowledge of Wendy
- `wendy mcp setup` works for all major AI coding tools
- A top-level `AGENTS.md` for LLMs with repo access

## Non-Goals

- Dynamic tool registration based on connection state (MCP protocol expects a stable tool list)
- Exposing `--cloud-grpc` or `--broker-url` flags on `wendy run` (inferred from config)
- Changing any existing tool behavior beyond what is described here

---

## Section 1: CLI Unification

### Routing logic

`wendy run` gains a `resolveRunConnection()` helper that replaces the separate `wendy cloud run` path. The transport is an implementation detail inferred from device identity — no new flags are exposed.

**With `--device <name>`** (checked in this order):

| Priority | Device identity | Transport |
|---|---|---|
| 1 | Matches a local provider key (`docker`, `local`, etc.) | Local provider |
| 2 | Matches a cloud-enrolled device name in config | Cloud tunnel (broker URL + cloud gRPC inferred from config auth entry) |
| 3 | Otherwise (hostname, mDNS name, IP:port) | Direct gRPC |

**Without `--device`:**

Show a unified picker combining local providers, discovered devices, and cloud-enrolled devices. Each picker entry carries its type; the selected entry drives transport selection automatically.

### `wendy cloud run` deprecation

`wendy cloud run` is registered as a hidden Cobra alias pointing to the same `RunE` as `wendy run`. Its long-form help text reads: "Deprecated: use `wendy run` instead. Routing is now automatic."

All flags previously unique to `wendy cloud run` (`--cloud-grpc`, `--broker-url`) are removed from the surface; their values are inferred from config.

---

## Section 2: MCP `run` Tool

### Rename and generalize `cloud_run`

The `cloud_run` MCP tool is renamed to `run`. It shells out to `wendy run` (the now-unified command).

**Parameters:**

| Parameter | Type | Required | Description |
|---|---|---|---|
| `project_path` | string | yes | Local filesystem path to build and deploy |
| `device_name` | string | no | Cloud or local device name. If omitted, passes no `--device` flag to `wendy run`, which uses the config default or returns an error if none is set. |
| `build_type` | string | no | `docker`, `swift`, or `python`. Auto-detected if absent. |

**Backwards compatibility:**

`cloud_run` stays registered as a deprecated alias with description: "Deprecated — use `run` instead. Routing is now automatic."

### LLM benefit

One tool, one mental model. The LLM does not need to reason about whether a device is local or cloud-enrolled before deploying.

---

## Section 3: MCP Guide Resource + `wendy_status` Tool

### `wendy://guide` MCP resource

Registered as a static MCP resource at server startup. LLMs that call `resources/list` at session start will see it and can read it to orient before using any tools.

**Content:**

```
Wendy manages WendyOS edge devices. This MCP server exposes tools for device
management, container deployment, WiFi, Bluetooth, hardware telemetry, OS
updates, file sync, and cloud connectivity.

## Connection model

Most tools require an active device connection. Call wendy_status first to
see the current state and get a suggested next step.

## Common workflows

Local/direct device:
  1. device_list (optionally with scan:true for mDNS discovery)
  2. device_connect
  3. Use container, WiFi, hardware, telemetry tools

Cloud-enrolled device:
  1. cloud_discover
  2. cloud_connect
  3. Use container, WiFi, hardware, telemetry tools

Deploy a local project (either connection type):
  run — builds and deploys automatically, no prior connection needed
```

### `wendy_status` tool

Returns current connection state and a plain-English next-step suggestion.

**Response shape:**

```json
{
  "connected": true,
  "device": "mydevice.local:50051",
  "connection_type": "direct",
  "suggested_next_step": "connected to mydevice.local via direct — ready to use container, WiFi, hardware, and telemetry tools"
}
```

```json
{
  "connected": false,
  "suggested_next_step": "not connected — call device_list to see available devices, then device_connect; or cloud_discover + cloud_connect for cloud-enrolled devices"
}
```

`suggested_next_step` is the key field — it removes guesswork for an LLM that doesn't know where it is in the workflow.

---

## Section 4: `device_list` Scan + `wendy mcp setup` Expansion + AGENTS.md

### `device_list` scan parameter

Adds an optional `scan` boolean parameter. When `true`, runs a 3-second mDNS/local-network discovery pass (reusing existing discovery machinery, consistent with `wendy discover` defaults) and merges results with config-known devices. Default `false` so the common case stays fast.

Each result entry gains a `source` field:

| Value | Meaning |
|---|---|
| `"config"` | Known from `~/.wendy/config.json` |
| `"scan"` | Found via mDNS scan |
| `"cloud"` | Cloud-enrolled (from auth config) |

### `wendy mcp setup` expansion

Adds detection and config-writing for three additional AI coding tools:

| Tool | Config path | Format |
|---|---|---|
| Cursor | `~/.cursor/mcp.json` | JSON (`mcpServers` key, same shape as Claude Code) |
| Windsurf | `~/.codeium/windsurf/mcp_config.json` | JSON (`mcpServers` key) |
| Codex | `~/.codex/config.yaml` | YAML (`mcpServers` key) |

Detection follows the same pattern as Claude Code: check for the config path or the tool binary in PATH. A new `addMCPToYAMLConfig` writer handles the Codex YAML format.

### Top-level `AGENTS.md`

A short file at the repository root, visible to any LLM with repo access or RAG over docs.

**Contents:**
- What Wendy is (one paragraph)
- Install command
- MCP setup: `wendy mcp setup` — configures Claude Code, Claude Desktop, Cursor, Windsurf, Codex
- Quick workflow: connect to a device, deploy an app, check telemetry
- Pointer to `wendy_status` as the recommended starting point once MCP is running

---

## Implementation Scope

| Area | Files affected |
|---|---|
| CLI unification | `commands/run.go`, `commands/cloud_run.go`, `commands/cloud.go`, new `resolveRunConnection()` helper |
| MCP `run` tool | `mcp/tools_cloud.go` |
| MCP guide resource | `mcp/server.go` |
| `wendy_status` tool | `mcp/tools_device.go` (or new `mcp/tools_status.go`) |
| `device_list` scan | `mcp/tools_device.go` |
| `wendy mcp setup` expansion | `commands/mcp_setup.go` |
| AGENTS.md | `/AGENTS.md` (repo root) |
