package commands

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wendylabsinc/wendy/go/internal/cli/grpcclient"
	"github.com/wendylabsinc/wendy/go/internal/shared/appconfig"
	"github.com/wendylabsinc/wendy/go/proto/gen/agentpb"
)

// tunnelStarterCall records one startCloudAppTunnel call.
type tunnelStarterCall struct {
	remotePort int
	ctx        context.Context
}

// swapCloudAppTunnel replaces startCloudAppTunnel with start and records
// every call. A nil start fails the test on any call: the path under test
// must not open a cloud app tunnel.
func swapCloudAppTunnel(t *testing.T, start func(ctx context.Context, conn *grpcclient.AgentConnection, remotePort int) (*cloudAppTunnel, error)) *[]tunnelStarterCall {
	t.Helper()
	original := startCloudAppTunnel
	t.Cleanup(func() { startCloudAppTunnel = original })
	var mu sync.Mutex
	var calls []tunnelStarterCall
	startCloudAppTunnel = func(ctx context.Context, conn *grpcclient.AgentConnection, remotePort int) (*cloudAppTunnel, error) {
		mu.Lock()
		calls = append(calls, tunnelStarterCall{remotePort: remotePort, ctx: ctx})
		mu.Unlock()
		if start == nil {
			t.Errorf("opened a cloud app tunnel to port %d; this path must not", remotePort)
			return nil, errors.New("unexpected tunnel")
		}
		return start(ctx, conn, remotePort)
	}
	return &calls
}

// fixedTunnel is an injected starter whose tunnel listens on localPort.
func fixedTunnel(localPort int) func(context.Context, *grpcclient.AgentConnection, int) (*cloudAppTunnel, error) {
	return func(_ context.Context, _ *grpcclient.AgentConnection, remotePort int) (*cloudAppTunnel, error) {
		return &cloudAppTunnel{LocalPort: localPort, RemotePort: remotePort}, nil
	}
}

// offNetworkCloudConn is a cloud connection whose device reports a LAN
// address this machine cannot reach, with no Wendy Mesh: the case the cloud
// app tunnel exists for.
func offNetworkCloudConn(t *testing.T) *grpcclient.AgentConnection {
	t.Helper()
	stubLANAddressReachable(t, false)
	return &grpcclient.AgentConnection{
		Host:             "Dev Kit",
		Reconnect:        neverReconnect,
		ContainerService: &lifecycleFakeContainerClient{},
		AgentService: &fakeAgentVersionClient{resp: &agentpb.GetAgentVersionResponse{
			NetworkInterfaces: []*agentpb.NetworkInterface{{Name: "eth0", IpAddresses: []string{"10.20.30.40"}}},
		}},
	}
}

// headServer stands in for a running app at the tunnel's local end. It
// answers every request, counting the readiness probe's HEADs.
func headServer(t *testing.T) (port int, heads func() int) {
	t.Helper()
	var mu sync.Mutex
	count := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			mu.Lock()
			count++
			mu.Unlock()
		}
		io.WriteString(w, "app")
	}))
	t.Cleanup(server.Close)
	return testPort(t, server.Listener), func() int {
		mu.Lock()
		defer mu.Unlock()
		return count
	}
}

func freeLoopbackPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := testPort(t, ln)
	ln.Close()
	return port
}

// appBehindCloud is a fake cloud port dialer: every tunnel connection reaches
// target, as the relay reaches the app's port on the device.
func appBehindCloud(target string, dialed *[]int, mu *sync.Mutex) func(context.Context, int) (net.Conn, error) {
	return func(ctx context.Context, port int) (net.Conn, error) {
		mu.Lock()
		*dialed = append(*dialed, port)
		mu.Unlock()
		var d net.Dialer
		return d.DialContext(ctx, "tcp", target)
	}
}

func getBody(t *testing.T, rawURL string) string {
	t.Helper()
	client := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{Proxy: nil, DisableKeepAlives: true}}
	resp, err := client.Get(rawURL)
	if err != nil {
		t.Fatalf("GET %s: %v", rawURL, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return string(body)
}

func TestOpenCloudAppTunnelKeepsAppPortWhenFree(t *testing.T) {
	appPort, _ := headServer(t)
	port := freeLoopbackPort(t)
	var mu sync.Mutex
	var dialed []int
	conn := &grpcclient.AgentConnection{Host: "Dev Kit", Reconnect: neverReconnect, RegistryDialer: appBehindCloud(net.JoinHostPort("127.0.0.1", strconv.Itoa(appPort)), &dialed, &mu)}

	tunnel, err := openCloudAppTunnel(context.Background(), conn, port)
	if err != nil {
		t.Fatal(err)
	}
	defer tunnel.Close()
	if tunnel.LocalPort != port || tunnel.RemotePort != port {
		t.Fatalf("tunnel = localhost:%d → %d, want the app's own port %d on both ends", tunnel.LocalPort, tunnel.RemotePort, port)
	}
	if body := getBody(t, fmt.Sprintf("http://localhost:%d/", port)); body != "app" {
		t.Fatalf("body through tunnel = %q, want the app's", body)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(dialed) != 1 || dialed[0] != port {
		t.Fatalf("cloud dialer asked for ports %v, want [%d]", dialed, port)
	}
}

func TestOpenCloudAppTunnelFallsBackWhenPortInUse(t *testing.T) {
	for _, network := range []struct{ name, addr string }{{"ipv4 loopback", "127.0.0.1:0"}, {"ipv6 loopback", "[::1]:0"}} {
		t.Run(network.name, func(t *testing.T) {
			busy, err := net.Listen("tcp", network.addr)
			if err != nil {
				t.Skipf("cannot listen on %s: %v", network.addr, err)
			}
			defer busy.Close()
			go func() {
				for {
					c, err := busy.Accept()
					if err != nil {
						return
					}
					c.Close()
				}
			}()
			port := testPort(t, busy)
			appPort, _ := headServer(t)
			var mu sync.Mutex
			var dialed []int
			conn := &grpcclient.AgentConnection{Reconnect: neverReconnect, RegistryDialer: appBehindCloud(net.JoinHostPort("127.0.0.1", strconv.Itoa(appPort)), &dialed, &mu)}

			tunnel, err := openCloudAppTunnel(context.Background(), conn, port)
			if err != nil {
				t.Fatal(err)
			}
			defer tunnel.Close()
			if tunnel.LocalPort == port || tunnel.LocalPort == 0 || tunnel.RemotePort != port {
				t.Fatalf("tunnel = localhost:%d → %d, want a free local port forwarding to %d", tunnel.LocalPort, tunnel.RemotePort, port)
			}
			if body := getBody(t, fmt.Sprintf("http://127.0.0.1:%d/", tunnel.LocalPort)); body != "app" {
				t.Fatalf("body through tunnel = %q, want the app's", body)
			}
			mu.Lock()
			defer mu.Unlock()
			if len(dialed) != 1 || dialed[0] != port {
				t.Fatalf("cloud dialer asked for ports %v, want the device port [%d]", dialed, port)
			}
		})
	}
}

func TestOpenCloudAppTunnelClosesOnContextCancel(t *testing.T) {
	// The "app" holds each connection open until the client goes away.
	app, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	go func() {
		for {
			c, err := app.Accept()
			if err != nil {
				return
			}
			go func() { _, _ = io.Copy(io.Discard, c); c.Close() }()
		}
	}()
	var mu sync.Mutex
	var dialed []int
	conn := &grpcclient.AgentConnection{Reconnect: neverReconnect, RegistryDialer: appBehindCloud(app.Addr().String(), &dialed, &mu)}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tunnel, err := openCloudAppTunnel(ctx, conn, freeLoopbackPort(t))
	if err != nil {
		t.Fatal(err)
	}
	local := net.JoinHostPort("127.0.0.1", strconv.Itoa(tunnel.LocalPort))
	inFlight, err := net.Dial("tcp", local)
	if err != nil {
		t.Fatal(err)
	}
	defer inFlight.Close()
	if _, err := inFlight.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}

	cancel()
	select {
	case <-tunnel.done:
	case <-time.After(5 * time.Second):
		t.Fatal("tunnel still accepting 5s after its context ended")
	}
	if tunnel.open() {
		t.Fatal("open() = true after the context ended")
	}
	if c, err := net.DialTimeout("tcp", local, time.Second); err == nil {
		c.Close()
		t.Fatalf("%s still accepts connections after the run ended", local)
	}
	// The forwarded connection is closed too: a read ends promptly, not at
	// the deadline.
	_ = inFlight.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, err = bufio.NewReader(inFlight).ReadByte()
	var netErr net.Error
	if err == nil || (errors.As(err, &netErr) && netErr.Timeout()) {
		t.Fatalf("forwarded connection still open after the run ended: %v", err)
	}
	tunnel.Close() // idempotent after the context ended
}

func TestOpenCloudAppTunnelRequiresCloudDialer(t *testing.T) {
	if _, err := openCloudAppTunnel(context.Background(), &grpcclient.AgentConnection{Reconnect: neverReconnect}, 3005); err == nil {
		t.Fatal("opened a tunnel on a connection with no cloud port dialer")
	}
}

func TestCloudAppTunnelPort(t *testing.T) {
	readiness := func(port int) *appconfig.ReadinessConfig {
		return &appconfig.ReadinessConfig{TCPSocket: &appconfig.TCPSocketProbe{Port: port}}
	}
	hooks := func(h appconfig.HookCommand) *appconfig.HooksConfig { return &appconfig.HooksConfig{PostStart: &h} }
	httpPort := func(port int) []appconfig.Entitlement {
		return []appconfig.Entitlement{{Type: appconfig.EntitlementHTTP, Port: port}}
	}
	for _, tc := range []struct {
		name string
		cfg  *appconfig.AppConfig
		want int
	}{
		{"nil config", nil, 0},
		{"openURL port", &appconfig.AppConfig{Hooks: hooks(appconfig.HookCommand{OpenURL: "http://${WENDY_HOSTNAME}:3005/view"})}, 3005},
		{"openURL https default port", &appconfig.AppConfig{Hooks: hooks(appconfig.HookCommand{OpenURL: "https://${WENDY_HOSTNAME}/"})}, 443},
		{"openURL wins over http entitlement", &appconfig.AppConfig{Entitlements: httpPort(8080), Hooks: hooks(appconfig.HookCommand{OpenURL: "http://${WENDY_HOSTNAME}:9000"})}, 9000},
		{"http entitlement synthesizes openURL", &appconfig.AppConfig{Entitlements: httpPort(8080)}, 8080},
		{"cli hook uses readiness port", &appconfig.AppConfig{Readiness: readiness(3005), Hooks: hooks(appconfig.HookCommand{CLI: "wendy utils open-browser http://${WENDY_HOSTNAME}:3005"})}, 3005},
		{"device hostname only in query", &appconfig.AppConfig{Entitlements: httpPort(8765), Hooks: hooks(appconfig.HookCommand{OpenURL: "https://viewer.example/?ws=ws://${WENDY_HOSTNAME}:8765"})}, 8765},
		{"readiness alone is not forwarded", &appconfig.AppConfig{Readiness: readiness(6379)}, 0},
		{"cli hook not addressing the device", &appconfig.AppConfig{Readiness: readiness(3005), Hooks: hooks(appconfig.HookCommand{CLI: "say ready"})}, 0},
		{"cli hook with no known port", &appconfig.AppConfig{Hooks: hooks(appconfig.HookCommand{CLI: "open http://${WENDY_HOSTNAME}"})}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := cloudAppTunnelPort(tc.cfg); got != tc.want {
				t.Fatalf("cloudAppTunnelPort = %d, want %d", got, tc.want)
			}
		})
	}
}

// An off-network cloud run forwards the app's port on the same local port,
// probes readiness through it, and opens the browser on localhost.
func TestRunPostStartIfReadyCloudForwardsThroughTunnel(t *testing.T) {
	port, heads := headServer(t)
	calls := swapCloudAppTunnel(t, fixedTunnel(port))
	opened := swapBrowserOpen(t)
	conn := offNetworkCloudConn(t)
	cfg := &appconfig.AppConfig{
		AppID:     "camera-feed",
		Readiness: &appconfig.ReadinessConfig{TCPSocket: &appconfig.TCPSocketProbe{Port: port}, TimeoutSeconds: 5},
		Hooks:     &appconfig.HooksConfig{PostStart: &appconfig.HookCommand{OpenURL: fmt.Sprintf("http://${WENDY_HOSTNAME}:%d/view", port)}},
	}
	hookCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	out := captureStderr(t, func() {
		runPostStartIfReady(context.Background(), hookCtx, conn, cfg, runOptions{})
	})
	want := fmt.Sprintf("http://localhost:%d/view", port)
	if len(*opened) != 1 || (*opened)[0] != want {
		t.Fatalf("browser opened %v, want [%s]", *opened, want)
	}
	if len(*calls) != 1 || (*calls)[0].remotePort != port || (*calls)[0].ctx != hookCtx {
		t.Fatalf("tunnel starts = %+v, want one to port %d tied to the hook context", *calls, port)
	}
	if heads() == 0 {
		t.Fatal("readiness was not probed through the tunnel")
	}
	for _, line := range []string{
		fmt.Sprintf("Forwarding http://localhost:%d → camera-feed on Dev Kit through Wendy Cloud (closes when wendy run exits)", port),
		"App reachable at " + want,
		fmt.Sprintf("Waiting for localhost:%d to be ready", port),
	} {
		if !strings.Contains(out, line) {
			t.Errorf("missing %q in output:\n%s", line, out)
		}
	}
	if strings.Contains(out, "10.20.30.40") || strings.Contains(out, "Connect the desktop VPN") {
		t.Errorf("announced the unreachable LAN address or the skip notice:\n%s", out)
	}
}

// When the app's port is busy here, the tunnel listens elsewhere and the
// openURL follows it, keeping scheme, path and query.
func TestRunPostStartIfReadyCloudTunnelFallbackPortRewritesOpenURL(t *testing.T) {
	local, heads := headServer(t)
	swapCloudAppTunnel(t, fixedTunnel(local))
	opened := swapBrowserOpen(t)
	conn := offNetworkCloudConn(t)
	cfg := &appconfig.AppConfig{
		AppID:     "camera-feed",
		Readiness: &appconfig.ReadinessConfig{TCPSocket: &appconfig.TCPSocketProbe{Port: 3005}, TimeoutSeconds: 5},
		Hooks:     &appconfig.HooksConfig{PostStart: &appconfig.HookCommand{OpenURL: "https://${WENDY_HOSTNAME}:3005/view?app=${WENDY_APP_ID}"}},
	}

	out := captureStderr(t, func() {
		runPostStartIfReady(context.Background(), context.Background(), conn, cfg, runOptions{})
	})
	want := fmt.Sprintf("https://localhost:%d/view?app=camera-feed", local)
	if len(*opened) != 1 || (*opened)[0] != want {
		t.Fatalf("browser opened %v, want [%s]", *opened, want)
	}
	if heads() == 0 {
		t.Fatal("readiness was not probed at the tunnel's local port")
	}
	if !strings.Contains(out, fmt.Sprintf("Forwarding https://localhost:%d → camera-feed (port 3005) on Dev Kit through Wendy Cloud; port 3005 is in use on this machine", local)) {
		t.Errorf("missing the fallback-port forwarding line:\n%s", out)
	}
}

// The camera-feed-yolo shape: a cli hook opening http://${WENDY_HOSTNAME}:3005
// runs against localhost when the tunnel kept port 3005.
func TestServiceHookRunnerCloudTunnelRunsCLIHookOnSamePort(t *testing.T) {
	swapCloudAppTunnel(t, fixedTunnel(3005))
	execs := swapPostStartExec(t)
	conn := offNetworkCloudConn(t)
	r := &serviceHookRunner{conn: conn, opts: runOptions{hostReadinessConfirmed: true}}
	cfg := &appconfig.AppConfig{
		AppID:     "camera-feed-yolo",
		Readiness: &appconfig.ReadinessConfig{TCPSocket: &appconfig.TCPSocketProbe{Port: 3005}},
		Hooks:     &appconfig.HooksConfig{PostStart: &appconfig.HookCommand{CLI: "wendy utils open-browser http://${WENDY_HOSTNAME}:3005"}},
	}

	captureStderr(t, func() { r.runOne(context.Background(), context.Background(), cfg) })
	if len(*execs) != 1 || (*execs)[0] != "wendy utils open-browser http://localhost:3005" {
		t.Fatalf("cli hooks run = %q, want the hook against localhost", *execs)
	}
}

// A cli hook is shell text, so when the tunnel had to move ports the run
// skips it and prints the local URL instead of rewriting the command.
func TestServiceHookRunnerCloudTunnelSkipsCLIHookOnOtherPort(t *testing.T) {
	swapCloudAppTunnel(t, fixedTunnel(54321))
	execs := swapPostStartExec(t)
	conn := offNetworkCloudConn(t)
	r := &serviceHookRunner{conn: conn, opts: runOptions{hostReadinessConfirmed: true}}
	cfg := &appconfig.AppConfig{
		AppID:     "camera-feed-yolo",
		Readiness: &appconfig.ReadinessConfig{TCPSocket: &appconfig.TCPSocketProbe{Port: 3005}},
		Hooks:     &appconfig.HooksConfig{PostStart: &appconfig.HookCommand{CLI: "wendy utils open-browser http://${WENDY_HOSTNAME}:3005"}},
	}

	out := captureStderr(t, func() { r.runOne(context.Background(), context.Background(), cfg) })
	if len(*execs) != 0 {
		t.Fatalf("ran cli hook %q against the wrong port", *execs)
	}
	if !strings.Contains(out, "Skipping the postStart cli hook") || !strings.Contains(out, "http://localhost:54321") {
		t.Fatalf("missing the skip notice with the local URL:\n%s", out)
	}
}

// When the tunnel cannot start, the run falls back to today's skip notice
// plus the exact command to forward the port by hand.
func TestRunPostStartIfReadyCloudTunnelFailureFallsBack(t *testing.T) {
	swapCloudAppTunnel(t, func(context.Context, *grpcclient.AgentConnection, int) (*cloudAppTunnel, error) {
		return nil, errors.New("listening on 127.0.0.1: no ports left")
	})
	opened := swapBrowserOpen(t)
	execs := swapPostStartExec(t)
	conn := offNetworkCloudConn(t)
	cfg := &appconfig.AppConfig{
		AppID:     "camera-feed-yolo",
		Readiness: &appconfig.ReadinessConfig{TCPSocket: &appconfig.TCPSocketProbe{Port: 3005}, TimeoutSeconds: 5},
		Hooks:     &appconfig.HooksConfig{PostStart: &appconfig.HookCommand{CLI: "wendy utils open-browser http://${WENDY_HOSTNAME}:3005"}},
	}

	start := time.Now()
	out := captureStderr(t, func() {
		if cmd := runPostStartIfReady(context.Background(), context.Background(), conn, cfg, runOptions{}); cmd != nil {
			t.Errorf("started a hook after the tunnel failed: %v", cmd)
		}
	})
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Errorf("took %v; a failed tunnel must skip readiness, not wait on it", elapsed)
	}
	if len(*opened) != 0 || len(*execs) != 0 {
		t.Fatalf("ran hooks after the tunnel failed: browser=%v cli=%v", *opened, *execs)
	}
	for _, line := range []string{
		"Could not open a Wendy Cloud tunnel to camera-feed-yolo port 3005: listening on 127.0.0.1: no ports left",
		"Connect the desktop VPN to reach this cloud device.",
		`wendy cloud tunnel 3005:3005 --device "Dev Kit"`,
		"http://localhost:3005",
	} {
		if !strings.Contains(out, line) {
			t.Errorf("missing %q in output:\n%s", line, out)
		}
	}
}

// A detached run cannot keep a tunnel open, so it prints the command instead;
// JSON output (the MCP run tool's) is unchanged.
func TestDetachedRunCloudWithoutMeshPrintsTunnelCommand(t *testing.T) {
	swapCloudAppTunnel(t, nil)
	conn := &grpcclient.AgentConnection{Host: "Dev Kit", Reconnect: neverReconnect}
	cfg := &appconfig.AppConfig{AppID: "web", Entitlements: []appconfig.Entitlement{{Type: appconfig.EntitlementHTTP, Port: 8080}}}

	t.Run("text", func(t *testing.T) {
		isolateDetachedOutput(t)
		jsonOutput = false
		stdout, stderr := captureBoth(t, func() {
			if err := (runOptions{detachedOutput: true}).reportDetachedRun(context.Background(), conn, "web", cfg, cfg); err != nil {
				t.Fatal(err)
			}
		})
		want := `To open web from this machine, run wendy cloud tunnel 8080:8080 --device "Dev Kit" and open http://localhost:8080`
		if stdout != "" || !strings.Contains(stderr, want) || strings.Count(stderr, "wendy cloud tunnel") != 1 {
			t.Fatalf("want one hint %q; stdout=%q stderr=%q", want, stdout, stderr)
		}
	})
	t.Run("json", func(t *testing.T) {
		isolateDetachedOutput(t)
		stdout, stderr := captureBoth(t, func() {
			if err := (runOptions{detachedOutput: true}).reportDetachedRun(context.Background(), conn, "web", cfg); err != nil {
				t.Fatal(err)
			}
		})
		if result := decodeDetachedResult(t, stdout); result.URL != "" || len(result.Endpoints) != 0 {
			t.Fatalf("JSON result changed: %+v", result)
		}
		if strings.Contains(stdout+stderr, "wendy cloud tunnel") {
			t.Fatalf("JSON run printed the tunnel hint: stdout=%q stderr=%q", stdout, stderr)
		}
	})
}

// Paths with a route to the device never open a tunnel: a reachable LAN
// address, an active Wendy Mesh, and a direct connection.
func TestResolveHookRouteLeavesRoutedTargetsAlone(t *testing.T) {
	swapCloudAppTunnel(t, nil)
	cfg := &appconfig.AppConfig{AppID: "web", Entitlements: []appconfig.Entitlement{{Type: appconfig.EntitlementHTTP, Port: 8080}}}
	agent := func() *fakeAgentVersionClient {
		return &fakeAgentVersionClient{resp: &agentpb.GetAgentVersionResponse{
			NetworkInterfaces: []*agentpb.NetworkInterface{{Name: "eth0", IpAddresses: []string{"10.20.30.40"}}},
		}}
	}

	t.Run("reachable LAN", func(t *testing.T) {
		stubLANAddressReachable(t, true)
		conn := &grpcclient.AgentConnection{Host: "Dev Kit", Reconnect: neverReconnect, AgentService: agent()}
		var route hookRoute
		var ok bool
		captureStderr(t, func() {
			route, ok = resolveHookRoute(context.Background(), context.Background(), conn, cfg, nil, false)
		})
		if !ok || route.host != "10.20.30.40" || route.tunnel != nil {
			t.Fatalf("route = %+v ok=%v, want the LAN address", route, ok)
		}
	})
	t.Run("mesh", func(t *testing.T) {
		originalLookup := osLookupHostFn
		t.Cleanup(func() { osLookupHostFn = originalLookup })
		osLookupHostFn = func(context.Context, string) ([]string, error) { return []string{"10.99.0.7"}, nil }
		conn := &grpcclient.AgentConnection{Host: "Dev Kit", MeshHost: "device-7.mesh.wendy.internal", Reconnect: neverReconnect, AgentService: agent()}
		var route hookRoute
		var ok bool
		captureStderr(t, func() {
			route, ok = resolveHookRoute(context.Background(), context.Background(), conn, cfg, nil, false)
		})
		if !ok || route.host != conn.MeshHost || route.tunnel != nil {
			t.Fatalf("route = %+v ok=%v, want the mesh host", route, ok)
		}
	})
	t.Run("direct", func(t *testing.T) {
		conn := &grpcclient.AgentConnection{Host: "dev.local", AgentService: agent()}
		var route hookRoute
		var ok bool
		captureStderr(t, func() {
			route, ok = resolveHookRoute(context.Background(), context.Background(), conn, cfg, nil, false)
		})
		if !ok || route.host != "dev.local" || route.tunnel != nil {
			t.Fatalf("route = %+v ok=%v, want the direct host", route, ok)
		}
	})
}

func TestAppTunnelSetReusesOpenTunnelPerPort(t *testing.T) {
	var tunnels []*cloudAppTunnel
	calls := swapCloudAppTunnel(t, func(_ context.Context, _ *grpcclient.AgentConnection, remotePort int) (*cloudAppTunnel, error) {
		tunnel := &cloudAppTunnel{LocalPort: remotePort, RemotePort: remotePort, done: make(chan struct{})}
		tunnel.stop = sync.OnceFunc(func() { close(tunnel.done) })
		tunnels = append(tunnels, tunnel)
		return tunnel, nil
	})
	var set appTunnelSet
	first, opened, err := set.forward(context.Background(), nil, 3005)
	if err != nil || !opened {
		t.Fatalf("first forward: opened=%v err=%v", opened, err)
	}
	again, opened, err := set.forward(context.Background(), nil, 3005)
	if err != nil || opened || again != first {
		t.Fatalf("second forward to the same port: opened=%v err=%v same=%v; want the open tunnel reused", opened, err, again == first)
	}
	if _, opened, _ := set.forward(context.Background(), nil, 8080); !opened {
		t.Fatal("a second port did not get its own tunnel")
	}
	first.Close()
	if _, opened, _ := set.forward(context.Background(), nil, 3005); !opened {
		t.Fatal("a closed tunnel was reused")
	}
	if len(*calls) != 3 {
		t.Fatalf("tunnel starts = %d, want 3", len(*calls))
	}
	set.closeAll()
	for _, tunnel := range tunnels {
		if tunnel.open() {
			t.Fatalf("closeAll left localhost:%d open", tunnel.LocalPort)
		}
	}
}

// A watch session keeps one tunnel across deploys: a deploy whose readiness
// failed releases the host lifecycle, and the retry reuses the session's
// tunnel instead of opening another.
func TestRunPostStartIfReadyWatchKeepsOneTunnel(t *testing.T) {
	dead := freeLoopbackPort(t) // nothing answers: readiness fails
	calls := swapCloudAppTunnel(t, fixedTunnel(dead))
	swapBrowserOpen(t)
	conn := offNetworkCloudConn(t)
	opts := withWatchInvariants(runOptions{readinessTimeout: 300 * time.Millisecond})
	session, cancel := context.WithCancel(context.Background())
	defer cancel()
	opts.watchState.setSessionContext(session)
	cfg := &appconfig.AppConfig{
		AppID:     "web",
		Readiness: &appconfig.ReadinessConfig{TCPSocket: &appconfig.TCPSocketProbe{Port: 3005}},
		Hooks:     &appconfig.HooksConfig{PostStart: &appconfig.HookCommand{OpenURL: "http://${WENDY_HOSTNAME}:3005"}},
	}

	out := captureStderr(t, func() {
		for range 2 {
			cycle, cycleCancel := context.WithCancel(context.Background())
			runPostStartIfReady(cycle, opts.watchState.hookContext(cycle), conn, cfg, opts)
			cycleCancel()
		}
	})
	if len(*calls) != 1 || (*calls)[0].ctx != session {
		t.Fatalf("tunnel starts = %+v, want one tied to the watch session", *calls)
	}
	if strings.Count(out, "Forwarding ") != 1 || !strings.Contains(out, "(closes when the watch stops)") {
		t.Fatalf("want one forwarding line for the session:\n%s", out)
	}
	if strings.Count(out, "Waiting for localhost:") != 2 {
		t.Fatalf("the second deploy did not retry readiness through the same tunnel:\n%s", out)
	}
}

// The whole attached path against a real tunnel: readiness and the browser
// go through it, and the run's end closes it.
func TestServiceHookRunnerCloudTunnelEndToEndClosesWithRun(t *testing.T) {
	swapBrowserOpen(t)
	appPort, heads := headServer(t)
	port := freeLoopbackPort(t)
	var mu sync.Mutex
	var dialed []int
	conn := offNetworkCloudConn(t)
	conn.RegistryDialer = appBehindCloud(net.JoinHostPort("127.0.0.1", strconv.Itoa(appPort)), &dialed, &mu)
	r := &serviceHookRunner{conn: conn}
	cfg := &appconfig.AppConfig{AppID: "web", Entitlements: []appconfig.Entitlement{{Type: appconfig.EntitlementHTTP, Port: port}}}
	runCtx, runCancel := context.WithCancel(context.Background())
	defer runCancel()

	out := captureStderr(t, func() { r.runOne(runCtx, runCtx, cfg) })
	if heads() == 0 {
		t.Fatalf("readiness did not reach the app through the tunnel:\n%s", out)
	}
	local := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	if body := getBody(t, "http://"+local+"/"); body != "app" {
		t.Fatalf("body through the tunnel = %q", body)
	}

	runCancel()
	r.reap()
	if c, err := net.DialTimeout("tcp", local, time.Second); err == nil {
		c.Close()
		t.Fatalf("%s still accepts connections after the run ended", local)
	}
}
