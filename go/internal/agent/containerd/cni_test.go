package containerd

import (
	"encoding/json"
	"os"
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

func TestWriteHostsFile(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/hosts"
	entries := map[string]string{
		"db":  "10.89.1.2",
		"api": "10.89.1.3",
	}
	if err := writeHostsFile(path, entries); err != nil {
		t.Fatalf("writeHostsFile error: %v", err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading hosts file: %v", err)
	}
	s := string(content)
	if !strings.Contains(s, "10.89.1.2\tdb") {
		t.Errorf("hosts file missing db entry, got:\n%s", s)
	}
	if !strings.Contains(s, "10.89.1.3\tapi") {
		t.Errorf("hosts file missing api entry, got:\n%s", s)
	}
	if !strings.Contains(s, "127.0.0.1\tlocalhost") {
		t.Errorf("hosts file missing localhost entry, got:\n%s", s)
	}
}
