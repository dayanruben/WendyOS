package commands

import (
	"bytes"
	"context"
	"errors"
	"net"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wendylabsinc/wendy/go/internal/cli/grpcclient"
	"github.com/wendylabsinc/wendy/go/internal/cli/vm"
	"github.com/wendylabsinc/wendy/go/internal/shared/config"
	"github.com/wendylabsinc/wendy/go/internal/shared/discovery"
	"github.com/wendylabsinc/wendy/go/internal/shared/discoverycache"
	"github.com/wendylabsinc/wendy/go/internal/shared/models"
	"github.com/wendylabsinc/wendy/go/proto/gen/agentpb"
)

// stubLoopbackVMs describes which running VM forwards which plaintext agent
// port on 127.0.0.1, for both views of the VM store the pin code reads.
func stubLoopbackVMs(t *testing.T, byPort map[int]string) {
	t.Helper()
	origName, origPort := loopbackVMNameFn, runningVMAgentPortFn
	loopbackVMNameFn = func(port int) (string, bool) {
		name, ok := byPort[port]
		return name, ok
	}
	runningVMAgentPortFn = func(name string) (int, bool) {
		for port, n := range byPort {
			if n == name {
				return port, true
			}
		}
		return 0, false
	}
	t.Cleanup(func() { loopbackVMNameFn, runningVMAgentPortFn = origName, origPort })
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
	// Only the plaintext agent port names the VM: a dial at AgentPort+1 also
	// tries AgentPort+2, which QEMU does not forward.
	for port, want := range map[int]string{50151: "dev", 50152: "", 50051: "", 50061: "", 50153: ""} {
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
	return enforceDeviceIdentityAt(key, vmEndpointPinKeys(key, addr), obs)
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
	stubLoopbackVMs(t, map[int]string{50051: "dev"})
	both := []string{"127.0.0.1:50051", "127.0.0.1:50052"}
	for _, tc := range []struct {
		key, addr string
		want      []string
	}{
		// Both ports QEMU forwards for the VM, from either of them.
		{"vm:dev", "127.0.0.1:50051", both},
		{"vm:dev", "127.0.0.1:50052", both},
		// A port the store says is not this VM's forward.
		{"vm:dev", "127.0.0.1:50061", nil},
		// A VM the store cannot place: only the address dialled.
		{"vm:other", "127.0.0.1:50071", []string{"127.0.0.1:50071"}},
		{"vm:dev", "127.0.0.1.:50051", nil},
		{"vm:dev", " 127.0.0.1:50051", nil},
		{"vm:dev", "localhost:50051", nil},
		{"vm:dev", "[::1]:50051", nil},
		{"vm:dev", "127.0.0.2:50051", nil},
		{"vm:dev", "", nil},
		{"127.0.0.1:50051", "127.0.0.1:50051", nil},
		{"rpi5.local", "127.0.0.1:50051", nil},
	} {
		if got := vmEndpointPinKeys(tc.key, tc.addr); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("vmEndpointPinKeys(%q, %q) = %q, want %q", tc.key, tc.addr, got, tc.want)
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
	if err := enforceDeviceIdentityAt("vm:dev", []string{"127.0.0.1:50051"}, observedDeviceIdentity{mTLS: true, orgID: 7}); err != nil {
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

// connectTypedLoopback drives the real connectToAgent path for a typed
// --device address, with the ladder answering as obs. It returns the dial
// target the ladder was handed and the connect error.
func connectTypedLoopback(t *testing.T, addr string, obs observedDeviceIdentity) (dialTarget, error) {
	t.Helper()
	var dialled dialTarget
	origLadder, origObserve, origDiscover := dialAgentLadderFn, observeDeviceIdentityFn, discoverLANDevices
	origUSB := usbDirectCandidatesFn
	dialAgentLadderFn = func(_ context.Context, target dialTarget) (*grpcclient.AgentConnection, error, error) {
		dialled = target
		return &grpcclient.AgentConnection{Host: "127.0.0.1", Addr: target.Addr,
			AgentService: &fakeAgentVersionClient{resp: &agentpb.GetAgentVersionResponse{}}}, nil, nil
	}
	observeDeviceIdentityFn = func(*grpcclient.AgentConnection) observedDeviceIdentity { return obs }
	discoverLANDevices = func(context.Context, time.Duration) ([]models.LANDevice, error) { return nil, nil }
	usbDirectCandidatesFn = func() []discovery.USBDirectCandidate { return nil }
	defer func() {
		dialAgentLadderFn, observeDeviceIdentityFn, discoverLANDevices = origLadder, origObserve, origDiscover
		usbDirectCandidatesFn = origUSB
	}()
	deviceFlag = addr
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	conn, err := connectToAgent(ctx, SuppressProvisioningHint(), SuppressUpdateCheck(), NonInteractive(), DisableSessionBroker())
	if conn != nil {
		conn.Close()
	}
	return dialled, err
}

// R21, B1: a VM that names its asset while an org-only legacy pin covers its
// endpoint gets that asset pinned at the endpoint (the org-only pin names no
// device, so it cannot block it) — otherwise, once the VM stops, the endpoint
// constrains only the organisation and any same-org device is adopted there.
func TestVMAssetIsPinnedAtItsEndpointOverAnOrgOnlyLegacyPin(t *testing.T) {
	restoreDeviceGlobals(t)
	stubNonInteractive(t)
	setPinCache(t)
	orgOnly := config.DevicePin{OrgID: 7, CloudGRPC: "grpc.a.sh:443"}
	readPins := writePinTestConfig(t, map[string]config.DevicePin{"127.0.0.1": orgOnly})
	const addr = "127.0.0.1:50051"

	stubLoopbackVMs(t, map[int]string{50051: "dev"})
	if _, err := connectTypedLoopback(t, addr, observedDeviceIdentity{mTLS: true, orgID: 7, assetID: "42"}); err != nil {
		t.Fatalf("the running VM was refused: %v", err)
	}
	pins := readPins()
	if pins["vm:dev"].AssetID != "42" || pins[addr].AssetID != "42" {
		t.Fatalf("pins = %+v, want vm:dev and %s both naming asset 42", pins, addr)
	}
	if pin, ok := pins["127.0.0.1"]; !ok || pin.AssetID != "" {
		t.Errorf("the org-only bare pin = %+v (present %v), want it kept (R19)", pin, ok)
	}

	stubLoopbackVMs(t, nil) // the VM stops
	target, err := connectTypedLoopback(t, addr, observedDeviceIdentity{mTLS: true, orgID: 7, assetID: "99"})
	if !errors.Is(err, errDeviceIdentityRefused) {
		t.Fatalf("asset 99 on the stopped VM's endpoint: got %v, want a refusal", err)
	}
	if target.Expected == nil || target.Expected.EntityID != "42" || !target.pinned() {
		t.Fatalf("dial target = %+v, want Expected asset 42 and the plaintext rung blocked", target)
	}
}

// R21, B1b: an endpoint pinned org-only by an asset-less VM answer is upgraded
// in place once the VM names its asset.
func TestVMAssetUpgradesItsOrgOnlyEndpointPin(t *testing.T) {
	restoreDeviceGlobals(t)
	stubNonInteractive(t)
	setPinCache(t)
	readPins := writePinTestConfig(t, map[string]config.DevicePin{})
	const addr = "127.0.0.1:50051"

	stubLoopbackVMs(t, map[int]string{50051: "dev"})
	if _, err := connectTypedLoopback(t, addr, observedDeviceIdentity{mTLS: true, orgID: 7}); err != nil {
		t.Fatal(err)
	}
	if pins := readPins(); pins[addr].OrgID != 7 || pins[addr].AssetID != "" {
		t.Fatalf("pins = %+v, want %s pinned org-only first", pins, addr)
	}
	if _, err := connectTypedLoopback(t, addr, observedDeviceIdentity{mTLS: true, orgID: 7, assetID: "42"}); err != nil {
		t.Fatal(err)
	}
	pins := readPins()
	if pins["vm:dev"].AssetID != "42" || pins[addr].AssetID != "42" {
		t.Fatalf("pins = %+v, want vm:dev and %s upgraded to asset 42", pins, addr)
	}
	if pins[addr].Source != config.PinSourceLAN {
		t.Errorf("endpoint pin source = %q, want it kept (%q)", pins[addr].Source, config.PinSourceLAN)
	}

	stubLoopbackVMs(t, nil) // the VM stops
	target, err := connectTypedLoopback(t, addr, observedDeviceIdentity{mTLS: true, orgID: 7, assetID: "99"})
	if !errors.Is(err, errDeviceIdentityRefused) {
		t.Fatalf("asset 99 on the stopped VM's endpoint: got %v, want a refusal", err)
	}
	if target.Expected == nil || target.Expected.EntityID != "42" {
		t.Fatalf("dial target = %+v, want Expected asset 42", target)
	}
}

// R22: only the VM's plaintext agent port is keyed as the VM. A dial at its
// mTLS forward (AgentPort+1) also tries AgentPort+2, which QEMU does not
// forward, so it is keyed per endpoint — and pinned to the VM's identity by
// any connection to the VM, so an answer from AgentPort+2 is refused.
func TestVMMTLSForwardIsKeyedAndPinnedAsAnEndpoint(t *testing.T) {
	stubNonInteractive(t)
	setPinCache(t)
	stubLoopbackVMs(t, map[int]string{50151: "dev"})
	readPins := writePinTestConfig(t, map[string]config.DevicePin{"vm:dev": pinA})
	const mtls = "127.0.0.1:50152"
	if key := pinKeyForAddr(mtls); key != mtls {
		t.Fatalf("pinKeyForAddr(%q) = %q, want the endpoint", mtls, key)
	}
	if err := connectAsVMAt("127.0.0.1:50151", observedDeviceIdentity{mTLS: true, orgID: 7, assetID: "42"}); err != nil {
		t.Fatal(err)
	}
	pins := readPins()
	if pins["127.0.0.1:50151"].AssetID != "42" || pins[mtls].AssetID != "42" {
		t.Fatalf("pins = %+v, want both of the VM's forwards naming asset 42", pins)
	}
	target := newDialTarget(pinKeyForAddr(mtls), mtls)
	if !target.pinned() || target.Expected == nil || target.Expected.EntityID != "42" {
		t.Fatalf("dial target for %s = %+v, want Expected 42 and the plaintext rung blocked", mtls, target)
	}
	// As if AgentPort+2 answered the ladder's port+1 rung.
	if err := enforceDeviceIdentity(pinKeyForAddr(mtls), observedDeviceIdentity{mTLS: true, orgID: 7, assetID: "99"}); !errors.Is(err, errDeviceIdentityRefused) {
		t.Fatalf("asset 99 at %s: got %v, want a refusal", mtls, err)
	}
}

// R22: a VM reconnect after an agent update passes conn.Addr, which is the
// mTLS forward. It is dialled at the plaintext forward instead, so the ladder
// only ever tries the two ports QEMU forwards, and both are pinned.
func TestVMReconnectAtItsMTLSForwardDialsThePlaintextForward(t *testing.T) {
	stubNonInteractive(t)
	setPinCache(t)
	stubLoopbackVMs(t, map[int]string{50151: "dev"})
	readPins := writePinTestConfig(t, map[string]config.DevicePin{})
	origLadder, origObserve := dialAgentLadderFn, observeDeviceIdentityFn
	var dialled []string
	dialAgentLadderFn = func(_ context.Context, target dialTarget) (*grpcclient.AgentConnection, error, error) {
		dialled = append(dialled, target.Addr)
		return &grpcclient.AgentConnection{Host: "127.0.0.1", Addr: target.Addr,
			AgentService: &fakeAgentVersionClient{resp: &agentpb.GetAgentVersionResponse{}}}, nil, nil
	}
	observeDeviceIdentityFn = func(*grpcclient.AgentConnection) observedDeviceIdentity {
		return observedDeviceIdentity{mTLS: true, orgID: 7, assetID: "42"}
	}
	t.Cleanup(func() { dialAgentLadderFn, observeDeviceIdentityFn = origLadder, origObserve })

	conn, _, err := connectSimulatorAgent(context.Background(), "dev", "127.0.0.1:50152")
	if err != nil {
		t.Fatal(err)
	}
	conn.Close()
	if !reflect.DeepEqual(dialled, []string{"127.0.0.1:50151"}) {
		t.Fatalf("dialled %q, want only the plaintext forward 127.0.0.1:50151", dialled)
	}
	pins := readPins()
	if pins["vm:dev"].AssetID != "42" || pins["127.0.0.1:50151"].AssetID != "42" || pins["127.0.0.1:50152"].AssetID != "42" {
		t.Fatalf("pins = %+v, want vm:dev and both forwards naming asset 42", pins)
	}
}

// mtlsAnswerAddr is where a provisioned agent's authenticated connection
// lands when the ladder dials addr: the mTLS port after it, which is what
// conn.Addr then holds (and what a reconnect after an agent update dials).
func mtlsAnswerAddr(t *testing.T, addr string) string {
	t.Helper()
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("mtlsAnswerAddr(%q): %v", addr, err)
	}
	p, err := strconv.Atoi(port)
	if err != nil {
		t.Fatalf("mtlsAnswerAddr(%q): %v", addr, err)
	}
	return net.JoinHostPort(host, strconv.Itoa(p+agentMTLSPortOffset))
}

// connectTypedAnsweringAtMTLS drives the real connectToAgent path for a typed
// --device address with the ladder answering as obs the way a provisioned
// agent does: on the mTLS port after the one dialled. It returns the
// connection (the caller closes it) and the dial target the ladder was handed.
func connectTypedAnsweringAtMTLS(t *testing.T, addr string, obs observedDeviceIdentity) (*grpcclient.AgentConnection, dialTarget, error) {
	t.Helper()
	var dialled dialTarget
	origLadder, origObserve, origDiscover := dialAgentLadderFn, observeDeviceIdentityFn, discoverLANDevices
	origUSB := usbDirectCandidatesFn
	dialAgentLadderFn = func(_ context.Context, target dialTarget) (*grpcclient.AgentConnection, error, error) {
		dialled = target
		return &grpcclient.AgentConnection{Host: "127.0.0.1", Addr: mtlsAnswerAddr(t, target.Addr), IsMTLS: obs.mTLS,
			AgentService: &fakeAgentVersionClient{resp: &agentpb.GetAgentVersionResponse{}}}, nil, nil
	}
	observeDeviceIdentityFn = func(*grpcclient.AgentConnection) observedDeviceIdentity { return obs }
	discoverLANDevices = func(context.Context, time.Duration) ([]models.LANDevice, error) { return nil, nil }
	usbDirectCandidatesFn = func() []discovery.USBDirectCandidate { return nil }
	defer func() {
		dialAgentLadderFn, observeDeviceIdentityFn, discoverLANDevices = origLadder, origObserve, origDiscover
		usbDirectCandidatesFn = origUSB
	}()
	deviceFlag = addr
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	conn, err := connectToAgent(ctx, SuppressProvisioningHint(), SuppressUpdateCheck(), NonInteractive(), DisableSessionBroker())
	return conn, dialled, err
}

// reconnectAfterUpdate runs the reconnect an agent update makes with conn
// (reconnectAgentAfterRestart), against a ladder that answers every dial as
// obs, and returns each dial target it handed the ladder plus the VMs whose
// simulator path it took.
func reconnectAfterUpdate(t *testing.T, conn *grpcclient.AgentConnection, obs observedDeviceIdentity) (targets []dialTarget, simulatorPath []string, err error) {
	t.Helper()
	origLadder, origObserve, origRecord := dialAgentLadderFn, observeDeviceIdentityFn, vmRecordHostnameFn
	dialAgentLadderFn = func(_ context.Context, target dialTarget) (*grpcclient.AgentConnection, error, error) {
		targets = append(targets, target)
		return &grpcclient.AgentConnection{Host: "127.0.0.1", Addr: mtlsAnswerAddr(t, target.Addr), IsMTLS: obs.mTLS,
			AgentService: &fakeAgentVersionClient{resp: &agentpb.GetAgentVersionResponse{}}}, nil, nil
	}
	observeDeviceIdentityFn = func(*grpcclient.AgentConnection) observedDeviceIdentity { return obs }
	vmRecordHostnameFn = func(name, _ string) error {
		simulatorPath = append(simulatorPath, name)
		return nil
	}
	defer func() {
		dialAgentLadderFn, observeDeviceIdentityFn, vmRecordHostnameFn = origLadder, origObserve, origRecord
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	newConn, err := reconnectAgentAfterRestart(ctx, conn)
	if newConn != nil {
		newConn.Close()
	}
	return targets, simulatorPath, err
}

// assertPlaintextRungRefused runs the real ladder over target with no CLI
// certificate, so nothing can authenticate: a pinned target must end in a
// refusal without ever offering the plaintext rung.
func assertPlaintextRungRefused(t *testing.T, target dialTarget) {
	t.Helper()
	// Counted per address and answered with an error, so a stray probe from
	// elsewhere in the package can neither skew the count nor get a
	// half-built connection back.
	var plaintextCalls atomic.Int32
	origPlaintext := plaintextConnectFn
	plaintextConnectFn = func(_ context.Context, address string) (*grpcclient.AgentConnection, error) {
		if address == target.Addr {
			plaintextCalls.Add(1)
		}
		return nil, errors.New("plaintext rung reached in test")
	}
	defer func() { plaintextConnectFn = origPlaintext }()
	conn, _, err := dialAgentLadderWithCerts(context.Background(), target, nil)
	if conn != nil {
		conn.Close()
	}
	if n := plaintextCalls.Load(); n != 0 || conn != nil {
		t.Errorf("%s: the plaintext rung was offered (%d calls) — an unprovisioned listener there would be accepted", target.Addr, n)
	}
	if !errors.Is(err, errNoAuthenticatedEndpoint) {
		t.Errorf("%s: ladder err = %v, want a pinned-host refusal", target.Addr, err)
	}
}

// R24 (C1): one agent answers a typed 127.0.0.1:P on P (plaintext) and P+1
// (mTLS), so conn.Addr is P+1 after an authenticated connect — and the
// reconnect after an agent update (device update, os update, the connect-time
// update offer) dials exactly that address. It must be pinned to the identity
// the connection just verified, as the single bare 127.0.0.1 key pinned it on
// main: otherwise the plaintext rung is open there, and any same-org
// certificate is accepted on P+1 or P+2, for the rest of the command.
func TestTypedLoopbackReconnectAtTheMTLSPortStaysPinned(t *testing.T) {
	orgOnly := config.DevicePin{OrgID: 7, CloudGRPC: "grpc.a.sh:443"}
	for _, tc := range []struct {
		name string
		pins map[string]config.DevicePin
	}{
		{"fresh user", map[string]config.DevicePin{}},
		{"legacy bare pin", map[string]config.DevicePin{"127.0.0.1": pinA}},
		{"legacy org-only bare pin", map[string]config.DevicePin{"127.0.0.1": orgOnly}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			restoreDeviceGlobals(t)
			stubNonInteractive(t)
			setPinCache(t)
			stubLoopbackVMs(t, nil)
			readPins := writePinTestConfig(t, tc.pins)
			const typed, mtls = "127.0.0.1:50051", "127.0.0.1:50052"

			conn, _, err := connectTypedAnsweringAtMTLS(t, typed, observedDeviceIdentity{mTLS: true, orgID: 7, assetID: "42"})
			if err != nil {
				t.Fatalf("first connect: %v", err)
			}
			defer conn.Close()
			if conn.Addr != mtls {
				t.Fatalf("conn.Addr = %q, want the mTLS port %s", conn.Addr, mtls)
			}

			targets, _, err := reconnectAfterUpdate(t, conn, observedDeviceIdentity{mTLS: true, orgID: 7, assetID: "42"})
			if err != nil {
				t.Fatalf("reconnect: %v", err)
			}
			if len(targets) == 0 {
				t.Fatal("the reconnect never dialled")
			}
			target := targets[0]
			if target.Addr != mtls {
				t.Fatalf("reconnect dialled %q, want %s (conn.Addr)", target.Addr, mtls)
			}
			if !target.pinned() || target.Expected == nil || target.Expected.EntityID != "42" {
				t.Errorf("reconnect target at %s = %+v: want it pinned to asset 42 (plaintext blocked, another device refused)", mtls, target)
			}
			assertPlaintextRungRefused(t, target)

			pins := readPins()
			if pins[typed].AssetID != "42" || pins[mtls].AssetID != "42" {
				t.Errorf("pins = %+v, want %s and %s both naming asset 42", pins, typed, mtls)
			}
			// Unprovisioned first: an accepted asset 99 would be recorded as a
			// first use and then refuse it for the wrong reason.
			for _, key := range []string{typed, mtls} {
				if err := enforceDeviceIdentity(key, observedDeviceIdentity{}); !errors.Is(err, errDeviceIdentityRefused) {
					t.Errorf("an unprovisioned answer at %s: got %v, want a refusal", key, err)
				}
				if err := enforceDeviceIdentity(key, observedDeviceIdentity{mTLS: true, orgID: 7, assetID: "99"}); !errors.Is(err, errDeviceIdentityRefused) {
					t.Errorf("asset 99 at %s: got %v, want a refusal", key, err)
				}
			}
		})
	}
}

// R24: the endpoint that answered is recorded only as R21 records a VM's
// forwards — a first use or an asset adoption. A pin there naming another
// device is never overwritten, and a connection some fallback substituted for
// the dial records nothing beside its own key.
func TestAnsweredEndpointIsRecordedOnlyWhereNothingElseApplies(t *testing.T) {
	stubNonInteractive(t)
	setPinCache(t)
	stubLoopbackVMs(t, nil)
	readPins := writePinTestConfig(t, map[string]config.DevicePin{"127.0.0.1:50052": pinB})
	conn := &grpcclient.AgentConnection{Addr: "127.0.0.1:50052"}
	orig := observeDeviceIdentityFn
	observeDeviceIdentityFn = func(*grpcclient.AgentConnection) observedDeviceIdentity {
		return observedDeviceIdentity{mTLS: true, orgID: 7, assetID: "42"}
	}
	t.Cleanup(func() { observeDeviceIdentityFn = orig })

	if err := enforceDevicePinAt("127.0.0.1:50051", "127.0.0.1:50051", conn); err != nil {
		t.Fatalf("a first use at :50051 was refused over another endpoint's pin: %v", err)
	}
	pins := readPins()
	if pins["127.0.0.1:50051"].AssetID != "42" || pins["127.0.0.1:50052"].AssetID != "43" {
		t.Fatalf("pins = %+v, want :50051=42 and :50052's own pin (43) untouched", pins)
	}

	readPins = writePinTestConfig(t, map[string]config.DevicePin{})
	if err := enforceDevicePinAt("127.0.0.1:50051", "", conn); err != nil {
		t.Fatal(err)
	}
	if pins := readPins(); len(pins) != 1 || pins["127.0.0.1:50051"].AssetID != "42" {
		t.Fatalf("pins = %+v, want only :50051 — a substituted connection answered nowhere that was dialled", pins)
	}
}

// R25 (C2, parked A1/A5): a FIRST connection typed at a running VM's mTLS
// forward, 127.0.0.1:A+1, is dialled at the VM's agent forward A and keyed
// vm:<name>, so it records both forwards like any connection to the VM.
// Keyed as the endpoint A+1 instead, it moved (or adopted) the legacy bare pin
// onto A+1 alone, and once the VM stopped, another local account binding A was
// accepted where main refused it.
func TestTypedMTLSForwardOfARunningVMConnectsAsTheVM(t *testing.T) {
	orgOnly := config.DevicePin{OrgID: 7, CloudGRPC: "grpc.a.sh:443"}
	for _, tc := range []struct {
		name string
		pins map[string]config.DevicePin
	}{
		{"legacy bare pin (A1)", map[string]config.DevicePin{"127.0.0.1": pinA}},
		{"legacy org-only bare pin (A5)", map[string]config.DevicePin{"127.0.0.1": orgOnly}},
		{"fresh user", map[string]config.DevicePin{}},
	} {
		for _, door := range []string{"connectToAgent", "resolveTarget"} {
			t.Run(tc.name+"/"+door, func(t *testing.T) {
				restoreDeviceGlobals(t)
				stubNonInteractive(t)
				setPinCache(t)
				readPins := writePinTestConfig(t, tc.pins)
				const agent, mtls = "127.0.0.1:50051", "127.0.0.1:50052"
				obs42 := observedDeviceIdentity{mTLS: true, orgID: 7, assetID: "42"}

				stubLoopbackVMs(t, map[int]string{50051: "dev"})
				var target dialTarget
				var err error
				if door == "connectToAgent" {
					var conn *grpcclient.AgentConnection
					conn, target, err = connectTypedAnsweringAtMTLS(t, mtls, obs42)
					if conn != nil {
						conn.Close()
					}
				} else {
					target, err = resolveTypedAnsweringAtMTLS(t, mtls, obs42)
				}
				if err != nil {
					t.Fatalf("the running VM was refused: %v", err)
				}
				if target.Addr != agent || target.PinKey != "vm:dev" {
					t.Errorf("typed %s dialled %q under %q, want %s under vm:dev", mtls, target.Addr, target.PinKey, agent)
				}
				pins := readPins()
				if pins["vm:dev"].AssetID != "42" || pins[agent].AssetID != "42" || pins[mtls].AssetID != "42" {
					t.Errorf("pins = %+v, want vm:dev and both forwards naming asset 42", pins)
				}

				stubLoopbackVMs(t, nil) // the VM stops
				for _, addr := range []string{agent, mtls} {
					key := pinKeyForAddr(addr)
					if target := newDialTarget(key, addr); !target.pinned() || target.Expected == nil || target.Expected.EntityID != "42" {
						t.Errorf("stopped VM's %s: dial target %+v, want Expected 42 and the plaintext rung blocked", addr, target)
					}
					// Unprovisioned first: an accepted asset 99 would be
					// recorded as a first use and then refuse it for the
					// wrong reason.
					for _, obs := range []observedDeviceIdentity{{}, {mTLS: true, orgID: 7, assetID: "99"}} {
						if err := enforceDeviceIdentity(key, obs); !errors.Is(err, errDeviceIdentityRefused) {
							t.Errorf("stopped VM's %s answered as %+v: got %v, want a refusal", addr, obs, err)
						}
					}
				}
			})
		}
	}
}

// resolveTypedAnsweringAtMTLS is connectTypedAnsweringAtMTLS through
// resolveTarget's direct path.
func resolveTypedAnsweringAtMTLS(t *testing.T, addr string, obs observedDeviceIdentity) (dialTarget, error) {
	t.Helper()
	var dialled dialTarget
	origLadder, origObserve, origDiscover := dialAgentLadderFn, observeDeviceIdentityFn, discoverLANDevices
	dialAgentLadderFn = func(_ context.Context, target dialTarget) (*grpcclient.AgentConnection, error, error) {
		dialled = target
		return &grpcclient.AgentConnection{Host: "127.0.0.1", Addr: mtlsAnswerAddr(t, target.Addr), IsMTLS: obs.mTLS,
			AgentService: &fakeAgentVersionClient{resp: &agentpb.GetAgentVersionResponse{}}}, nil, nil
	}
	observeDeviceIdentityFn = func(*grpcclient.AgentConnection) observedDeviceIdentity { return obs }
	discoverLANDevices = func(context.Context, time.Duration) ([]models.LANDevice, error) { return nil, nil }
	defer func() {
		dialAgentLadderFn, observeDeviceIdentityFn, discoverLANDevices = origLadder, origObserve, origDiscover
	}()
	deviceFlag = addr
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	sel, err := resolveTarget(ctx, SuppressProvisioningHint(), SuppressUpdateCheck(), NonInteractive(), DisableSessionBroker())
	if sel != nil {
		sel.Close()
	}
	return dialled, err
}

// R25: the reconnect path (waitForAgentRestart → connectWithAutoTLS), and
// every other direct dial through connectWithAutoTLS, dials a running VM's
// mTLS forward as the VM too — never under the endpoint key, whose ladder
// would also try A+2, which QEMU does not forward.
func TestDirectDialOfARunningVMsMTLSForwardIsKeyedAsTheVM(t *testing.T) {
	setPinCache(t)
	stubLoopbackVMs(t, map[int]string{50051: "dev"})
	setPinConfig(t, map[string]config.DevicePin{"vm:dev": pinA, "127.0.0.1:50052": pinB})
	var targets []dialTarget
	origLadder := dialAgentLadderFn
	dialAgentLadderFn = func(_ context.Context, target dialTarget) (*grpcclient.AgentConnection, error, error) {
		targets = append(targets, target)
		return nil, nil, errors.New("agent restarting")
	}
	t.Cleanup(func() { dialAgentLadderFn = origLadder })

	_, _ = connectWithAutoTLS(context.Background(), "127.0.0.1:50052")
	if len(targets) != 1 {
		t.Fatalf("ladder dialled %d times, want 1", len(targets))
	}
	got := targets[0]
	if got.Addr != "127.0.0.1:50051" || got.PinKey != "vm:dev" || got.Expected == nil || got.Expected.EntityID != "42" {
		t.Fatalf("dial target = %+v, want 127.0.0.1:50051 under vm:dev (asset 42)", got)
	}

	// Anything else is left alone: a port no VM forwards, the agent port
	// itself, and spellings other than the literal forward address.
	for _, addr := range []string{"127.0.0.1:50061", "127.0.0.1:50051", "localhost:50052", "127.0.0.2:50052", "10.0.0.5:50052"} {
		if got := vmForwardDialAddr(addr); got != addr {
			t.Errorf("vmForwardDialAddr(%q) = %q, want it unchanged", addr, got)
		}
	}
	if got := dialPinKeyForDevice("127.0.0.1:50052"); got != "vm:dev" {
		t.Errorf("dialPinKeyForDevice(127.0.0.1:50052) = %q, want vm:dev (the key its dial is checked under)", got)
	}
}

// R27(1): a reconnect answered by a device that is refused must say so at
// once. Retrying only asks the same wrong device again until the deadline, and
// then reports "timed out waiting for agent to restart" instead of the refusal
// and the unpin that resolves it.
func TestWaitForAgentRestartReturnsAnIdentityRefusalImmediately(t *testing.T) {
	calls := 0
	origLadder := dialAgentLadderFn
	dialAgentLadderFn = func(context.Context, dialTarget) (*grpcclient.AgentConnection, error, error) {
		calls++
		return nil, nil, refuseDevicePin(devicePinDiagnostic{hostname: "127.0.0.1:50052", heading: "Connection blocked."})
	}
	t.Cleanup(func() { dialAgentLadderFn = origLadder })
	stubLoopbackVMs(t, nil)
	setPinCache(t)
	setPinConfig(t, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	_, err := waitForAgentRestart(ctx, "127.0.0.1:50052")
	if !errors.Is(err, errDeviceIdentityRefused) {
		t.Fatalf("waitForAgentRestart = %v, want the identity refusal", err)
	}
	if calls != 1 {
		t.Fatalf("ladder dialled %d times after a refusal, want 1", calls)
	}
}

// R27(1): an agent that is simply not back yet is still waited for.
func TestWaitForAgentRestartStillRetriesAnAgentThatIsNotBackYet(t *testing.T) {
	calls := 0
	origLadder := dialAgentLadderFn
	dialAgentLadderFn = func(_ context.Context, target dialTarget) (*grpcclient.AgentConnection, error, error) {
		calls++
		if calls == 1 {
			return nil, nil, &noAuthenticatedEndpointError{msg: "nothing answered"}
		}
		return &grpcclient.AgentConnection{Addr: target.Addr,
			AgentService: &fakeAgentVersionClient{resp: &agentpb.GetAgentVersionResponse{}}}, nil, nil
	}
	t.Cleanup(func() { dialAgentLadderFn = origLadder })
	stubLoopbackVMs(t, nil)
	setPinCache(t)
	setPinConfig(t, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	conn, err := waitForAgentRestart(ctx, "127.0.0.1:50052")
	if err != nil {
		t.Fatalf("waitForAgentRestart = %v, want the agent once it answers", err)
	}
	conn.Close()
	if calls != 2 {
		t.Fatalf("ladder dialled %d times, want 2", calls)
	}
}

// R27(2), I2: VM b runs on the port where VM a left endpoint pins. A typed
// connection is keyed vm:b, and so must its reconnect after an agent update
// be: it goes through the VM's own path, not through conn.Addr — the mTLS
// forward, whose stale pin names VM a and would refuse b until a timeout.
func TestTypedVMConnectionReconnectsThroughTheVM(t *testing.T) {
	restoreDeviceGlobals(t)
	stubNonInteractive(t)
	setPinCache(t)
	stale := config.DevicePin{OrgID: 7, CloudGRPC: "grpc.a.sh:443", AssetID: "42"}
	readPins := writePinTestConfig(t, map[string]config.DevicePin{
		"vm:a": stale, "127.0.0.1:50051": stale, "127.0.0.1:50052": stale,
	})
	stubLoopbackVMs(t, map[int]string{50051: "b"})
	obs43 := observedDeviceIdentity{mTLS: true, orgID: 7, assetID: "43"}

	conn, _, err := connectTypedAnsweringAtMTLS(t, "127.0.0.1:50051", obs43)
	if err != nil {
		t.Fatalf("typed connect to VM b: %v", err)
	}
	defer conn.Close()
	if conn.SimulatorName != "b" {
		t.Fatalf("conn.SimulatorName = %q, want b: the connection was judged as vm:b", conn.SimulatorName)
	}
	targets, simulatorPath, err := reconnectAfterUpdate(t, conn, obs43)
	if err != nil {
		t.Fatalf("reconnect to VM b: %v", err)
	}
	if !reflect.DeepEqual(simulatorPath, []string{"b"}) {
		t.Fatalf("reconnect took the simulator path for %q, want [b]", simulatorPath)
	}
	if len(targets) == 0 || targets[0].PinKey != "vm:b" || targets[0].Addr != "127.0.0.1:50051" ||
		targets[0].Expected == nil || targets[0].Expected.EntityID != "43" {
		t.Fatalf("reconnect targets = %+v, want 127.0.0.1:50051 under vm:b (asset 43)", targets)
	}
	pins := readPins()
	if pins["vm:b"].AssetID != "43" || pins["127.0.0.1:50051"].AssetID != "42" || pins["127.0.0.1:50052"].AssetID != "42" {
		t.Fatalf("pins = %+v, want vm:b=43 and VM a's endpoint pins untouched (never overwritten)", pins)
	}
}

// R27(3): `wendy device unpin vm:<name>` — the recipe for replacing a VM —
// also clears the endpoint pins its connections filed at 127.0.0.1, but only
// those naming the same device; and `unpin 127.0.0.1:PORT` still clears a
// stale endpoint pin while no VM runs there.
func TestUnpinVMClearsItsSameIdentityEndpointPins(t *testing.T) {
	stubLoopbackVMs(t, nil)
	setPinCache(t)
	orgOnly := config.DevicePin{OrgID: 7, CloudGRPC: "grpc.a.sh:443"}
	readPins := writePinTestConfig(t, map[string]config.DevicePin{
		"vm:a":            pinA,
		"127.0.0.1:50051": pinA,
		"127.0.0.1:50052": pinA,
		"127.0.0.1:50061": pinB, // another device
		"localhost:50051": pinA, // another address spelling: not a VM forward
		"127.0.0.1":       pinA, // a bare legacy pin: not an endpoint
		"vm:b":            orgOnly,
		"127.0.0.1:50071": orgOnly,
	})
	out := runUnpin(t, "vm:a")
	pins := readPins()
	for _, gone := range []string{"vm:a", "127.0.0.1:50051", "127.0.0.1:50052"} {
		if _, ok := pins[gone]; ok {
			t.Errorf("unpin vm:a left %s", gone)
		}
		if !strings.Contains(out, `"`+gone+`"`) {
			t.Errorf("unpin vm:a did not report clearing %s:\n%s", gone, out)
		}
	}
	for _, kept := range []string{"127.0.0.1:50061", "localhost:50051", "127.0.0.1", "vm:b", "127.0.0.1:50071"} {
		if _, ok := pins[kept]; !ok {
			t.Errorf("unpin vm:a cleared %s, which it does not govern", kept)
		}
	}

	// An org-only VM pin names no device, so no endpoint is "the same".
	runUnpin(t, "vm:b")
	if _, ok := readPins()["127.0.0.1:50071"]; !ok {
		t.Error("unpin vm:b (org-only) cleared an org-only endpoint pin")
	}

	// A stale endpoint pin with no VM running there is cleared by address.
	runUnpin(t, "127.0.0.1:50061")
	if _, ok := readPins()["127.0.0.1:50061"]; ok {
		t.Error("unpin 127.0.0.1:50061 left the stale endpoint pin")
	}
}

// R24: which endpoints a connection records beside its own key. Only a VM key
// or a port-qualified loopback key records any; the answered endpoint is added
// only when it is another port-qualified loopback key (never vm:<name>, never
// a non-loopback host); a substituted connection records none.
func TestEndpointPinKeys(t *testing.T) {
	stubLoopbackVMs(t, map[int]string{50051: "dev"})
	for _, tc := range []struct {
		key, dialled, answered string
		want                   []string
	}{
		{"127.0.0.1:50061", "127.0.0.1:50061", "127.0.0.1:50062", []string{"127.0.0.1:50062"}},
		{"localhost:50061", "localhost:50061", "[::1]:50062", []string{"[::1]:50062"}},
		{"localhost:50061", "localhost:50061", "127.0.0.1:50062", []string{"127.0.0.1:50062"}},
		// Answered where it was dialled: nothing beside the key itself.
		{"127.0.0.1:50061", "127.0.0.1:50061", "127.0.0.1:50061", nil},
		// A substituted connection answered nowhere that was dialled.
		{"127.0.0.1:50061", "", "127.0.0.1:50062", nil},
		// An answering port that keys as a running VM is never filed.
		{"localhost:50050", "localhost:50050", "127.0.0.1:50051", nil},
		// Non-loopback keys, and bare hosts, record nothing extra.
		{"rpi5.local", "rpi5.local:50051", "10.0.0.5:50052", nil},
		{"rpi5.local", "rpi5.local:50051", "127.0.0.1:50052", nil},
		{"10.0.0.5", "10.0.0.5:50051", "10.0.0.5:50052", nil},
		{"127.0.0.1", "127.0.0.1:50061", "127.0.0.1:50062", nil},
		// A VM: both forwards, the answered one not repeated.
		{"vm:dev", "127.0.0.1:50051", "127.0.0.1:50052", []string{"127.0.0.1:50051", "127.0.0.1:50052"}},
		// A VM the store cannot place: the address dialled and the one that answered.
		{"vm:other", "127.0.0.1:50071", "127.0.0.1:50072", []string{"127.0.0.1:50071", "127.0.0.1:50072"}},
		{"vm:dev", "", "127.0.0.1:50052", nil},
	} {
		if got := endpointPinKeys(tc.key, tc.dialled, tc.answered); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("endpointPinKeys(%q, %q, %q) = %q, want %q", tc.key, tc.dialled, tc.answered, got, tc.want)
		}
	}
}

// R25: a port that is itself a running VM's agent port keys as that VM, even
// when it is also the port after another VM's.
func TestVMForwardDialAddrLeavesAnotherVMsAgentPortAlone(t *testing.T) {
	stubLoopbackVMs(t, map[int]string{50051: "a", 50052: "b"})
	if got := vmForwardDialAddr("127.0.0.1:50052"); got != "127.0.0.1:50052" {
		t.Fatalf("vmForwardDialAddr(127.0.0.1:50052) = %q, want it left as VM b's agent port", got)
	}
	if key := pinKeyForAddr(vmForwardDialAddr("127.0.0.1:50052")); key != "vm:b" {
		t.Fatalf("key = %q, want vm:b", key)
	}
}

// R27(3): the endpoint pins go only with the vm:<name> pin itself. When a
// cloud pin under another of the VM's names governs instead, naming another
// device, the unpin leaves vm:<name> (it names a different device than the
// one being unpinned), and so leaves the endpoint pins naming it too.
func TestUnpinVMKeepsEndpointPinsWhenItKeepsTheVMPin(t *testing.T) {
	stubLoopbackVMs(t, nil)
	cloud99 := config.DevicePin{OrgID: 7, CloudGRPC: "grpc.a.sh:443", AssetID: "99", Source: config.PinSourceCloud}
	readPins := writePinTestConfig(t, map[string]config.DevicePin{
		"vm:a": pinA, "a-alias": cloud99, "127.0.0.1:50051": pinA,
	})
	setPinCache(t, discoverycache.Entry{ID: "vm-a", DisplayName: "a-alias", Hostname: "vm:a"})
	runUnpin(t, "vm:a")
	pins := readPins()
	if _, ok := pins["a-alias"]; ok {
		t.Fatalf("pins = %+v: the governing alias pin was not cleared (test precondition)", pins)
	}
	if pins["vm:a"].AssetID != "42" || pins["127.0.0.1:50051"].AssetID != "42" {
		t.Fatalf("pins = %+v, want vm:a and its endpoint pin both kept", pins)
	}
}
