package appconfig

import "testing"

// TestClaudeOnDeviceExampleValidates guards the real Examples/ClaudeOnDevice
// app config: it must parse, validate, and declare the admin entitlement plus
// the two persist mounts the app relies on (OAuth token + workspace).
func TestClaudeOnDeviceExampleValidates(t *testing.T) {
	cfg, err := LoadFromFile("../../../../Examples/ClaudeOnDevice/wendy.json")
	if err != nil {
		t.Fatalf("loading claude-on-device example: %v", err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("claude-on-device wendy.json invalid: %v", err)
	}

	var admin, claudeCfg, workspace bool
	for _, e := range cfg.Entitlements {
		switch {
		case e.Type == EntitlementAdmin:
			admin = true
		case e.Type == EntitlementPersist && e.Path == "/root/.claude":
			claudeCfg = true
		case e.Type == EntitlementPersist && e.Path == "/workspace":
			workspace = true
		}
	}
	if !admin {
		t.Error("missing admin entitlement")
	}
	if !claudeCfg {
		t.Error("missing persist mount for /root/.claude")
	}
	if !workspace {
		t.Error("missing persist mount for /workspace")
	}
}
