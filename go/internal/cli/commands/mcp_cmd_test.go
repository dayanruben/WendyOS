package commands

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	toml "github.com/BurntSushi/toml"
)

func TestCursorConfigPath_ReturnsDirBasedPath(t *testing.T) {
	home, _ := os.UserHomeDir()
	want := filepath.Join(home, ".cursor", "mcp.json")
	if got := cursorConfigPath(); got != "" && got != want {
		t.Fatalf("cursorConfigPath() = %q, want %q or empty", got, want)
	}
}

func TestWindsurfConfigPath_ReturnsDirBasedPath(t *testing.T) {
	home, _ := os.UserHomeDir()
	want := filepath.Join(home, ".codeium", "windsurf", "mcp_config.json")
	if got := windsurfConfigPath(); got != "" && got != want {
		t.Fatalf("windsurfConfigPath() = %q, want %q or empty", got, want)
	}
}

func TestAddMCPToTOMLConfig_CreatesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := addMCPToTOMLConfig(path, "mcp_servers", "wendy", "wendy", []string{"mcp", "serve"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading file: %v", err)
	}
	var out map[string]any
	if _, err := toml.Decode(string(data), &out); err != nil {
		t.Fatalf("parsing TOML: %v", err)
	}
	servers, ok := out["mcp_servers"].(map[string]any)
	if !ok {
		t.Fatalf("expected mcp_servers map, got: %T", out["mcp_servers"])
	}
	wendyEntry, ok := servers["wendy"].(map[string]any)
	if !ok {
		t.Fatalf("expected wendy entry, got: %T %v", servers["wendy"], servers["wendy"])
	}
	if wendyEntry["command"] != "wendy" {
		t.Errorf("expected command=wendy, got: %v", wendyEntry["command"])
	}
}

func TestAddMCPToTOMLConfig_PreservesExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	existing := "[mcp_servers.other]\ncommand = \"other\"\n"
	if err := os.WriteFile(path, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := addMCPToTOMLConfig(path, "mcp_servers", "wendy", "wendy", []string{"mcp", "serve"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading file: %v", err)
	}
	var out map[string]any
	if _, err := toml.Decode(string(data), &out); err != nil {
		t.Fatalf("parsing TOML: %v", err)
	}
	servers, ok := out["mcp_servers"].(map[string]any)
	if !ok {
		t.Fatalf("expected mcp_servers map, got %T", out["mcp_servers"])
	}
	if _, ok := servers["other"]; !ok {
		t.Error("expected 'other' entry to be preserved")
	}
	if _, ok := servers["wendy"]; !ok {
		t.Error("expected 'wendy' entry to be present")
	}
}

// Setup owns only type, command and args of the wendy entry; anything the user
// added to it (env, timeouts) must survive setup and the upgrade refresh.
func TestAddMCPToJSONConfig_KeepsUserKeysInWendyEntry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mcp.json")
	in := `{"numStartups": 3, "mcpServers": {
  "github": {"command": "npx"},
  "wendy": {"type": "stdio", "command": "/old/wendy", "args": ["mcp", "serve", "--old"], "env": {"WENDY_DEVICE": "pi.local"}}
}}`
	if err := os.WriteFile(path, []byte(in), 0o644); err != nil {
		t.Fatal(err)
	}
	entry := map[string]any{"type": "stdio", "command": "/new/wendy", "args": []string{"mcp", "serve"}}
	if err := addMCPToJSONConfig(path, "mcpServers", "wendy", entry); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got, want map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(`{"numStartups": 3, "mcpServers": {
  "github": {"command": "npx"},
  "wendy": {"type": "stdio", "command": "/new/wendy", "args": ["mcp", "serve"], "env": {"WENDY_DEVICE": "pi.local"}}
}}`), &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got:\n%s", data)
	}
}

func TestCodexConfigPath_ReturnsDirBasedPath(t *testing.T) {
	home, _ := os.UserHomeDir()
	want := filepath.Join(home, ".codex", "config.toml")
	if got := codexConfigPath(); got != "" && got != want {
		t.Fatalf("codexConfigPath() = %q, want %q or empty", got, want)
	}
}

func TestShouldRefreshMCPSetup(t *testing.T) {
	tests := []struct {
		name        string
		lastVersion string
		current     string
		want        bool
	}{
		{name: "never set up", lastVersion: "", current: "0.11.0", want: false},
		{name: "dev build never refreshes", lastVersion: "0.10.0", current: "dev", want: false},
		{name: "dev branch build never refreshes", lastVersion: "0.10.0", current: "2026.06.30-1-dev", want: false},
		{name: "same version", lastVersion: "0.11.0", current: "0.11.0", want: false},
		{name: "upgraded", lastVersion: "0.10.0", current: "0.11.0", want: true},
		{name: "downgraded", lastVersion: "0.11.0", current: "0.10.0", want: true},
		{name: "set up on dev then real build", lastVersion: "dev", current: "0.11.0", want: true},
		{name: "never set up on dev build", lastVersion: "", current: "dev", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldRefreshMCPSetup(tt.lastVersion, tt.current); got != tt.want {
				t.Errorf("shouldRefreshMCPSetup(%q, %q) = %v, want %v", tt.lastVersion, tt.current, got, tt.want)
			}
		})
	}
}

func TestMCPCmd_HelpText(t *testing.T) {
	cmd := newMCPCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"--help"})
	_ = cmd.Execute()
	out := buf.String()
	if !strings.Contains(out, "serve") {
		t.Fatalf("expected help to mention 'serve', got: %s", out)
	}
}

func TestMCPRestartNotice(t *testing.T) {
	notice := mcpRestartNotice([]mcpSetupResult{
		{tool: "Claude Code", path: "/h/.claude.json"},
		{tool: "Cursor", path: "/h/.cursor/mcp.json", err: errors.New("parsing")},
		{tool: "Codex", path: "/h/.codex/config.toml"},
	})
	for _, want := range []string{"Restart any of these", "Claude Code: start a new session", "`/mcp`", "Codex: start a new Codex session"} {
		if !strings.Contains(notice, want) {
			t.Errorf("notice missing %q:\n%s", want, notice)
		}
	}
	if strings.Contains(notice, "Cursor") {
		t.Errorf("a tool whose setup failed must not be listed:\n%s", notice)
	}
	if got := mcpRestartNotice(nil); got != "" {
		t.Errorf("no configured tools should print nothing, got %q", got)
	}
	if got := mcpRestartNotice([]mcpSetupResult{{tool: "Codex skills", path: "/h/.codex/wendy-skills.md"}}); got != "" {
		t.Errorf("skill installs are not MCP clients, got %q", got)
	}
}

// End to end: `wendy mcp setup` in an isolated HOME keeps a hand-written Codex
// config intact apart from the wendy table, and tells the user what to restart.
func TestMCPSetupCmd_PreservesCodexConfigAndPrintsRestartNotice(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("PATH", t.TempDir()) // no claude/cursor/windsurf/codex binaries
	t.Setenv("WENDY_CONFIG_DIR", filepath.Join(home, ".wendy"))
	if err := os.MkdirAll(filepath.Join(home, ".codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	original := "# my codex settings\nmodel = \"o3\"  # pinned\n\n[mcp_servers.github]\ncommand = \"npx\"\n"
	codexPath := filepath.Join(home, ".codex", "config.toml")
	if err := os.WriteFile(codexPath, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}

	cmd := newMCPSetupCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(nil)
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(codexPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(got), original+"\n[mcp_servers.wendy]\n") {
		t.Fatalf("codex config not preserved:\n%s", got)
	}
	for _, want := range []string{"✓ Codex: configured at " + codexPath, "Codex: start a new Codex session"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output missing %q:\n%s", want, out.String())
		}
	}
}
