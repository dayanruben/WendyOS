package mcp

import (
	"encoding/json"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
)

// okResult returns a success result carrying v as structuredContent plus an
// indented-JSON text fallback for hosts that do not render structured content.
func okResult(v any) *mcpgo.CallToolResult {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return errResultf(errCodeInternal, "marshaling result: %s", err.Error())
	}
	return mcpgo.NewToolResultStructured(v, string(b))
}

// okText returns a plain-text success result (for simple confirmations).
func okText(msg string) *mcpgo.CallToolResult {
	return mcpgo.NewToolResultText(msg)
}
