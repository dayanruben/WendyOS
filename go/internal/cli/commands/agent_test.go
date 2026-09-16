package commands

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wendylabsinc/wendy/go/internal/cli/agentservice"
)

func TestChatListsProfilesWithoutModelOrTerminal(t *testing.T) {
	original := jsonOutput
	defer func() { jsonOutput = original }()
	jsonOutput = false
	cmd := newChatCmd()
	out := new(bytes.Buffer)
	cmd.SetOut(out)
	cmd.SetArgs([]string{"--list-profiles", "-C", "/does-not-exist"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"general", "developer", "simulation", "debugger", "fleet", "device-reasoning", "device-sensors", "device-control"} {
		if !strings.Contains(out.String(), name) {
			t.Fatal("missing profile", name)
		}
	}
	cmd = newChatCmd()
	cmd.SetArgs([]string{"--profile", "invented"})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "unknown chat profile") {
		t.Fatal(err)
	}
}
func TestAgentExampleAndCommands(t *testing.T) {
	root := NewRootCmd()
	cmd, _, err := root.Find([]string{"agent", "serve"})
	if err != nil || cmd.Name() != "serve" {
		t.Fatal(cmd, err)
	}
	example := newAgentExampleCmd()
	out := new(bytes.Buffer)
	example.SetOut(out)
	if err := example.Execute(); err != nil {
		t.Fatal(err)
	}
	var c agentservice.Config
	if err := json.Unmarshal(out.Bytes(), &c); err != nil {
		t.Fatal(err)
	}
	if c.Profile != "device-reasoning" || len(c.Triggers) != 1 || len(c.AllowTools) != 0 {
		t.Fatal(c)
	}
	file := filepath.Join(t.TempDir(), "agent.json")
	if err := os.WriteFile(file, out.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := agentservice.LoadConfig(file); err != nil {
		t.Fatal(err)
	}
}
func TestAgentRefusesUnauthenticatedOrCleartextPublicListener(t *testing.T) {
	c := `{"name":"test","profile":"debugger","workspace":".","model":{"provider":"local","model":"test"}}`
	file := filepath.Join(t.TempDir(), "agent.json")
	if err := os.WriteFile(file, []byte(c), 0600); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		args []string
		want string
	}{{[]string{"--listen", "0.0.0.0:8787"}, "require TLS"}, {nil, "WENDY_AGENT_TOKEN"}} {
		t.Setenv("WENDY_AGENT_TOKEN", "")
		cmd := newAgentServeCmd()
		cmd.SetArgs(append([]string{"--config", file}, test.args...))
		err := cmd.Execute()
		if err == nil || !strings.Contains(err.Error(), test.want) {
			t.Fatal(err)
		}
	}
}
