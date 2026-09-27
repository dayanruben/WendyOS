package mcp

import (
	"context"
	"fmt"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/wendylabsinc/wendy/go/internal/shared/version"
)

func (s *mcpServer) registerStatusTools(srv *server.MCPServer) {
	statusOpts := []mcpgo.ToolOption{
		mcpgo.WithDescription("Return current MCP session connection state and a plain-English suggested next step. Call this first to orient yourself."),
	}
	statusOpts = append(statusOpts, readOnly()...)
	statusOpts = append(statusOpts, localOnly()...)
	srv.AddTool(mcpgo.NewTool("wendy_status", statusOpts...), s.handleWendyStatus)
}

func (s *mcpServer) handleWendyStatus(_ context.Context, _ mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	conn, connType, target := s.connectionSnapshot()

	if conn == nil {
		out := map[string]any{
			"connected":             false,
			"suggested_next_step":   "Call device_list (scan=true adds LAN discovery), then device_connect or cloud_connect. If this is a new board without WendyOS/Agent, use os_install_plan; an empty scan does not distinguish uninstalled hardware from a network problem.",
			"cli_version":           version.Version,
			"installation_planning": s.installation.Plan != nil,
			"proxy_diagnostics":     s.proxyDiagnostics(),
		}
		return okResult(out), nil
	}

	host := conn.Host
	if host == "" {
		host = "device"
	}
	out := map[string]any{
		"connected":             true,
		"cli_version":           version.Version,
		"installation_planning": s.installation.Plan != nil,
		"device":                host,
		"connection_type":       connType,
		"suggested_next_step":   fmt.Sprintf("connected to %s via %s — ready to use container, wifi, hardware, telemetry, and os tools", host, connType),
		"proxy_diagnostics":     s.proxyDiagnostics(),
	}
	if target.Device != "" {
		out["command_target"] = target
	}
	return okResult(out), nil
}
