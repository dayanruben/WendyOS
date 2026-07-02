package containerd

import (
	"net"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	"go.uber.org/zap"

	"github.com/wendylabsinc/wendy/go/internal/shared/appconfig"
)

// wantGatewayFor computes the expected ".1" gateway address for a subnet in
// CIDR notation, without relying on fragile string slicing (allocateSubnet
// hands back /28s whose last octet varies, e.g. "10.1.2.192/28", so chopping
// a fixed-length suffix off the string is not safe).
func wantGatewayFor(t *testing.T, subnet string) string {
	t.Helper()
	_, ipNet, err := net.ParseCIDR(subnet)
	if err != nil {
		t.Fatalf("parsing subnet %q: %v", subnet, err)
	}
	gw := make(net.IP, len(ipNet.IP))
	copy(gw, ipNet.IP)
	gw[len(gw)-1] |= 1
	return gw.String()
}

// withTempSubnetRegistry redirects cniSubnetRegistryPath to a fresh temp file
// for the duration of the test, mirroring the pattern used in cni_test.go so
// allocateSubnet (and therefore meshGateway) never touches the real
// /run/wendy/cni state during unit tests.
func withTempSubnetRegistry(t *testing.T) {
	t.Helper()
	tmp := t.TempDir()
	orig := cniSubnetRegistryPath
	cniSubnetRegistryPath = tmp + "/subnets.json"
	t.Cleanup(func() { cniSubnetRegistryPath = orig })
}

func TestFindMeshEntitlement(t *testing.T) {
	tests := []struct {
		name         string
		entitlements []appconfig.Entitlement
		wantFound    bool
		wantEnt      appconfig.Entitlement
	}{
		{
			name:         "no entitlements",
			entitlements: nil,
			wantFound:    false,
		},
		{
			name: "mesh mode present",
			entitlements: []appconfig.Entitlement{
				{Type: appconfig.EntitlementNetwork, Mode: "mesh", ServiceCIDR: "10.99.0.0/16"},
			},
			wantFound: true,
			wantEnt:   appconfig.Entitlement{Type: appconfig.EntitlementNetwork, Mode: "mesh", ServiceCIDR: "10.99.0.0/16"},
		},
		{
			name: "host mode is not mesh",
			entitlements: []appconfig.Entitlement{
				{Type: appconfig.EntitlementNetwork, Mode: "host"},
			},
			wantFound: false,
		},
		{
			name: "host-admin mode is not mesh",
			entitlements: []appconfig.Entitlement{
				{Type: appconfig.EntitlementNetwork, Mode: "host-admin"},
			},
			wantFound: false,
		},
		{
			name: "none mode is not mesh",
			entitlements: []appconfig.Entitlement{
				{Type: appconfig.EntitlementNetwork, Mode: "none"},
			},
			wantFound: false,
		},
		{
			name: "unrelated entitlements only",
			entitlements: []appconfig.Entitlement{
				{Type: appconfig.EntitlementBluetooth},
				{Type: appconfig.EntitlementGPU},
			},
			wantFound: false,
		},
		{
			name: "mesh entitlement among others",
			entitlements: []appconfig.Entitlement{
				{Type: appconfig.EntitlementBluetooth},
				{Type: appconfig.EntitlementNetwork, Mode: "mesh", ServiceCIDR: "172.30.0.0/16"},
			},
			wantFound: true,
			wantEnt:   appconfig.Entitlement{Type: appconfig.EntitlementNetwork, Mode: "mesh", ServiceCIDR: "172.30.0.0/16"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := findMeshEntitlement(tc.entitlements)
			if ok != tc.wantFound {
				t.Fatalf("findMeshEntitlement() ok = %v, want %v", ok, tc.wantFound)
			}
			if ok && !reflect.DeepEqual(got, tc.wantEnt) {
				t.Fatalf("findMeshEntitlement() = %+v, want %+v", got, tc.wantEnt)
			}
		})
	}
}

func TestNormalizeCIDR(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{name: "already canonical", in: "10.99.0.0/16", want: "10.99.0.0/16"},
		{name: "host bits set are masked off", in: "10.99.0.5/16", want: "10.99.0.0/16"},
		{name: "single host /32", in: "10.99.0.5/32", want: "10.99.0.5/32"},
		{name: "invalid CIDR", in: "not-a-cidr", wantErr: true},
		{name: "missing prefix", in: "10.99.0.0", wantErr: true},
		{name: "empty string", in: "", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := normalizeCIDR(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("normalizeCIDR(%q) = %q, nil; want error", tc.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("normalizeCIDR(%q) unexpected error: %v", tc.in, err)
			}
			if got != tc.want {
				t.Fatalf("normalizeCIDR(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestMeshGateway(t *testing.T) {
	withTempSubnetRegistry(t)

	appID := "com.example.meshapp"
	subnet, err := allocateSubnet(appID)
	if err != nil {
		t.Fatalf("allocateSubnet: %v", err)
	}

	gateway, err := meshGateway(appID)
	if err != nil {
		t.Fatalf("meshGateway: %v", err)
	}

	// The gateway must be the ".1" address of the exact subnet allocateSubnet
	// returned for this appID — i.e. the same subnet the bridge plugin
	// configures with isGateway:true (buildBridgeCNIConfig), not an
	// independently-derived value that could disagree with it.
	wantGateway := wantGatewayFor(t, subnet)
	if gateway != wantGateway {
		t.Fatalf("meshGateway(%q) = %q, want %q (derived from allocated subnet %q)", appID, gateway, wantGateway, subnet)
	}
}

func TestMeshGatewayIsStableAcrossCalls(t *testing.T) {
	withTempSubnetRegistry(t)

	appID := "com.example.stableapp"
	first, err := meshGateway(appID)
	if err != nil {
		t.Fatalf("first meshGateway: %v", err)
	}
	second, err := meshGateway(appID)
	if err != nil {
		t.Fatalf("second meshGateway: %v", err)
	}
	if first != second {
		t.Fatalf("meshGateway not stable across calls: %q vs %q", first, second)
	}
}

func TestResolveMeshEgress(t *testing.T) {
	withTempSubnetRegistry(t)

	t.Run("mesh entitlement present returns gateway and normalized cidr", func(t *testing.T) {
		appID := "com.example.mesh1"
		subnet, err := allocateSubnet(appID)
		if err != nil {
			t.Fatalf("allocateSubnet: %v", err)
		}
		wantGateway := wantGatewayFor(t, subnet)

		entitlements := []appconfig.Entitlement{
			{Type: appconfig.EntitlementNetwork, Mode: "mesh", ServiceCIDR: "10.99.0.5/16"},
		}
		params, ok, err := resolveMeshEgress(entitlements, appID)
		if err != nil {
			t.Fatalf("resolveMeshEgress: %v", err)
		}
		if !ok {
			t.Fatal("resolveMeshEgress: ok = false, want true for mesh entitlement")
		}
		if params.gateway != wantGateway {
			t.Errorf("gateway = %q, want %q", params.gateway, wantGateway)
		}
		// The raw entitlement CIDR has host bits set ("10.99.0.5/16"); the
		// resolved params must carry the normalized form (C3a-review Minor #1).
		if params.cidr != "10.99.0.0/16" {
			t.Errorf("cidr = %q, want normalized %q", params.cidr, "10.99.0.0/16")
		}
	})

	t.Run("no network entitlement is a no-op", func(t *testing.T) {
		params, ok, err := resolveMeshEgress(nil, "com.example.none1")
		if err != nil {
			t.Fatalf("resolveMeshEgress: unexpected error: %v", err)
		}
		if ok {
			t.Fatalf("resolveMeshEgress: ok = true, want false for no entitlements; got %+v", params)
		}
	})

	t.Run("host mode is a no-op", func(t *testing.T) {
		entitlements := []appconfig.Entitlement{{Type: appconfig.EntitlementNetwork, Mode: "host"}}
		_, ok, err := resolveMeshEgress(entitlements, "com.example.host1")
		if err != nil {
			t.Fatalf("resolveMeshEgress: unexpected error: %v", err)
		}
		if ok {
			t.Fatal("resolveMeshEgress: ok = true, want false for mode host")
		}
	})

	t.Run("host-admin mode is a no-op", func(t *testing.T) {
		entitlements := []appconfig.Entitlement{{Type: appconfig.EntitlementNetwork, Mode: "host-admin"}}
		_, ok, err := resolveMeshEgress(entitlements, "com.example.hostadmin1")
		if err != nil {
			t.Fatalf("resolveMeshEgress: unexpected error: %v", err)
		}
		if ok {
			t.Fatal("resolveMeshEgress: ok = true, want false for mode host-admin")
		}
	})

	t.Run("none mode is a no-op", func(t *testing.T) {
		entitlements := []appconfig.Entitlement{{Type: appconfig.EntitlementNetwork, Mode: "none"}}
		_, ok, err := resolveMeshEgress(entitlements, "com.example.none2")
		if err != nil {
			t.Fatalf("resolveMeshEgress: unexpected error: %v", err)
		}
		if ok {
			t.Fatal("resolveMeshEgress: ok = true, want false for mode none")
		}
	})

	t.Run("mesh entitlement with invalid serviceCIDR errors", func(t *testing.T) {
		entitlements := []appconfig.Entitlement{{Type: appconfig.EntitlementNetwork, Mode: "mesh", ServiceCIDR: "not-a-cidr"}}
		_, ok, err := resolveMeshEgress(entitlements, "com.example.badcidr")
		if err == nil {
			t.Fatal("resolveMeshEgress: expected error for invalid serviceCIDR")
		}
		if !ok {
			t.Fatal("resolveMeshEgress: ok should be true (mesh entitlement was found) even though params resolution failed")
		}
	})
}

// TestWriteMeshResolvConf verifies the per-app resolv.conf content and that it
// points at the same gateway meshGateway derives independently, so a meshed
// container's DNS listener address always matches its resolv.conf nameserver.
//
// withTempSubnetRegistry redirects the CNI subnet registry (as the other
// tests in this file do) so meshGateway never touches the real
// /run/wendy/cni state, which is not writable in a non-root test sandbox.
func TestWriteMeshResolvConf(t *testing.T) {
	withTempSubnetRegistry(t)

	dir := t.TempDir()
	path, err := writeMeshResolvConfIn(dir, "myapp")
	if err != nil {
		t.Fatalf("writeMeshResolvConfIn: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	gw, err := meshGateway("myapp")
	if err != nil {
		t.Fatal(err)
	}
	want := "nameserver " + gw + "\noptions ndots:1\n"
	if string(data) != want {
		t.Fatalf("resolv.conf = %q, want %q", data, want)
	}

	// Overwrite an existing file (sibling-service create rewrites the same
	// appID-keyed path while a running sibling may have it bind-mounted): the
	// write must go through a temp-file + rename so the mounted file is never
	// observed truncated, and the final content must be intact.
	path2, err := writeMeshResolvConfIn(dir, "myapp")
	if err != nil {
		t.Fatalf("writeMeshResolvConfIn (overwrite): %v", err)
	}
	if path2 != path {
		t.Fatalf("overwrite returned different path: %q vs %q", path2, path)
	}
	data, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != want {
		t.Fatalf("resolv.conf after overwrite = %q, want %q", data, want)
	}
	// No stray temp files may be left behind next to the mounted file.
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != "resolv.conf" {
			t.Fatalf("unexpected leftover file %q next to resolv.conf", e.Name())
		}
	}
}

// TestRecreateMeshResolvConfIn covers reboot resilience for meshed containers'
// resolv.conf bind-mount source (C-final-review Fix 1): /etc/resolv.conf is
// bind-mounted from a tmpfs path baked into the OCI spec once at
// CreateContainerWithProgress time, but a reboot wipes tmpfs while containerd
// keeps the container definition (and its spec's mount list) around.
// ReconcileBootContainers restarts surviving containers via StartContainer
// directly — never CreateContainer — so without recreating the file before
// the runtime processes the spec's mounts, a meshed container's task creation
// fails outright and it never starts again after a reboot.
func TestRecreateMeshResolvConfIn(t *testing.T) {
	withTempSubnetRegistry(t)

	meshEnts := []appconfig.Entitlement{
		{Type: appconfig.EntitlementNetwork, Mode: "mesh", ServiceCIDR: "10.99.0.0/16"},
	}

	t.Run("recreates a deleted resolv.conf for a meshed app", func(t *testing.T) {
		dir := t.TempDir()
		path, err := writeMeshResolvConfIn(dir, "myapp")
		if err != nil {
			t.Fatalf("writeMeshResolvConfIn: %v", err)
		}
		// Simulate a reboot wiping tmpfs: the container/task definition survives
		// (that's containerd's job), but the bind-mount source is gone.
		if err := os.Remove(path); err != nil {
			t.Fatalf("os.Remove: %v", err)
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("precondition: resolv.conf should be gone, stat err = %v", err)
		}

		ok, err := recreateMeshResolvConfIn(dir, meshEnts, "myapp")
		if err != nil {
			t.Fatalf("recreateMeshResolvConfIn: %v", err)
		}
		if !ok {
			t.Fatal("recreateMeshResolvConfIn: ok = false, want true for meshed app")
		}
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("resolv.conf was not recreated: %v", err)
		}
	})

	t.Run("no-op for an app without the mesh entitlement", func(t *testing.T) {
		dir := t.TempDir()
		ok, err := recreateMeshResolvConfIn(dir, nil, "otherapp")
		if err != nil {
			t.Fatalf("recreateMeshResolvConfIn: unexpected error: %v", err)
		}
		if ok {
			t.Fatal("recreateMeshResolvConfIn: ok = true, want false without mesh entitlement")
		}
		if _, err := os.Stat(filepath.Join(dir, "otherapp", "resolv.conf")); !os.IsNotExist(err) {
			t.Fatalf("recreateMeshResolvConfIn must not create a file for a non-meshed app, stat err = %v", err)
		}
	})
}

// fakeMeshDNS is a recording implementation of the meshDNSService seam used
// by ensureMeshDNS/releaseMeshDNS, letting tests assert Ensure/Release
// pairing without binding real UDP listeners.
type fakeMeshDNS struct {
	mu         sync.Mutex
	failEnsure bool
	ensures    []string
	releases   []string
}

func (f *fakeMeshDNS) EnsureListener(gatewayIP string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failEnsure {
		return os.ErrPermission
	}
	f.ensures = append(f.ensures, gatewayIP)
	return nil
}

func (f *fakeMeshDNS) ReleaseListener(gatewayIP string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.releases = append(f.releases, gatewayIP)
}

func (f *fakeMeshDNS) counts() (ensures, releases int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.ensures), len(f.releases)
}

// TestMeshDNSRefcountGuard covers the sibling-imbalance scenario: two
// services of one app share a single gateway listener. Service A's
// EnsureListener fails, sibling B's succeeds. A's teardown must NOT call
// ReleaseListener (that would decrement B's live refcount); B's must release
// exactly once, and a second teardown of B (stopOne-then-deleteOne double
// teardown) must be a no-op.
func TestMeshDNSRefcountGuard(t *testing.T) {
	withTempSubnetRegistry(t)

	appID := "com.example.refguard"
	gw, err := meshGateway(appID)
	if err != nil {
		t.Fatalf("meshGateway: %v", err)
	}

	fake := &fakeMeshDNS{}
	c := &Client{logger: zap.NewNop(), meshDNS: fake}

	// Service A: ensure fails — no refcount was taken.
	fake.failEnsure = true
	c.ensureMeshDNS(appID+"_a", gw)
	fake.failEnsure = false

	// Service B: ensure succeeds — one refcount held.
	c.ensureMeshDNS(appID+"_b", gw)

	if e, r := fake.counts(); e != 1 || r != 0 {
		t.Fatalf("after ensures: ensures=%d releases=%d, want 1/0", e, r)
	}

	// A's teardown must not release the listener B holds.
	c.releaseMeshDNS(appID+"_a", appID)
	if _, r := fake.counts(); r != 0 {
		t.Fatalf("release after failed ensure must be a no-op, got %d releases", r)
	}

	// B's teardown releases exactly once.
	c.releaseMeshDNS(appID+"_b", appID)
	if _, r := fake.counts(); r != 1 {
		t.Fatalf("release after successful ensure: got %d releases, want 1", r)
	}

	// Double teardown (stopOne then deleteOne) must not over-release.
	c.releaseMeshDNS(appID+"_b", appID)
	if _, r := fake.counts(); r != 1 {
		t.Fatalf("double teardown must not over-release: got %d releases, want 1", r)
	}
}

// TestEnsureMeshDNSIsIdempotentPerContainer covers the monitor-restart
// scenario (C-final-review Fix 3): restartSingle calls StartContainer
// directly, with no intervening stopOne/teardownMeshEgress to release the
// container's held listener reference first. Without a same-container guard,
// ensureMeshDNS would call EnsureListener a second time (refs++) while
// meshDNSHeld[name] was already true, so the paired release in
// teardownMeshEgress would only ever bring the refcount down by one,
// permanently leaking one reference (and, on the underlying real DNS server,
// the listener itself) per restart.
func TestEnsureMeshDNSIsIdempotentPerContainer(t *testing.T) {
	withTempSubnetRegistry(t)

	appID := "com.example.restartguard"
	containerName := appID + "_svc"
	gw, err := meshGateway(appID)
	if err != nil {
		t.Fatalf("meshGateway: %v", err)
	}

	fake := &fakeMeshDNS{}
	c := &Client{logger: zap.NewNop(), meshDNS: fake}

	// First start: acquires the listener.
	c.ensureMeshDNS(containerName, gw)
	if e, r := fake.counts(); e != 1 || r != 0 {
		t.Fatalf("after first ensure: ensures=%d releases=%d, want 1/0", e, r)
	}

	// Monitor restart calls StartContainer directly (no stopOne/teardown in
	// between), so ensureMeshDNS runs again for the same container name while
	// meshDNSHeld[containerName] is still true. This must be a no-op: the
	// listener is already held for this container.
	c.ensureMeshDNS(containerName, gw)
	if e, r := fake.counts(); e != 1 || r != 0 {
		t.Fatalf("after re-ensure without teardown: ensures=%d releases=%d, want 1/0 (must not double-acquire)", e, r)
	}

	// A single teardown must still release exactly the one refcount actually
	// held, leaving nothing stranded.
	c.releaseMeshDNS(containerName, appID)
	if _, r := fake.counts(); r != 1 {
		t.Fatalf("release after re-ensure: got %d releases, want 1", r)
	}
}

// TestTeardownMeshEgressReleasesHeldListener exercises the full
// teardownMeshEgress path a delete-without-stop takes: a held DNS listener
// is released even when the container IP could not be recovered (ip == ""),
// and a container that never acquired one releases nothing.
func TestTeardownMeshEgressReleasesHeldListener(t *testing.T) {
	withTempSubnetRegistry(t)

	appID := "com.example.delrelease"
	containerName := appID + "_svc"
	gw, err := meshGateway(appID)
	if err != nil {
		t.Fatalf("meshGateway: %v", err)
	}

	fake := &fakeMeshDNS{}
	c := &Client{logger: zap.NewNop(), meshDNS: fake}
	meshEnts := []appconfig.Entitlement{
		{Type: appconfig.EntitlementNetwork, Mode: "mesh", ServiceCIDR: "10.99.0.0/16"},
	}

	// Simulate a successful start's DNS acquisition, then a delete path that
	// lost the IP (serviceIPs entry already gone): the listener must still be
	// released.
	c.ensureMeshDNS(containerName, gw)
	c.teardownMeshEgress(meshEnts, containerName, appID, "")
	if _, r := fake.counts(); r != 1 {
		t.Fatalf("teardown of held listener: got %d releases, want 1", r)
	}

	// A container without the mesh entitlement must not touch the listener.
	c.teardownMeshEgress(nil, containerName, appID, "")
	if _, r := fake.counts(); r != 1 {
		t.Fatalf("teardown without mesh entitlement must not release: got %d releases, want 1", r)
	}

	// A meshed container whose ensure never happened must not release.
	c.teardownMeshEgress(meshEnts, appID+"_other", appID, "")
	if _, r := fake.counts(); r != 1 {
		t.Fatalf("teardown of never-held listener must not release: got %d releases, want 1", r)
	}
}
