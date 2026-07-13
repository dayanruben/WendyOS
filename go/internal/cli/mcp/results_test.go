package mcp

import (
	"encoding/json"
	"testing"
)

func TestOkResult_HasStructuredAndJSONText(t *testing.T) {
	r := okResult(map[string]any{"version": "1.2.3"})
	if r.IsError {
		t.Fatal("expected success result")
	}
	if r.StructuredContent == nil {
		t.Fatal("expected structured content")
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(toolResultText(t, r)), &m); err != nil {
		t.Fatalf("text fallback is not valid JSON: %v", err)
	}
	if m["version"] != "1.2.3" {
		t.Errorf("version = %v", m["version"])
	}
}
