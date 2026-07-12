package mcp

import (
	"testing"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
)

func TestAnnotations_ReadOnlyAndDestructive(t *testing.T) {
	ro := mcpgo.NewTool("t_ro", readOnly()...)
	if ro.Annotations.ReadOnlyHint == nil || !*ro.Annotations.ReadOnlyHint {
		t.Error("readOnly() should set ReadOnlyHint=true")
	}
	de := mcpgo.NewTool("t_de", destructive()...)
	if de.Annotations.DestructiveHint == nil || !*de.Annotations.DestructiveHint {
		t.Error("destructive() should set DestructiveHint=true")
	}
	if de.Annotations.ReadOnlyHint == nil || *de.Annotations.ReadOnlyHint {
		t.Error("destructive() should set ReadOnlyHint=false")
	}
}
