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
	"github.com/wendylabsinc/wendy/go/internal/shared/discovery"
	"github.com/wendylabsinc/wendy/go/internal/shared/models"
	"github.com/wendylabsinc/wendy/go/proto/gen/agentpb"
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
		// Only the IPv4 address QEMU's forward binds names the VM: localhost
		// may resolve to ::1 first, and ::1 / 127.0.0.x / an IPv4-mapped
		// address are other sockets that something else can answer on.
		"localhost:50151":          "localhost:50151",
		"[::1]:50151":              "[::1]:50151",
		"127.0.0.2:50151":          "127.0.0.2:50151",
		"[::ffff:127.0.0.1]:50151": "[::ffff:127.0.0.1]:50151",
		// Only the literal text 127.0.0.1 is the forward: a trailing dot is
		// looked up as a DNS name, and padding is not an address at all.
		"127.0.0.1.:50151": "127.0.0.1:50151",
		" 127.0.0.1:50151": "127.0.0.1:50151",
		"127.0.0.1:50051":  "127.0.0.1:50051",
		"localhost:50051":  "localhost:50051",
		"LOCALHOST.:50051": "localhost:50051",
		"[::1]:50051":      "[::1]:50051",
		"127.0.0.1":        "127.0.0.1",
		"vm:dev":           "vm:dev",
		"sim":              "vm:sim",
	} {
		if got := pinKeyForAddr(addr); got != want {
			t.Errorf("pinKeyForAddr(%q) = %q, want %q", addr, got, want)
		}
	}
	calls = 0
	for _, addr := range []string{"localhost:50151", "[::1]:50151", "127.0.0.2:50151", "127.0.0.1.:50151", " 127.0.0.1:50151"} {
		pinKeyForAddr(addr)
	}
	if calls != 0 {
		t.Errorf("the VM store was consulted %d times for loopback spellings QEMU's forward never answers", calls)
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
	if _, ok := pins["127.0.0.1"]; ok {
		t.Error("the legacy 127.0.0.1 pin survived its move to 127.0.0.1:50051")
	}
	// A connection at 127.0.0.1 proves nothing about localhost, which can
	// resolve to ::1 — another socket, which something else can listen on.
	if pins["localhost"].AssetID != "42" {
		t.Error("a 127.0.0.1 connection retired the localhost pin, leaving localhost:50051 unpinned")
	}
}

// A connection retires only the bare pin of the host it reached, so a legacy
// pin under another loopback spelling keeps governing that spelling's
// endpoints on the same port.
func TestAConnectionRetiresOnlyItsOwnHostsBarePin(t *testing.T) {
	stubNonInteractive(t)
	setPinCache(t)
	stubLoopbackVMs(t, map[int]string{50051: "dev"})
	readPins := writePinTestConfig(t, map[string]config.DevicePin{"127.0.0.1": pinA, "localhost": pinA, "::1": pinA})
	if err := connectAsVMAt("127.0.0.1:50051", observedDeviceIdentity{mTLS: true, orgID: 7, assetID: "42"}); err != nil {
		t.Fatal(err)
	}
	pins := readPins()
	if _, ok := pins["127.0.0.1"]; ok {
		t.Error("the VM's own bare 127.0.0.1 pin was not retired")
	}
	for _, other := range []string{"localhost", "::1"} {
		if pins[other].AssetID != "42" {
			t.Errorf("the %s pin was retired by a connection that never reached %s", other, other)
		}
	}
	for _, addr := range []string{"localhost:50051", "[::1]:50051"} {
		key := pinKeyForAddr(addr)
		if target := newDialTarget(key, addr); !target.pinned() || target.Expected == nil || target.Expected.EntityID != "42" {
			t.Errorf("%s is no longer governed by its legacy pin: %+v", addr, target)
		}
		if err := enforceDeviceIdentity(key, observedDeviceIdentity{mTLS: true, orgID: 7, assetID: "99"}); !errors.Is(err, errDeviceIdentityRefused) {
			t.Errorf("%s: asset 99 got %v, want a refusal", addr, err)
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
	// An org-only pin names no device, so this device cannot show it is the
	// one the pin protects elsewhere: it keeps governing every other port.
	if pin, ok := pins["127.0.0.1"]; !ok || pin.OrgID != 7 || pin.AssetID != "" {
		t.Errorf("the org-only legacy pin = %+v (present %v), want it kept as it was", pin, ok)
	}
}

// R19, the reviewer's sequence: adopting an org-only legacy pin at one port
// must not leave another port unpinned.
func TestAdoptingAnOrgOnlyLegacyPinKeepsOtherPortsPinned(t *testing.T) {
	stubNonInteractive(t)
	setPinCache(t)
	stubLoopbackVMs(t, nil)
	readPins := writePinTestConfig(t, map[string]config.DevicePin{"127.0.0.1": {OrgID: 7, CloudGRPC: "grpc.a.sh:443"}})
	if err := enforceDeviceIdentity("127.0.0.1:50061", observedDeviceIdentity{mTLS: true, orgID: 7, assetID: "43"}); err != nil {
		t.Fatal(err)
	}
	pins := readPins()
	if pins["127.0.0.1:50061"].AssetID != "43" {
		t.Fatalf("pins = %+v, want 127.0.0.1:50061 pinned to asset 43", pins)
	}
	if _, ok := pins["127.0.0.1"]; !ok {
		t.Fatal("the org-only bare pin was cleared by a device at another port")
	}
	if target := newDialTarget(pinKeyForAddr("127.0.0.1:50051"), "127.0.0.1:50051"); !target.pinned() {
		t.Fatalf("127.0.0.1:50051 lost its pin, so the plaintext rung is open: %+v", target)
	}
	if err := enforceDeviceIdentity("127.0.0.1:50051", observedDeviceIdentity{}); !errors.Is(err, errDeviceIdentityRefused) {
		t.Fatalf("an unprovisioned answer at 127.0.0.1:50051: got %v, want a refusal", err)
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
	// provisioned-mTLS hint's LAN browse and, once the dial fails, the
	// USB-direct fallback; keep both off the network too.
	discoverLANDevices = func(context.Context, time.Duration) ([]models.LANDevice, error) { return nil, nil }
	origUSB := usbDirectCandidatesFn
	usbDirectCandidatesFn = func() []discovery.USBDirectCandidate { return nil }
	t.Cleanup(func() {
		osLookupHostFn, lanBrowseFn, dialAgentLadderFn, discoverLANDevices = origLookup, origBrowse, origLadder, origDiscover
		usbDirectCandidatesFn = origUSB
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

// I2: localhost resolves to ::1 as well as 127.0.0.1, and the dial ladder
// moves on to the next address when one fails, so a VM's forward on
// 127.0.0.1 does not make localhost:PORT that VM. A legacy localhost pin keeps
// governing it, and a different device answering there is refused.
func TestLocalhostOnAVMPortStaysUnderItsLegacyPin(t *testing.T) {
	stubNonInteractive(t)
	setPinCache(t)
	stubLoopbackVMs(t, map[int]string{50151: "dev"})
	readPins := writePinTestConfig(t, map[string]config.DevicePin{"localhost": pinA, "::1": pinA})

	for _, addr := range []string{"localhost:50151", "[::1]:50151"} {
		key := pinKeyForAddr(addr)
		if strings.HasPrefix(key, vmDeviceIDPrefix) {
			t.Fatalf("pinKeyForAddr(%q) = %q: only 127.0.0.1 is the VM's forward", addr, key)
		}
		target := newDialTarget(key, addr)
		if !target.pinned() || target.Expected == nil || target.Expected.EntityID != "42" {
			t.Fatalf("%s: dial not governed by the legacy bare pin: %+v", addr, target)
		}
		err := enforceDeviceIdentity(key, observedDeviceIdentity{mTLS: true, orgID: 7, assetID: "99"})
		if !errors.Is(err, errDeviceIdentityRefused) {
			t.Fatalf("%s: asset 99 under a legacy pin for 42: got %v, want a refusal", addr, err)
		}
	}
	pins := readPins()
	if _, ok := pins["vm:dev"]; ok {
		t.Error("a refused device was pinned as vm:dev")
	}
	if pins["localhost"].AssetID != "42" || pins["::1"].AssetID != "42" {
		t.Errorf("legacy pins changed by a refusal: %+v", pins)
	}
}

// I3: the endpoint key is built from the normalised host, so a pin moved from
// a legacy bare host under one spelling is found under every other.
func TestLoopbackEndpointKeyIgnoresHostSpelling(t *testing.T) {
	stubNonInteractive(t)
	setPinCache(t)
	stubLoopbackVMs(t, nil)
	if a, b := pinKeyForAddr("LOCALHOST.:50051"), pinKeyForAddr("localhost:50051"); a != b {
		t.Fatalf("pinKeyForAddr: %q vs %q for the same endpoint", a, b)
	}
	readPins := writePinTestConfig(t, map[string]config.DevicePin{"localhost": pinA})
	for _, addr := range []string{"LOCALHOST.:50051", "localhost:50051"} {
		target := newDialTarget(pinKeyForAddr(addr), addr)
		if target.PinnedKey != "localhost" || target.Expected == nil || target.Expected.EntityID != "42" {
			t.Fatalf("%s: legacy localhost pin does not govern: %+v", addr, target)
		}
	}
	if err := enforceDeviceIdentity(pinKeyForAddr("LOCALHOST.:50051"), observedDeviceIdentity{mTLS: true, orgID: 7, assetID: "42"}); err != nil {
		t.Fatal(err)
	}
	if pins := readPins(); pins["localhost:50051"].AssetID != "42" {
		t.Fatalf("pins = %+v, want the legacy pin moved to localhost:50051", pins)
	}
	target := newDialTarget(pinKeyForAddr("localhost:50051"), "localhost:50051")
	if target.PinnedKey != "localhost:50051" || target.Expected == nil || target.Expected.EntityID != "42" {
		t.Fatalf("the moved pin is orphaned from the plain spelling: %+v", target)
	}
}

// connectAsVMAt runs the enforcement a connection to a typed loopback address
// gets: keyed by pinKeyForAddr, and told which endpoint it was dialled at.
func connectAsVMAt(addr string, obs observedDeviceIdentity) error {
	key := pinKeyForAddr(addr)
	return enforceDeviceIdentityAt(key, vmEndpointPinKey(key, addr), obs)
}

// I1: 127.0.0.1:PORT is keyed as the VM only while the VM runs. Once it stops,
// the same address is keyed per endpoint, so the endpoint must carry the VM's
// identity too — or anything that binds the port then is a first use, where
// the bare 127.0.0.1 key used to refuse it.
func TestVMEndpointStaysPinnedWhileItsVMIsStopped(t *testing.T) {
	for _, tc := range []struct {
		name string
		pins map[string]config.DevicePin
	}{
		{"legacy bare pin", map[string]config.DevicePin{"127.0.0.1": pinA}},
		{"fresh user", map[string]config.DevicePin{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stubNonInteractive(t)
			setPinCache(t)
			readPins := writePinTestConfig(t, tc.pins)
			const addr = "127.0.0.1:50051"

			stubLoopbackVMs(t, map[int]string{50051: "dev"})
			if err := connectAsVMAt(addr, observedDeviceIdentity{mTLS: true, orgID: 7, assetID: "42"}); err != nil {
				t.Fatalf("the running VM was refused: %v", err)
			}
			pins := readPins()
			if pins["vm:dev"].AssetID != "42" || pins[addr].AssetID != "42" {
				t.Fatalf("pins = %+v, want vm:dev and %s both naming asset 42", pins, addr)
			}
			if _, ok := pins["127.0.0.1"]; ok {
				t.Error("the same-identity bare pin was not retired")
			}

			stubLoopbackVMs(t, nil) // the VM stops
			key := pinKeyForAddr(addr)
			if key != addr {
				t.Fatalf("pinKeyForAddr(%q) = %q with no VM running", addr, key)
			}
			target := newDialTarget(key, addr)
			if !target.pinned() || target.Expected == nil || target.Expected.EntityID != "42" {
				t.Fatalf("stopped VM's endpoint lost its pin, so the plaintext rung is open: %+v", target)
			}
			if err := enforceDeviceIdentity(key, observedDeviceIdentity{mTLS: true, orgID: 7, assetID: "99"}); !errors.Is(err, errDeviceIdentityRefused) {
				t.Fatalf("asset 99 on the stopped VM's endpoint: got %v, want a refusal", err)
			}
			if err := enforceDeviceIdentity(key, observedDeviceIdentity{}); !errors.Is(err, errDeviceIdentityRefused) {
				t.Fatalf("an unprovisioned answer on the stopped VM's endpoint: got %v, want a refusal", err)
			}
		})
	}
}

func TestVMEndpointPinNeverOverwritesTheEndpointsOwnPin(t *testing.T) {
	stubNonInteractive(t)
	setPinCache(t)
	stubLoopbackVMs(t, map[int]string{50051: "dev"})
	readPins := writePinTestConfig(t, map[string]config.DevicePin{"127.0.0.1:50051": pinB})
	if err := connectAsVMAt("127.0.0.1:50051", observedDeviceIdentity{mTLS: true, orgID: 7, assetID: "42"}); err != nil {
		t.Fatalf("vm:dev refused over its endpoint's own pin for another device: %v", err)
	}
	pins := readPins()
	if pins["vm:dev"].AssetID != "42" {
		t.Errorf("vm:dev = %+v, want asset 42", pins["vm:dev"])
	}
	if pins["127.0.0.1:50051"].AssetID != "43" {
		t.Errorf("the endpoint's own pin was overwritten: %+v", pins["127.0.0.1:50051"])
	}
}

func TestVMEndpointPinRetiresOnlySameIdentityBarePins(t *testing.T) {
	stubNonInteractive(t)
	setPinCache(t)
	stubLoopbackVMs(t, map[int]string{50051: "dev"})
	readPins := writePinTestConfig(t, map[string]config.DevicePin{"127.0.0.1": pinA, "localhost": pinB})
	if err := connectAsVMAt("127.0.0.1:50051", observedDeviceIdentity{mTLS: true, orgID: 7, assetID: "42"}); err != nil {
		t.Fatal(err)
	}
	pins := readPins()
	if _, ok := pins["127.0.0.1"]; ok {
		t.Error("the bare pin naming the VM's identity survived")
	}
	if pins["localhost"].AssetID != "43" {
		t.Error("a bare pin naming another device was retired")
	}
	if pins["127.0.0.1:50051"].AssetID != "42" {
		t.Errorf("endpoint pin = %+v, want asset 42", pins["127.0.0.1:50051"])
	}
}

// A VM reached some other way than the IPv4 forward — or with no dialled
// endpoint at all — pins nothing but vm:<name>.
func TestVMEndpointKeyIsOnlyTheIPv4Forward(t *testing.T) {
	for _, tc := range []struct{ key, addr, want string }{
		{"vm:dev", "127.0.0.1:50051", "127.0.0.1:50051"},
		{"vm:dev", "127.0.0.1.:50051", ""},
		{"vm:dev", " 127.0.0.1:50051", ""},
		{"vm:dev", "localhost:50051", ""},
		{"vm:dev", "[::1]:50051", ""},
		{"vm:dev", "127.0.0.2:50051", ""},
		{"vm:dev", "", ""},
		{"127.0.0.1:50051", "127.0.0.1:50051", ""},
		{"rpi5.local", "127.0.0.1:50051", ""},
	} {
		if got := vmEndpointPinKey(tc.key, tc.addr); got != tc.want {
			t.Errorf("vmEndpointPinKey(%q, %q) = %q, want %q", tc.key, tc.addr, got, tc.want)
		}
	}
}

// R15: a certificate with no asset id proves an organisation, not a device. It
// may pass a legacy pin, but it must not move it, retire bare pins, or pin a
// VM's endpoint — or a same-org device would clear the way for another.
func TestAnAssetlessMatchLeavesLegacyLoopbackPinsInPlace(t *testing.T) {
	stubNonInteractive(t)
	setPinCache(t)
	stubLoopbackVMs(t, nil)
	legacy := map[string]config.DevicePin{"127.0.0.1": pinA, "localhost": pinA}
	readPins := writePinTestConfig(t, legacy)
	if err := enforceDeviceIdentity("127.0.0.1:50071", observedDeviceIdentity{mTLS: true, orgID: 7}); err != nil {
		t.Fatalf("an asset-less same-org answer under a legacy pin: %v", err)
	}
	if pins := readPins(); !reflect.DeepEqual(pins, legacy) {
		t.Fatalf("pins = %+v, want the legacy pins untouched", pins)
	}
	if err := enforceDeviceIdentity("127.0.0.1:50051", observedDeviceIdentity{mTLS: true, orgID: 7, assetID: "43"}); !errors.Is(err, errDeviceIdentityRefused) {
		t.Fatalf("asset 43 after an asset-less match elsewhere: got %v, want a refusal", err)
	}

	readPins = writePinTestConfig(t, map[string]config.DevicePin{"127.0.0.1": pinA})
	if err := enforceDeviceIdentityAt("vm:dev", "127.0.0.1:50051", observedDeviceIdentity{mTLS: true, orgID: 7}); err != nil {
		t.Fatal(err)
	}
	pins := readPins()
	if _, ok := pins["127.0.0.1:50051"]; ok {
		t.Error("an asset-less VM answer pinned its endpoint")
	}
	if pins["127.0.0.1"].AssetID != "42" {
		t.Error("an asset-less VM answer retired a bare pin")
	}
}

// The endpoint reaches the pin check from every front door a VM's forwarded
// address can be dialled through.
func TestEveryVMFrontDoorPinsItsEndpoint(t *testing.T) {
	for _, tc := range []struct {
		name    string
		connect func(ctx context.Context) error
	}{
		{"vm alias", func(ctx context.Context) error {
			conn, _, err := connectSimulatorAgent(ctx, "dev", "127.0.0.1:50151")
			if conn != nil {
				conn.Close()
			}
			return err
		}},
		{"connectToAgent", func(ctx context.Context) error {
			deviceFlag = "127.0.0.1:50151"
			conn, err := connectToAgent(ctx, SuppressProvisioningHint(), SuppressUpdateCheck(), NonInteractive(), DisableSessionBroker())
			if conn != nil {
				conn.Close()
			}
			return err
		}},
		{"resolveTarget", func(ctx context.Context) error {
			deviceFlag = "127.0.0.1:50151"
			sel, err := resolveTarget(ctx, SuppressProvisioningHint(), SuppressUpdateCheck(), NonInteractive(), DisableSessionBroker())
			if sel != nil {
				sel.Close()
			}
			return err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			restoreDeviceGlobals(t)
			stubNonInteractive(t)
			setPinCache(t)
			stubLoopbackVMs(t, map[int]string{50151: "dev"})
			readPins := writePinTestConfig(t, map[string]config.DevicePin{})

			origLadder, origObserve, origDiscover := dialAgentLadderFn, observeDeviceIdentityFn, discoverLANDevices
			dialAgentLadderFn = func(_ context.Context, target dialTarget) (*grpcclient.AgentConnection, error, error) {
				return &grpcclient.AgentConnection{Host: "127.0.0.1", Addr: target.Addr,
					AgentService: &fakeAgentVersionClient{resp: &agentpb.GetAgentVersionResponse{}}}, nil, nil
			}
			observeDeviceIdentityFn = func(*grpcclient.AgentConnection) observedDeviceIdentity {
				return observedDeviceIdentity{mTLS: true, orgID: 7, assetID: "42"}
			}
			discoverLANDevices = func(context.Context, time.Duration) ([]models.LANDevice, error) { return nil, nil }
			t.Cleanup(func() {
				dialAgentLadderFn, observeDeviceIdentityFn, discoverLANDevices = origLadder, origObserve, origDiscover
			})

			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := tc.connect(ctx); err != nil {
				t.Fatal(err)
			}
			pins := readPins()
			if pins["vm:dev"].AssetID != "42" || pins["127.0.0.1:50151"].AssetID != "42" {
				t.Fatalf("pins = %+v, want vm:dev and 127.0.0.1:50151 both naming asset 42", pins)
			}
		})
	}
}

// R17, OPEN 1: an mTLS VM whose certificate names no asset still pins its
// endpoint when nothing else governs it, so an unprovisioned answer there is
// refused once the VM stops — as the bare 127.0.0.1 key refused it.
func TestAnAssetlessVMStillPinsItsEndpoint(t *testing.T) {
	stubNonInteractive(t)
	setPinCache(t)
	readPins := writePinTestConfig(t, map[string]config.DevicePin{})
	const addr = "127.0.0.1:50051"
	stubLoopbackVMs(t, map[int]string{50051: "dev"})
	if err := connectAsVMAt(addr, observedDeviceIdentity{mTLS: true, orgID: 7}); err != nil {
		t.Fatal(err)
	}
	pins := readPins()
	if pins["vm:dev"].OrgID != 7 || pins[addr].OrgID != 7 {
		t.Fatalf("pins = %+v, want vm:dev and %s both pinned to org 7", pins, addr)
	}
	stubLoopbackVMs(t, nil) // the VM stops
	if target := newDialTarget(pinKeyForAddr(addr), addr); !target.pinned() {
		t.Fatalf("the stopped VM's endpoint is unpinned: %+v", target)
	}
	if err := enforceDeviceIdentity(pinKeyForAddr(addr), observedDeviceIdentity{}); !errors.Is(err, errDeviceIdentityRefused) {
		t.Fatalf("an unprovisioned answer on the stopped VM's endpoint: got %v, want a refusal", err)
	}
}

// R17, OPEN 2: an identity only ever verified under vm:<name> must not become
// the endpoint's own pin where a legacy pin governs that endpoint — it would
// outrank the pin the old CLI applied there.
func TestVMEndpointIsNotFiledWhereALegacyPinGoverns(t *testing.T) {
	stubNonInteractive(t)
	setPinCache(t)
	readPins := writePinTestConfig(t, map[string]config.DevicePin{"127.0.0.1": pinA})
	const addr = "127.0.0.1:50061"
	stubLoopbackVMs(t, map[int]string{50061: "dev2"})
	if err := connectAsVMAt(addr, observedDeviceIdentity{mTLS: true, orgID: 7, assetID: "99"}); err != nil {
		t.Fatalf("vm:dev2 is judged under its own key: %v", err)
	}
	pins := readPins()
	if _, ok := pins[addr]; ok {
		t.Fatalf("pins = %+v: the endpoint got its own pin over the legacy pin that governs it", pins)
	}
	if pins["vm:dev2"].AssetID != "99" || pins["127.0.0.1"].AssetID != "42" {
		t.Fatalf("pins = %+v, want vm:dev2=99 and the bare 42 kept", pins)
	}
	stubLoopbackVMs(t, nil) // the VM stops
	target := newDialTarget(pinKeyForAddr(addr), addr)
	if target.PinnedKey != "127.0.0.1" || target.Expected == nil || target.Expected.EntityID != "42" {
		t.Fatalf("stopped VM's endpoint is not governed by the bare pin: %+v", target)
	}
	if err := enforceDeviceIdentity(pinKeyForAddr(addr), observedDeviceIdentity{mTLS: true, orgID: 7, assetID: "99"}); !errors.Is(err, errDeviceIdentityRefused) {
		t.Fatalf("asset 99 at %s under the bare 42 pin: got %v, want a refusal", addr, err)
	}
}

// R18, OPEN 3: a VM is never on USB. A typed address keyed as vm:<name> whose
// ladder fails must not fall back to a USB gadget that merely reports that
// name — it would be judged under a key that consults no loopback pin.
func TestNoUSBFallbackForAVMKey(t *testing.T) {
	for _, tc := range []struct {
		name string
		obs  observedDeviceIdentity
	}{
		{"same-org gadget", observedDeviceIdentity{mTLS: true, orgID: 7, assetID: "99"}},
		{"unprovisioned gadget", observedDeviceIdentity{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			restoreDeviceGlobals(t)
			stubNonInteractive(t)
			setPinCache(t)
			stubLoopbackVMs(t, map[int]string{50051: "dev"})
			legacy := map[string]config.DevicePin{"127.0.0.1": pinA}
			readPins := writePinTestConfig(t, legacy)

			origLadder, origObserve, origDiscover := dialAgentLadderFn, observeDeviceIdentityFn, discoverLANDevices
			origCands, origPreDial, origConnect := usbDirectCandidatesFn, usbDirectPreDialFn, usbDirectConnectFn
			dialAgentLadderFn = func(context.Context, dialTarget) (*grpcclient.AgentConnection, error, error) {
				return nil, nil, errors.New("VM agent still booting")
			}
			observeDeviceIdentityFn = func(*grpcclient.AgentConnection) observedDeviceIdentity { return tc.obs }
			discoverLANDevices = func(context.Context, time.Duration) ([]models.LANDevice, error) { return nil, nil }
			usbDirectCandidatesFn = func() []discovery.USBDirectCandidate {
				t.Error("the USB-direct fallback was consulted for a vm: key")
				return []discovery.USBDirectCandidate{{Interface: "gadget", Zone: "gadget"}}
			}
			usbDirectPreDialFn = func(context.Context, discovery.USBDirectCandidate) bool { return true }
			usbDirectConnectFn = func(context.Context, string) (*grpcclient.AgentConnection, error) {
				return &grpcclient.AgentConnection{Host: "fe80::5741:1", AgentService: &fakeAgentVersionClient{
					resp: &agentpb.GetAgentVersionResponse{Hostname: "vm:dev"}}}, nil
			}
			t.Cleanup(func() {
				dialAgentLadderFn, observeDeviceIdentityFn, discoverLANDevices = origLadder, origObserve, origDiscover
				usbDirectCandidatesFn, usbDirectPreDialFn, usbDirectConnectFn = origCands, origPreDial, origConnect
			})

			deviceFlag = "127.0.0.1:50051"
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			conn, err := connectToAgent(ctx, SuppressProvisioningHint(), SuppressUpdateCheck(), NonInteractive(), DisableSessionBroker())
			if conn != nil {
				conn.Close()
				t.Fatal("a USB gadget reporting vm:dev was accepted for 127.0.0.1:50051")
			}
			if err == nil {
				t.Fatal("no error for an unreachable VM")
			}
			if pins := readPins(); !reflect.DeepEqual(pins, legacy) {
				t.Fatalf("pins = %+v, want only the legacy pin", pins)
			}
		})
	}
}

// R18: a connection the USB-direct fallback substituted is not the dialled
// endpoint, so it can never be recorded as one.
func TestUSBFallbackConnectionIsNotTheDialledEndpoint(t *testing.T) {
	restoreDeviceGlobals(t)
	stubNonInteractive(t)
	setPinCache(t)
	writePinTestConfig(t, map[string]config.DevicePin{})
	origLookup, origBrowse, origLadder, origDiscover := osLookupHostFn, lanBrowseFn, dialAgentLadderFn, discoverLANDevices
	origCands, origPreDial, origConnect := usbDirectCandidatesFn, usbDirectPreDialFn, usbDirectConnectFn
	osLookupHostFn = func(context.Context, string) ([]string, error) { return nil, errors.New("no resolver in test") }
	lanBrowseFn = func(context.Context, time.Duration) ([]models.LANDevice, error) { return nil, nil }
	dialAgentLadderFn = func(context.Context, dialTarget) (*grpcclient.AgentConnection, error, error) {
		return nil, nil, errors.New("device offline in test")
	}
	discoverLANDevices = func(context.Context, time.Duration) ([]models.LANDevice, error) { return nil, nil }
	usbDirectCandidatesFn = func() []discovery.USBDirectCandidate {
		return []discovery.USBDirectCandidate{{Interface: "gadget", Zone: "gadget"}}
	}
	usbDirectPreDialFn = func(context.Context, discovery.USBDirectCandidate) bool { return true }
	usbDirectConnectFn = func(context.Context, string) (*grpcclient.AgentConnection, error) {
		return &grpcclient.AgentConnection{Host: "fe80::5741:1", AgentService: &fakeAgentVersionClient{
			resp: &agentpb.GetAgentVersionResponse{Hostname: "wendy-thor.local"}}}, nil
	}
	t.Cleanup(func() {
		osLookupHostFn, lanBrowseFn, dialAgentLadderFn, discoverLANDevices = origLookup, origBrowse, origLadder, origDiscover
		usbDirectCandidatesFn, usbDirectPreDialFn, usbDirectConnectFn = origCands, origPreDial, origConnect
	})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	conn, dialled, finished, err := connectToAgentDirect(ctx, resolveConfig{nonInteractive: true}, "wendy-thor", "wendy-thor.local:50051", false)
	if err != nil || finished || conn == nil {
		t.Fatalf("connectToAgentDirect = (%v, finished %v, %v), want the USB connection", conn, finished, err)
	}
	conn.Close()
	if dialled != "" {
		t.Fatalf("a USB-fallback connection reports dialled endpoint %q, want none", dialled)
	}
}
