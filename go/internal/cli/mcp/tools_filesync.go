package mcp

import (
	"context"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func (s *mcpServer) registerFileSyncTools(srv *server.MCPServer) {
	syncOpts := []mcpgo.ToolOption{
		mcpgo.WithDescription("Sync files to a container app on the connected device (requires binary file data; not available via MCP)"),
	}
	syncOpts = append(syncOpts, mutating()...)
	syncOpts = append(syncOpts, localOnly()...)
	syncOpts = append(syncOpts, idempotent()...)
	srv.AddTool(mcpgo.NewTool("filesync_sync", syncOpts...), s.handleFileSyncSync)
}

func (s *mcpServer) handleFileSyncSync(_ context.Context, _ mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	return errResult(errCodeUnsupported, "filesync over MCP is not supported; run the equivalent from the wendy CLI"), nil
}
