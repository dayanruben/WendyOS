package commands

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/wendylabsinc/wendy/go/internal/cli/grpcclient"
	"github.com/wendylabsinc/wendy/go/internal/cli/vm"
	"github.com/wendylabsinc/wendy/go/internal/shared/config"
	"github.com/wendylabsinc/wendy/go/internal/shared/models"
)

// stubLoopbackVMs describes which running VM forwards which loopback port.
func stubLoopbackVMs(t *testing.T, byPort map[int]string) {
	t.Helper()
	orig := loopbackVMNameFn
	loopbackVMNameFn = func(port int) (string, bool) {
		name, ok := byPort[port]
		return name, ok
	}
	t.Cleanup(func() { loopbackVMNameFn = orig })
}

var pinA, pinB = config.DevicePin{OrgID: 7, CloudGRPC: "grpc.a.sh:443", AssetID: "42"}, config.DevicePin{OrgID: 7, CloudGRPC: "grpc.a.sh:443", AssetID: "43"}

func TestPinKeyDerivationKeysLoopbackPerEndpoint(t *testing.T) {
	calls := 0
	orig := loopbackVMNameFn
	loopbackVMNameFn = func(port int) (string, bool) {
		calls++
		if port == 50151 {
			return "dev", true
		}
		return "", false
	}
	t.Cleanup(func() { loopbackVMNameFn = orig })

	for addr, want := range map[string]string{
		"127.0.0.1:50151": "vm:dev",
		"localhost:50151": "vm:dev",
		"127.0.0.1:50051": "127.0.0.1:50051",
		"localhost:50051": "localhost:50051",
		"[::1]:50051":     "[::1]:50051",
		"127.0.0.1":       "127.0.0.1",
		"vm:dev":          "vm:dev",
		"sim":             "vm:sim",
	} {
		if got := pinKeyForAddr(addr); got != want {
			t.Errorf("pinKeyForAddr(%q) = %q, want %q", addr, got, want)
		}
	}
	calls = 0
	for addr, want := range map[string]string{
		"rpi5.local:50051":      "rpi5.local",
		"192.168.2.253":         "192.168.2.253",
		"192.168.2.253:50051":   "192.168.2.253",
		"[fe80::1%en0]:50051":   "fe80::1%en0",
		"wendyos-thor.local:99": "wendyos-thor.local",
	} {
		if got := pinKeyForAddr(addr); got != want {
			t.Errorf("non-loopback pinKeyForAddr(%q) = %q, want %q (must be unchanged)", addr, got, want)
		}
	}
	if calls != 0 {
		t.Errorf("the VM store was consulted %d times for non-loopback addresses", calls)
	}
	for _, addr := range []string{"127.0.0.1:50051", "localhost:50051", "127.0.0.1:50151"} {
		if pinKeyForAddr(addr) == "" {
			t.Errorf("pinKeyForAddr(%q) is empty; that would disarm the downgrade guard", addr)
		}
	}
}

func TestRunningVMOnLoopbackPort(t *testing.T) {
	orig := vmStatusesFn
	t.Cleanup(func() { vmStatusesFn = orig })
	vmStatusesFn = func() ([]vm.Status, error) {
		return []vm.Status{
			{Name: "dev", Exists: true, Running: true, State: vm.State{Name: "dev", AgentPort: 50151, NetMode: vm.NetUser}},
			{Name: "stopped", Exists: true, Meta: vm.Meta{Name: "stopped", AgentPort: 50051}},
			{Name: "bridged", Exists: true, Running: true, State: vm.State{Name: "bridged", AgentPort: 50061, NetMode: vm.NetShared}},
		}, nil
	}
	for port, want := range map[int]string{50151: "dev", 50152: "dev", 50051: "", 50061: "", 50153: ""} {
		got, ok := runningVMOnLoopbackPort(port)
		if got != want || ok != (want != "") {
			t.Errorf("runningVMOnLoopbackPort(%d) = (%q, %v), want %q", port, got, ok, want)
		}
	}
	vmStatusesFn = func() ([]vm.Status, error) {
		return []vm.Status{
			{Name: "a", Exists: true, Running: true, State: vm.State{Name: "a", AgentPort: 50171, NetMode: vm.NetUser}},
			{Name: "b", Exists: true, Running: true, State: vm.State{Name: "b", AgentPort: 50171, NetMode: vm.NetUser}},
		}, nil
	}
	if name, ok := runningVMOnLoopbackPort(50171); ok {
		t.Errorf("two run records claim port 50171; picked %q instead of trusting neither", name)
	}
	vmStatusesFn = func() ([]vm.Status, error) { return nil, errors.New("store unreadable") }
	if _, ok := runningVMOnLoopbackPort(50151); ok {
		t.Error("an unreadable VM store matched a VM")
	}
}

func TestPinCandidateKeysAddsTheLegacyBareLoopbackHost(t *testing.T) {
	setPinCache(t)
	for key, want := range map[string][]string{
		"127.0.0.1:50051": {"127.0.0.1:50051", "127.0.0.1"},
		"[::1]:50051":     {"[::1]:50051", "::1"},
		"vm:dev":          {"vm:dev"},
		"rpi5.local":      {"rpi5.local"},
	} {
		if got := pinCandidateKeys(key); !reflect.DeepEqual(got, want) {
			t.Errorf("pinCandidateKeys(%q) = %q, want %q", key, got, want)
		}
	}
}

// Migration: a pin an older CLI filed under bare "127.0.0.1" keeps governing a
// loopback endpoint after the upgrade — the upgrade must not turn it into a
// first use.
func TestLegacyBareLoopbackPinStillGovernsItsEndpoint(t *testing.T) {
	setPinCache(t)
	stubLoopbackVMs(t, nil)
	setPinConfig(t, map[string]config.DevicePin{"127.0.0.1": pinA})
	target := newDialTarget(pinKeyForAddr("127.0.0.1:50051"), "127.0.0.1:50051")
	if target.PinKey != "127.0.0.1:50051" {
		t.Fatalf("PinKey = %q, want 127.0.0.1:50051", target.PinKey)
	}
	if !target.pinned() || target.PinnedKey != "127.0.0.1" || target.Expected == nil || target.Expected.EntityID != "42" {
		t.Fatalf("legacy pin no longer governs the endpoint: %+v", target)
	}
}

func TestEndpointPinOutranksTheLegacyBareLoopbackPin(t *testing.T) {
	setPinCache(t)
	stubLoopbackVMs(t, nil)
	setPinConfig(t, map[string]config.DevicePin{"127.0.0.1": pinA, "127.0.0.1:50061": pinB})
	target := newDialTarget("127.0.0.1:50061", "127.0.0.1:50061")
	if target.PinnedKey != "127.0.0.1:50061" || target.Expected == nil || target.Expected.EntityID != "43" {
		t.Fatalf("target = %+v, want the endpoint's own pin (asset 43)", target)
	}
}

func TestTypedLoopbackAddressOfARunningVMUsesItsVMPin(t *testing.T) {
	setPinCache(t)
	stubLoopbackVMs(t, map[int]string{50151: "dev", 50161: "fresh"})
	setPinConfig(t, map[string]config.DevicePin{"127.0.0.1": pinA, "vm:dev": pinB})

	key := pinKeyForAddr("127.0.0.1:50151")
	target := newDialTarget(key, "127.0.0.1:50151")
	if key != "vm:dev" || target.PinnedKey != "vm:dev" || target.Expected == nil || target.Expected.EntityID != "43" {
		t.Fatalf("typed address of VM dev: key %q target %+v, want vm:dev's pin", key, target)
	}
	// A VM with no pin of its own does not inherit another VM's localhost pin:
	// this is the collision that blocked a second VM.
	fresh := newDialTarget(pinKeyForAddr("127.0.0.1:50161"), "127.0.0.1:50161")
	if fresh.pinned() {
		t.Fatalf("fresh VM inherited the bare 127.0.0.1 pin: %+v", fresh)
	}
}

func TestEnforceDeviceIdentityMovesALegacyLoopbackPinToItsEndpoint(t *testing.T) {
	stubNonInteractive(t)
	readPins := writePinTestConfig(t, map[string]config.DevicePin{"127.0.0.1": pinA, "localhost": pinA})
	if err := enforceDeviceIdentity("127.0.0.1:50051", observedDeviceIdentity{mTLS: true, orgID: 7, assetID: "42"}); err != nil {
		t.Fatalf("the device the legacy pin names was refused: %v", err)
	}
	pins := readPins()
	if pins["127.0.0.1:50051"].AssetID != "42" {
		t.Fatalf("endpoint pin = %+v, want asset 42 moved onto it", pins["127.0.0.1:50051"])
	}
	for _, legacy := range []string{"127.0.0.1", "localhost"} {
		if _, ok := pins[legacy]; ok {
			t.Errorf("legacy pin %q naming the same device survived the move", legacy)
		}
	}
}

func TestEnforceDeviceIdentityRefusesAMismatchUnderALegacyLoopbackPin(t *testing.T) {
	stubNonInteractive(t)
	readPins := writePinTestConfig(t, map[string]config.DevicePin{"127.0.0.1": pinA})
	err := enforceDeviceIdentity("127.0.0.1:50051", observedDeviceIdentity{mTLS: true, orgID: 7, assetID: "43"})
	if !errors.Is(err, errDeviceIdentityRefused) {
		t.Fatalf("a different device at a legacy-pinned endpoint: got %v, want a refusal", err)
	}
	if !strings.Contains(err.Error(), "wendy device unpin 127.0.0.1\n") {
		t.Errorf("refusal must name the key the governing pin is filed under:\n%s", err)
	}
	pins := readPins()
	if _, ok := pins["127.0.0.1:50051"]; ok {
		t.Error("a refused device was pinned under the endpoint")
	}
	if pins["127.0.0.1"].AssetID != "42" {
		t.Error("the legacy pin was changed by a refusal")
	}

	unprovisioned := enforceDeviceIdentity("127.0.0.1:50051", observedDeviceIdentity{})
	if !errors.Is(unprovisioned, errDeviceIdentityRefused) {
		t.Fatalf("an unprovisioned answer at a legacy-pinned endpoint: got %v, want a refusal", unprovisioned)
	}
}

func TestEnforceDeviceIdentityAdoptsAnOrgOnlyLegacyLoopbackPin(t *testing.T) {
	stubNonInteractive(t)
	readPins := writePinTestConfig(t, map[string]config.DevicePin{"127.0.0.1": {OrgID: 7, CloudGRPC: "grpc.a.sh:443"}})
	if err := enforceDeviceIdentity("127.0.0.1:50051", observedDeviceIdentity{mTLS: true, orgID: 7, assetID: "42"}); err != nil {
		t.Fatal(err)
	}
	pins := readPins()
	if pins["127.0.0.1:50051"].AssetID != "42" {
		t.Fatalf("endpoint pin = %+v, want the observed asset adopted", pins["127.0.0.1:50051"])
	}
	if _, ok := pins["127.0.0.1"]; ok {
		t.Error("the adopted legacy pin was left behind")
	}
}

func TestEnforceDeviceIdentityRetiresALegacyPinOnlyForTheSameVM(t *testing.T) {
	stubNonInteractive(t)
	readPins := writePinTestConfig(t, map[string]config.DevicePin{"127.0.0.1": pinA})
	if err := enforceDeviceIdentity("vm:dev", observedDeviceIdentity{mTLS: true, orgID: 7, assetID: "42"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := readPins()["127.0.0.1"]; ok {
		t.Error("a bare 127.0.0.1 pin naming vm:dev's own identity was not retired")
	}

	readPins = writePinTestConfig(t, map[string]config.DevicePin{"127.0.0.1": pinB})
	if err := enforceDeviceIdentity("vm:dev", observedDeviceIdentity{mTLS: true, orgID: 7, assetID: "42"}); err != nil {
		t.Fatalf("vm:dev refused over another device's bare localhost pin: %v", err)
	}
	pins := readPins()
	if pins["vm:dev"].AssetID != "42" || pins["127.0.0.1"].AssetID != "43" {
		t.Fatalf("pins = %+v: want vm:dev pinned and the unrelated bare pin kept", pins)
	}
}

// Unpinning one loopback endpoint must not drop another endpoint's pin — the
// old `wendy device unpin 127.0.0.1` dropped every VM's at once.
func TestUnpinLoopbackEndpointsIndependently(t *testing.T) {
	stubLoopbackVMs(t, nil)
	setPinCache(t)
	readPins := writePinTestConfig(t, map[string]config.DevicePin{"127.0.0.1": pinA, "127.0.0.1:50061": pinB})
	runUnpin(t, "127.0.0.1:50061")
	pins := readPins()
	if _, ok := pins["127.0.0.1:50061"]; ok {
		t.Error("unpin 127.0.0.1:50061 left its pin")
	}
	if pins["127.0.0.1"].AssetID != "42" {
		t.Error("unpin 127.0.0.1:50061 dropped a different device's bare pin")
	}

	readPins = writePinTestConfig(t, map[string]config.DevicePin{"127.0.0.1": pinA})
	runUnpin(t, "127.0.0.1:50051")
	if _, ok := readPins()["127.0.0.1"]; ok {
		t.Error("unpin of an endpoint left the legacy pin that governs it")
	}
}

func TestSetDefaultOnBareLoopbackClearsTheEndpointPin(t *testing.T) {
	restoreDeviceGlobals(t)
	stubNonInteractive(t)
	stubLoopbackVMs(t, nil)
	setPinCache(t)
	readPins := writePinTestConfig(t, map[string]config.DevicePin{"127.0.0.1:50051": pinA, "127.0.0.1:50061": pinB})
	origLookup, origBrowse, origLadder, origDiscover := osLookupHostFn, lanBrowseFn, dialAgentLadderFn, discoverLANDevices
	osLookupHostFn = func(context.Context, string) ([]string, error) { return nil, errors.New("no resolver in test") }
	lanBrowseFn = func(context.Context, time.Duration) ([]models.LANDevice, error) { return nil, nil }
	dialAgentLadderFn = func(context.Context, dialTarget) (*grpcclient.AgentConnection, error, error) {
		return nil, nil, errors.New("device offline in test")
	}
	// A literal IP skips name resolution, so the connect reaches the
	// provisioned-mTLS hint's LAN browse; keep that off the network too.
	discoverLANDevices = func(context.Context, time.Duration) ([]models.LANDevice, error) { return nil, nil }
	t.Cleanup(func() {
		osLookupHostFn, lanBrowseFn, dialAgentLadderFn, discoverLANDevices = origLookup, origBrowse, origLadder, origDiscover
	})

	cmd := newDeviceSetDefaultCmd()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd.SetContext(ctx)
	if err := cmd.RunE(cmd, []string{"127.0.0.1"}); err != nil {
		t.Fatal(err)
	}
	pins := readPins()
	if _, ok := pins["127.0.0.1:50051"]; ok {
		t.Error("set-default 127.0.0.1 did not clear the pin its dial (127.0.0.1:50051) is checked under")
	}
	if _, ok := pins["127.0.0.1:50061"]; !ok {
		t.Error("set-default 127.0.0.1 cleared another endpoint's pin")
	}
}

func TestVMPrintReachabilityNamesTheVMAlias(t *testing.T) {
	var buf bytes.Buffer
	vmPrintReachability(&buf, "dev", vm.NetConfig{Mode: vm.NetUser}, 50151)
	out := buf.String()
	if !strings.Contains(out, "wendy --device vm:dev device info") {
		t.Errorf("vm start output does not suggest --device vm:dev:\n%s", out)
	}
	if strings.Contains(out, "--device 127.0.0.1") {
		t.Errorf("vm start still suggests a loopback address:\n%s", out)
	}
	if !strings.Contains(out, "127.0.0.1:50151") {
		t.Errorf("vm start no longer says where the agent is forwarded:\n%s", out)
	}
}
