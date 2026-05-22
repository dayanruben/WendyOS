# MCP Container Entitlement Design

**Date:** 2026-05-01  
**Status:** Approved

## Overview

Standardize a way for Wendy app developers to expose an MCP server from their container. The wendy-agent picks it up automatically, and `wendy mcp serve` merges the container's tools into the AI assistant's MCP session alongside the built-in device tools.

## App Declaration

A new entitlement type `"mcp"` is added to `wendy.json`. It declares that the container runs an HTTP-based MCP server on the specified port.

```json
{
  "appId": "my-sensor-app",
  "entitlements": [
    { "type": "mcp", "port": 3000 }
  ]
}
```

**Constraints:**
- `port` is required, must be 1–65535.
- At most one `mcp` entitlement per app.
- The `mcp` entitlement does not imply host networking. The container's MCP port is internal to the device; the agent connects to it from within the device.
- The entitlement uses a dedicated `Port int` field on the `Entitlement` struct, separate from `Ports []PortMapping` which belongs to the `network` entitlement.

**`appconfig` changes:**
- Add `EntitlementMCP = "mcp"` to `ValidEntitlementTypes`.
- Add `Port int` field to the `Entitlement` struct.
- Add `"mcp"` to `allowedKeys` with keys `{"type", "port"}`.
- Validate: if type is `mcp`, `port` must be 1–65535; more than one `mcp` entitlement is a hard validation error.

## Proto Changes

### `AppContainer` — new field

```proto
message AppContainer {
  string app_name = 1;
  string app_version = 2;
  AppRunningState running_state = 3;
  uint32 failure_count = 4;
  uint32 mcp_port = 5;  // 0 = no MCP server declared
}
```

The agent populates `mcp_port` from the `mcp` entitlement in the container's `wendy.json` when building `ListContainers` responses. `mcp_port = 0` means no MCP server; the CLI ignores those containers.

### New RPC on `WendyContainerService`

```proto
rpc StreamMCP(stream MCPChunk) returns (stream MCPChunk);

message MCPChunk {
  bytes data = 1;
}
```

A bidirectional streaming RPC. The CLI passes `app_name` as gRPC request metadata (`"app-name"` key). The agent connects to the container's MCP port on `localhost:{mcp_port}` and pipes bytes in both directions between the gRPC stream and the container's HTTP connection. The agent is a **transparent byte-pipe** — it does not parse MCP JSON-RPC.

## Agent Implementation

### `ListContainers` — populate `mcp_port`

When building each `AppContainer` response, the agent reads the container's stored `wendy.json` and checks for a `{"type": "mcp", "port": N}` entitlement. If found, sets `mcp_port = N`. This is read-only — no container restart required.

### `StreamMCP` handler

1. Read `app_name` from incoming gRPC metadata.
2. Look up the container's `mcp_port` (error `NotFound` if no MCP entitlement).
3. Check container is running (error `FailedPrecondition` if not).
4. Dial `localhost:{mcp_port}` via TCP.
5. Pipe bytes bidirectionally until either side closes.

**Error responses:**
- Container not running → `FailedPrecondition`
- No `mcp` entitlement declared → `NotFound`
- Dial to `localhost:{mcp_port}` fails → `Unavailable`

## CLI Implementation

### `startMCPProxy` (`internal/cli/mcp/proxy.go`)

Mirrors the existing `startRegistryProxyWithDialer` pattern in `commands/docker.go`. Starts a local TCP listener on an ephemeral port. For each incoming TCP connection, opens a new `StreamMCP` gRPC call (with `app_name` in metadata) and pipes bytes bidirectionally. Returns the listener address (`localhost:N`) and a `close()` function.

Each HTTP connection from the mcp-go client gets its own gRPC stream. This handles the two-connection pattern of streamable-HTTP MCP (one POST + one GET/SSE) cleanly.

### Startup scan in `wendy mcp serve`

After the agent connection is established, before `srv.Start()`. If no connection is established at startup (user will connect later via `device_connect`), the scan is skipped silently — no MCP container tools are registered until a new `wendy mcp serve` is started with a device.

1. Call `ListContainers` and collect containers where `mcp_port > 0` and `running_state == RUNNING`.
2. For each container:
   a. Call `startMCPProxy(conn, appName)` → `localhost:N` + cleanup.
   b. Create `client.NewStreamableHttpClient("http://localhost:N")`.
   c. Call `Initialize` + `ListTools` on the client.
   d. For each discovered tool, register a proxy handler on `srv` under `{appname}__{toolname}`.
3. Deferred cleanups run when `srv.Start()` returns.

### Proxy tool handler

```go
func makeProxyHandler(c *client.Client, toolName string) server.ToolHandlerFunc {
    return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
        return c.CallTool(ctx, mcp.CallToolRequest{
            Params: mcp.CallToolParams{
                Name:      toolName,
                Arguments: req.Params.Arguments,
            },
        })
    }
}
```

Tool descriptions from the container are forwarded verbatim at registration time (from `ListTools`).

### Error handling

- If a container's `StreamMCP` returns `Unavailable` at startup, retry with exponential backoff for up to 10 seconds, then skip with a `stderr` warning. The session starts regardless.
- If mcp-go `Initialize` fails, skip that container with a `stderr` warning.
- Proxy handlers that fail at call time return an MCP tool error result, not a session-level error — one broken app does not affect the others.

## Data Flow

```
AI Assistant
    │  MCP JSON-RPC (stdio)
    ▼
wendy mcp serve (CLI)
    │  gRPC StreamMCP (bidirectional bytes)
    ▼
wendy-agent (on device)
    │  HTTP (localhost:{mcp_port})
    ▼
Container's MCP server (port 3000)
```

## Out of Scope

- Multiple MCP entitlements per app (one per app is sufficient).
- Authentication between the agent and the container's MCP server (trust within the device is acceptable).
- Dynamic re-scanning after `mcp serve` starts (reconnect on tool call failure is a future concern).
- Non-HTTP MCP transports (stdio, SSE-only) inside the container.
