package mcp

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wendylabsinc/wendy/go/internal/cli/grpcclient"
	"github.com/wendylabsinc/wendy/go/internal/shared/config"
)

func runProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "wendy.json"), []byte(`{"appId":"test"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestRunWithoutConnectionOrDeviceIsNotConnected(t *testing.T) {
	s := New(&config.Config{DefaultDevice: "must-not-be-used"}, nil)
	s.runCommandFn = func(context.Context, []string, commandTarget, int) (string, bool, error) {
		t.Fatal("run must not start a child without a target")
		return "", false, nil
	}
	for _, args := range []map[string]any{
		{"project_path": runProject(t)},
		{"project_path": runProject(t), "device": ""},
	} {
		r, err := s.handleRun(context.Background(), callToolReq("run", args))
		if err != nil || !r.IsError {
			t.Fatalf("args %v: want tool error, got %v %v", args, r, err)
		}
		out := structuredMap(t, r)
		if out["error_code"] != string(errCodeNotConnected) || !strings.Contains(out["message"].(string), "device_connect") {
			t.Fatalf("args %v: got %v", args, out)
		}
	}
}

func TestRunTargetArgumentErrorsStayInvalidArgument(t *testing.T) {
	project := runProject(t)
	disconnected := New(&config.Config{}, nil)
	// A non-replayable live connection (the on-device agent socket).
	unreplayable := New(&config.Config{}, nil)
	unreplayable.SetConn(&grpcclient.AgentConnection{Host: "unix:/run/wendy/agent.sock"})
	for _, tc := range []struct {
		s    *mcpServer
		args map[string]any
	}{
		{disconnected, map[string]any{"project_path": project, "cloud_grpc": "other:443"}},
		{disconnected, map[string]any{"project_path": project, "device": "vm:a", "device_name": "robot"}},
		{disconnected, map[string]any{"project_path": project, "device": "  "}},
		{unreplayable, map[string]any{"project_path": project}},
	} {
		r, err := tc.s.handleRun(context.Background(), callToolReq("run", tc.args))
		if err != nil || !r.IsError || structuredMap(t, r)["error_code"] != string(errCodeInvalidArgument) {
			t.Fatalf("args %v: got %v %v", tc.args, r, err)
		}
	}
}

func TestRunFailureCodeFromFinalDiagnostic(t *testing.T) {
	for _, tc := range []struct {
		output string
		want   errorCode
	}{
		{"building...\n✗ not logged in; run 'wendy auth login' first\n", errCodeAuthRequired},
		{"✗ Unauthorized. Run 'wendy auth login' with an account that can access this provisioned wendy-agent.", errCodeAuthRequired},
		{"✗ no login for cloud cloud.wendy.dev:443 organization 2; log in to that organization or choose another default", errCodeAuthRequired},
		{"✗ multiple auth sessions exist; pass --cloud-grpc or run 'wendy auth use' to choose a default", errCodeMultipleSessions},
		{"#5 ERROR: process \"/bin/sh -c make\" did not complete successfully: exit code: 2", errCodeInternal},
		{"", errCodeInternal},
		// Only the final diagnostic counts: an early build-log mention is not an auth failure.
		{"echo 'wendy auth login'\n" + strings.Repeat("step output\n", 200) + "✗ build failed", errCodeInternal},
	} {
		if got := runFailureCode(tc.output); got != tc.want {
			t.Errorf("runFailureCode(%.60q) = %s, want %s", tc.output, got, tc.want)
		}
	}
}

func TestRunAuthFailureReportsAuthRequired(t *testing.T) {
	s := New(&config.Config{}, nil)
	s.runCommandFn = func(context.Context, []string, commandTarget, int) (string, bool, error) {
		return "✗ not logged in; run 'wendy auth login' first", false, errors.New("exit status 1")
	}
	r, err := s.handleRun(context.Background(), callToolReq("run", map[string]any{"project_path": runProject(t), "device": "robot.local:50051"}))
	if err != nil || !r.IsError {
		t.Fatalf("want tool error: %v %v", r, err)
	}
	out := structuredMap(t, r)
	if out["error_code"] != string(errCodeAuthRequired) || out["status"] != "failed" || !strings.Contains(out["output"].(string), "wendy auth login") {
		t.Fatalf("got %v", out)
	}
}

// Review focus: an explicit device that differs from the session deploys
// there, and the result says to connect to it before verifying; otherwise
// container_list would inspect the old device.
func TestRunExplicitDeviceOverridesSessionAndSaysToConnect(t *testing.T) {
	s := New(&config.Config{}, nil)
	s.commandTarget = commandTarget{Device: "robot-a.local:50051", Transport: "direct"}
	var args []string
	s.runCommandFn = func(_ context.Context, a []string, _ commandTarget, _ int) (string, bool, error) {
		args = a
		return "ok", false, nil
	}
	r, err := s.handleRun(context.Background(), callToolReq("run", map[string]any{"project_path": runProject(t), "device": "robot-b.local:50051"}))
	if err != nil || r.IsError {
		t.Fatalf("run: %v %v", r, err)
	}
	if !strings.Contains(strings.Join(args, " "), "--device robot-b.local:50051") {
		t.Fatalf("explicit device not used: %v", args)
	}
	out := structuredMap(t, r)
	if target, ok := out["target"].(commandTarget); !ok || target.Device != "robot-b.local:50051" {
		t.Fatalf("result target = %v", out["target"])
	}
	if !strings.Contains(out["suggested_next_step"].(string), "Connect to the returned target") {
		t.Fatalf("suggested_next_step = %v", out["suggested_next_step"])
	}
}

// Review focus: a relative project_path resolves against the MCP server's
// working directory and reaches the CLI as an absolute --prefix.
func TestRunResolvesRelativeProjectPath(t *testing.T) {
	project := runProject(t)
	t.Chdir(project)
	want, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	s := New(&config.Config{}, nil)
	var args []string
	s.runCommandFn = func(_ context.Context, a []string, _ commandTarget, _ int) (string, bool, error) {
		args = a
		return "ok", false, nil
	}
	if r, err := s.handleRun(context.Background(), callToolReq("run", map[string]any{"project_path": ".", "device": "vm:test"})); err != nil || r.IsError {
		t.Fatalf("run: %v %v", r, err)
	}
	if !strings.Contains(strings.Join(args, " "), "--prefix "+want+" ") {
		t.Fatalf("relative project_path not made absolute: %v", args)
	}
	t.Chdir(t.TempDir())
	r, _ := s.handleRun(context.Background(), callToolReq("run", map[string]any{"project_path": ".", "device": "vm:test"}))
	if !r.IsError || !strings.Contains(structuredMap(t, r)["message"].(string), "wendy.json") {
		t.Fatalf("directory without wendy.json must be rejected: %v", r)
	}
}

// Review focus: a build that outlives timeout_seconds reports TIMEOUT, keeps
// the partial log, and warns that the container may already exist.
func TestRunTimeoutKeepsTailAndWarnsContainerMayExist(t *testing.T) {
	s := New(&config.Config{}, nil)
	s.runCommandFn = func(ctx context.Context, _ []string, _ commandTarget, _ int) (string, bool, error) {
		<-ctx.Done()
		return "#7 exporting layers", false, ctx.Err()
	}
	r, err := s.handleRun(context.Background(), callToolReq("run", map[string]any{"project_path": runProject(t), "device": "vm:test", "timeout_seconds": 1}))
	if err != nil || !r.IsError {
		t.Fatalf("want tool error: %v %v", r, err)
	}
	out := structuredMap(t, r)
	if out["error_code"] != string(errCodeTimeout) || out["output"] != "#7 exporting layers" || !strings.Contains(out["message"].(string), "may already have created") {
		t.Fatalf("got %v", out)
	}
}

// Review focus: an MCP server running inside a device container (admin
// entitlement, WENDY_AGENT_SOCKET) must not spawn a host-style deploy.
func TestRunRefusesInsideDeviceContainer(t *testing.T) {
	t.Setenv("WENDY_AGENT_SOCKET", "/run/wendy/agent.sock")
	s := New(&config.Config{}, nil)
	s.runCommandFn = func(context.Context, []string, commandTarget, int) (string, bool, error) {
		t.Fatal("run must not spawn a child inside a device container")
		return "", false, nil
	}
	r, err := s.handleRun(context.Background(), callToolReq("run", map[string]any{"project_path": runProject(t), "device": "vm:test"}))
	if err != nil || !r.IsError || structuredMap(t, r)["error_code"] != string(errCodeUnsupported) {
		t.Fatalf("got %v %v", r, err)
	}
}
