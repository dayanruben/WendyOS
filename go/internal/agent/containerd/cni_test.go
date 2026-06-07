package containerd

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBuildCNIConfig(t *testing.T) {
	cfg := buildBridgeCNIConfig("com.example.myapp", "10.89.42.0/24")
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(cfg), &m); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if m["type"] != "bridge" {
		t.Errorf("type = %q, want bridge", m["type"])
	}
	if m["bridge"] != "wendy-br-com.example.myapp" {
		t.Errorf("bridge = %q, want wendy-br-com.example.myapp", m["bridge"])
	}
	ipam, ok := m["ipam"].(map[string]interface{})
	if !ok {
		t.Fatal("ipam field missing or wrong type")
	}
	if ipam["type"] != "host-local" {
		t.Errorf("ipam.type = %q, want host-local", ipam["type"])
	}
	if ipam["subnet"] != "10.89.42.0/24" {
		t.Errorf("ipam.subnet = %q, want 10.89.42.0/24", ipam["subnet"])
	}
}

func TestAllocateSubnet(t *testing.T) {
	s1 := allocateSubnet("com.example.app1")
	s2 := allocateSubnet("com.example.app2")
	if s1 == s2 {
		t.Errorf("different appIDs should get different subnets, both got %q", s1)
	}
	if !strings.HasPrefix(s1, "10.89.") {
		t.Errorf("subnet %q should start with 10.89.", s1)
	}
	if !strings.HasPrefix(s2, "10.89.") {
		t.Errorf("subnet %q should start with 10.89.", s2)
	}
}
