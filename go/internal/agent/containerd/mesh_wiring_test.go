package containerd

import (
	"net"
	"os"
	"reflect"
	"testing"

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
}
