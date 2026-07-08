package commands

import (
	"context"
	"fmt"
	"net"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/wendylabsinc/wendy/go/internal/cli/grpcclient"
	"github.com/wendylabsinc/wendy/go/internal/shared/appconfig"
	"github.com/wendylabsinc/wendy/go/proto/gen/agentpb"
	"google.golang.org/grpc"
)

// lifecycleFakeAgentClient is a minimal WendyAgentServiceClient for driving
// announceReachableURL from serviceHookRunner tests: GetAgentVersion returns a
// canned (network-interface-free) response so the call succeeds without a
// real agent connection.
type lifecycleFakeAgentClient struct {
	agentpb.WendyAgentServiceClient // embedded nil — satisfies the interface
}

func (f *lifecycleFakeAgentClient) GetAgentVersion(context.Context, *agentpb.GetAgentVersionRequest, ...grpc.CallOption) (*agentpb.GetAgentVersionResponse, error) {
	return &agentpb.GetAgentVersionResponse{}, nil
}

// lifecycleFakeContainerClient is a minimal WendyContainerServiceClient:
// ListContainers reports the configured container (or none, in which case
// warnReadiness's containerExitDetail lookup returns "" without needing a
// real agent), and listContainersCalls lets tests assert whether the warning
// path's exit-detail lookup ran at all — a proxy for "was warnReadiness
// invoked".
type lifecycleFakeContainerClient struct {
	agentpb.WendyContainerServiceClient // embedded nil — satisfies the interface

	listContainersCalls int
	container           *agentpb.AppContainer // nil → empty ListContainers response
}

func (f *lifecycleFakeContainerClient) ListContainers(context.Context, *agentpb.ListContainersRequest, ...grpc.CallOption) (grpc.ServerStreamingClient[agentpb.ListContainersResponse], error) {
	f.listContainersCalls++
	return &fakeListContainersStream{resp: &agentpb.ListContainersResponse{Container: f.container}}, nil
}

// newLifecycleTestConn builds an AgentConnection backed by the fakes above, so
// runOne can reach waitForReadiness (real net dialing against host), warnReadiness,
// and announceReachableURL without a live agent.
func newLifecycleTestConn(host string, containerFake *lifecycleFakeContainerClient) *grpcclient.AgentConnection {
	return &grpcclient.AgentConnection{
		Host:             host,
		AgentService:     &lifecycleFakeAgentClient{},
		ContainerService: containerFake,
	}
}

func swapBrowserOpen(t *testing.T) *[]string {
	t.Helper()
	original := browserOpen
	var calls []string
	browserOpen = func(url string) error {
		calls = append(calls, url)
		return nil
	}
	t.Cleanup(func() { browserOpen = original })
	return &calls
}

func TestServiceHookRunner_NoOpWithoutLifecycleConfig(t *testing.T) {
	containerFake := &lifecycleFakeContainerClient{}
	conn := newLifecycleTestConn("127.0.0.1", containerFake)
	r := &serviceHookRunner{conn: conn}

	t.Run("nil cfg", func(t *testing.T) {
		calls := swapBrowserOpen(t)
		r.runOne(context.Background(), context.Background(), nil)
		if len(*calls) != 0 {
			t.Errorf("browserOpen called for nil cfg: %v", *calls)
		}
		if containerFake.listContainersCalls != 0 {
			t.Errorf("ListContainers called for nil cfg (readiness warning path taken)")
		}
	})

	t.Run("nil Readiness and Hooks", func(t *testing.T) {
		calls := swapBrowserOpen(t)
		cfg := &appconfig.AppConfig{AppID: "app", ServiceName: "svc"}
		r.runOne(context.Background(), context.Background(), cfg)
		if len(*calls) != 0 {
			t.Errorf("browserOpen called for a non-declaring service: %v", *calls)
		}
		if containerFake.listContainersCalls != 0 {
			t.Errorf("ListContainers called for a non-declaring service (readiness warning path taken)")
		}
		if len(r.cmds) != 0 {
			t.Errorf("cmds tracked for a non-declaring service: %v", r.cmds)
		}
	})
}

// TestServiceHookRunner_FiresAfterReadiness verifies the happy path: readiness
// passes against a real listener, announceReachableURL runs without error, and
// the postStart openURL hook fires with both WENDY_HOSTNAME (the connection's
// host) and WENDY_SERVICE_NAME (this service's name) substituted.
func TestServiceHookRunner_FiresAfterReadiness(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start listener: %v", err)
	}
	defer ln.Close()
	port := testPort(t, ln)

	calls := swapBrowserOpen(t)
	containerFake := &lifecycleFakeContainerClient{}
	conn := newLifecycleTestConn("127.0.0.1", containerFake)
	r := &serviceHookRunner{conn: conn}

	cfg := &appconfig.AppConfig{
		AppID:       "app",
		ServiceName: "worker",
		Readiness: &appconfig.ReadinessConfig{
			TCPSocket:      &appconfig.TCPSocketProbe{Port: port},
			TimeoutSeconds: 5,
		},
		Hooks: &appconfig.HooksConfig{
			PostStart: &appconfig.HookCommand{
				OpenURL: "http://${WENDY_HOSTNAME}:9/${WENDY_SERVICE_NAME}",
			},
		},
	}

	r.runOne(context.Background(), context.Background(), cfg)

	if len(*calls) != 1 {
		t.Fatalf("browserOpen calls = %v, want exactly 1", *calls)
	}
	want := "http://127.0.0.1:9/worker"
	if (*calls)[0] != want {
		t.Errorf("openURL = %q, want %q", (*calls)[0], want)
	}
	if containerFake.listContainersCalls != 0 {
		t.Errorf("ListContainers called despite readiness succeeding (unexpected warning path)")
	}
	if len(r.cmds) != 0 {
		t.Errorf("cmds tracked for an openURL-only hook: %v", r.cmds)
	}
}

// TestServiceHookRunner_ReadinessTimeoutStillFiresHook mirrors the
// single-container contract: a readiness probe that times out (as opposed to
// being canceled) only warns — it does not suppress the postStart hook.
func TestServiceHookRunner_ReadinessTimeoutStillFiresHook(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to find free port: %v", err)
	}
	port := testPort(t, ln)
	ln.Close() // nothing listens on this port for the rest of the test

	calls := swapBrowserOpen(t)
	containerFake := &lifecycleFakeContainerClient{}
	conn := newLifecycleTestConn("127.0.0.1", containerFake)
	r := &serviceHookRunner{conn: conn}

	cfg := &appconfig.AppConfig{
		AppID:       "app",
		ServiceName: "worker",
		Readiness: &appconfig.ReadinessConfig{
			TCPSocket:      &appconfig.TCPSocketProbe{Port: port},
			TimeoutSeconds: 1,
		},
		Hooks: &appconfig.HooksConfig{
			PostStart: &appconfig.HookCommand{OpenURL: "http://localhost:9"},
		},
	}

	start := time.Now()
	r.runOne(context.Background(), context.Background(), cfg)
	elapsed := time.Since(start)

	if elapsed < 500*time.Millisecond {
		t.Errorf("runOne returned in %v, expected to wait out the ~1s readiness timeout", elapsed)
	}
	if len(*calls) != 1 {
		t.Fatalf("browserOpen calls = %v, want exactly 1 (hook must still fire)", *calls)
	}
	if containerFake.listContainersCalls == 0 {
		t.Errorf("ListContainers never called; expected warnReadiness's exit-detail lookup to run")
	}
}

// TestServiceHookRunner_ReadinessWarningIncludesGroupExitDetail locks in that
// the readiness-failure warning's container-exit-detail lookup matches on the
// GROUP appID: the agent's ListContainers groups per-service containers under
// the group app-ID label and reports AppContainer.AppName as the bare group
// appID (exit code/reason aggregate onto that group entry; per-service detail
// lives in a separate Services list). Passing "{AppID}_{ServiceName}" would
// never match, silently dropping the enrichment for every multi-service app.
func TestServiceHookRunner_ReadinessWarningIncludesGroupExitDetail(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to find free port: %v", err)
	}
	port := testPort(t, ln)
	ln.Close() // nothing listens; readiness will time out

	containerFake := &lifecycleFakeContainerClient{
		container: &agentpb.AppContainer{
			AppName:           "app", // GROUP appID, not "app_worker"
			RunningState:      agentpb.AppRunningState_STOPPED,
			TerminationReason: "crashed",
			ExitCode:          1,
		},
	}
	conn := newLifecycleTestConn("127.0.0.1", containerFake)
	r := &serviceHookRunner{conn: conn}

	cfg := &appconfig.AppConfig{
		AppID:       "app",
		ServiceName: "worker",
		Readiness: &appconfig.ReadinessConfig{
			TCPSocket:      &appconfig.TCPSocketProbe{Port: port},
			TimeoutSeconds: 1,
		},
	}

	out := captureStdout(t, func() {
		r.runOne(context.Background(), context.Background(), cfg)
	})

	if !strings.Contains(out, "Warning:") {
		t.Fatalf("readiness warning never printed; output:\n%s", out)
	}
	if !strings.Contains(out, "container crashed (exit 1)") {
		t.Errorf("readiness warning lacks the container-exit-detail suffix (group-appID lookup failed); output:\n%s", out)
	}
}

// TestServiceHookRunner_CancelDuringReadiness verifies that canceling ctx
// while waiting on readiness returns promptly and suppresses BOTH the
// readiness warning and the postStart hook — unlike a plain timeout.
func TestServiceHookRunner_CancelDuringReadiness(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to find free port: %v", err)
	}
	port := testPort(t, ln)
	ln.Close() // never becomes ready

	calls := swapBrowserOpen(t)
	containerFake := &lifecycleFakeContainerClient{}
	conn := newLifecycleTestConn("127.0.0.1", containerFake)
	r := &serviceHookRunner{conn: conn}

	cfg := &appconfig.AppConfig{
		AppID:       "app",
		ServiceName: "worker",
		Readiness: &appconfig.ReadinessConfig{
			TCPSocket:      &appconfig.TCPSocketProbe{Port: port},
			TimeoutSeconds: 30,
		},
		Hooks: &appconfig.HooksConfig{
			PostStart: &appconfig.HookCommand{OpenURL: "http://localhost:9"},
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(200 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	r.runOne(ctx, ctx, cfg)
	elapsed := time.Since(start)

	if elapsed > 3*time.Second {
		t.Errorf("runOne took %v, expected to return promptly after cancellation", elapsed)
	}
	if len(*calls) != 0 {
		t.Errorf("browserOpen called after cancellation: %v", *calls)
	}
	if containerFake.listContainersCalls != 0 {
		t.Errorf("ListContainers called after cancellation (readiness warning must be suppressed)")
	}
}

// TestServiceHookRunner_ReapWaitsCliHook verifies startAsync + reap's contract:
// the cli hook fires, and reap blocks until the (canceled) child is actually
// reaped rather than returning immediately and leaving a zombie.
func TestServiceHookRunner_ReapWaitsCliHook(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("host-side hook uses `touch`/`sleep`, unavailable on Windows")
	}

	sentinel := filepath.Join(t.TempDir(), "reap-cli-ran")
	containerFake := &lifecycleFakeContainerClient{}
	conn := newLifecycleTestConn("127.0.0.1", containerFake)
	r := &serviceHookRunner{conn: conn}

	cfg := &appconfig.AppConfig{
		AppID:       "app",
		ServiceName: "worker",
		Hooks: &appconfig.HooksConfig{
			PostStart: &appconfig.HookCommand{CLI: fmt.Sprintf("touch %q && sleep 30", sentinel)},
		},
	}

	runCtx, runCancel := context.WithCancel(context.Background())
	r.startAsync(runCtx, cfg)

	waitForFile(t, sentinel, 3*time.Second)
	r.mu.Lock()
	tracked := len(r.cmds)
	r.mu.Unlock()
	if tracked != 1 {
		t.Fatalf("cmds tracked = %d, want 1 after the cli hook started", tracked)
	}

	runCancel()

	done := make(chan struct{})
	go func() {
		r.reap()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("reap() did not return; cli hook child was not reaped (possible zombie)")
	}
}

func TestAppLevelLifecycleConfig(t *testing.T) {
	t.Run("nil top", func(t *testing.T) {
		if got := appLevelLifecycleConfig("app", nil); got != nil {
			t.Errorf("appLevelLifecycleConfig(nil top) = %+v, want nil", got)
		}
	})

	t.Run("neither Readiness nor Hooks", func(t *testing.T) {
		top := &appconfig.AppConfig{AppID: "app"}
		if got := appLevelLifecycleConfig("app", top); got != nil {
			t.Errorf("appLevelLifecycleConfig() = %+v, want nil", got)
		}
	})

	t.Run("populated", func(t *testing.T) {
		readiness := &appconfig.ReadinessConfig{TCPSocket: &appconfig.TCPSocketProbe{Port: 3001}}
		hooks := &appconfig.HooksConfig{PostStart: &appconfig.HookCommand{OpenURL: "http://${WENDY_HOSTNAME}:3001"}}
		top := &appconfig.AppConfig{AppID: "ignored-elsewhere", Readiness: readiness, Hooks: hooks}

		got := appLevelLifecycleConfig("group-app-id", top)
		if got == nil {
			t.Fatal("appLevelLifecycleConfig() = nil, want populated config")
		}
		if got.AppID != "group-app-id" {
			t.Errorf("AppID = %q, want %q", got.AppID, "group-app-id")
		}
		if got.ServiceName != "" {
			t.Errorf("ServiceName = %q, want empty", got.ServiceName)
		}
		if got.Readiness != readiness {
			t.Errorf("Readiness = %p, want the same pointer as top.Readiness (%p)", got.Readiness, readiness)
		}
		if got.Hooks != hooks {
			t.Errorf("Hooks = %p, want the same pointer as top.Hooks (%p)", got.Hooks, hooks)
		}
	})

	t.Run("only Readiness set", func(t *testing.T) {
		top := &appconfig.AppConfig{AppID: "app", Readiness: &appconfig.ReadinessConfig{TCPSocket: &appconfig.TCPSocketProbe{Port: 1}}}
		if got := appLevelLifecycleConfig("app", top); got == nil {
			t.Error("appLevelLifecycleConfig() = nil, want populated config when only Readiness is set")
		}
	})

	t.Run("only Hooks set", func(t *testing.T) {
		top := &appconfig.AppConfig{AppID: "app", Hooks: &appconfig.HooksConfig{PostStart: &appconfig.HookCommand{CLI: "echo hi"}}}
		if got := appLevelLifecycleConfig("app", top); got == nil {
			t.Error("appLevelLifecycleConfig() = nil, want populated config when only Hooks is set")
		}
	})
}
