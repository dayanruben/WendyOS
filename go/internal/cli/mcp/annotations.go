package mcp

import mcpgo "github.com/mark3labs/mcp-go/mcp"

// readOnly marks a tool that does not modify device or host state.
func readOnly() []mcpgo.ToolOption {
	return []mcpgo.ToolOption{mcpgo.WithReadOnlyHintAnnotation(true)}
}

// destructive marks a tool that may perform irreversible or disruptive updates.
func destructive() []mcpgo.ToolOption {
	return []mcpgo.ToolOption{
		mcpgo.WithDestructiveHintAnnotation(true),
		mcpgo.WithReadOnlyHintAnnotation(false),
	}
}

// idempotent marks a tool where repeated identical calls have no extra effect.
func idempotent() []mcpgo.ToolOption {
	return []mcpgo.ToolOption{mcpgo.WithIdempotentHintAnnotation(true)}
}

// openWorld marks a tool that interacts with external entities (network,
// nearby radios, cloud broker) whose state the server does not own.
func openWorld() []mcpgo.ToolOption {
	return []mcpgo.ToolOption{mcpgo.WithOpenWorldHintAnnotation(true)}
}
