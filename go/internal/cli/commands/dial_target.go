package commands

import (
	"errors"
	"fmt"
	"net"
	"slices"
	"strconv"
	"strings"

	"github.com/wendylabsinc/wendy/go/internal/cli/grpcclient"
	"github.com/wendylabsinc/wendy/go/internal/cli/vm"
	"github.com/wendylabsinc/wendy/go/internal/shared/certs"
	"github.com/wendylabsinc/wendy/go/internal/shared/config"
	"github.com/wendylabsinc/wendy/go/internal/shared/devicepin"
)

// dialTarget carries what the ladder needs to know about *who* it is dialing,
// not just where. Before this, the ladder took a bare address, so the identity
// the user asked for was gone by the time a certificate arrived — which is
// exactly what let a spoofed mDNS answer redirect a connection to another
// same-CA host.
type dialTarget struct {
	// PinKey is the name the user asked for (--device value, saved default, or
	// the picker's device name) — never the resolved IP, which changes on
	// ordinary DHCP churn and would train users to unpin reflexively. An empty
	// PinKey disables pin enforcement for this dial.
	PinKey string
	// PinnedKey is the key the governing pin is ACTUALLY filed under, which is
	// not always PinKey: lookupPin resolves a pin across every name the device
	// answers to, so dialling "wendyos-calm-zinnia" can be governed by a pin
	// recorded under the cloud roster's "calm-zinnia". Empty when no pin
	// governs this dial.
	//
	// It exists because a refusal has to name a key `wendy device unpin` can
	// act on. Naming the dialled key when an alias holds the pin sends the user
	// to a command that clears nothing, and the next dial refuses identically —
	// a permanent dead end dressed up as an escape hatch.
	PinnedKey string
	// Addr is the host:port actually dialed, and the first of Candidates when
	// there are several. It stays the single "primary" address so every caller
	// and diagnostic that only ever wanted one address keeps working.
	Addr string
	// Candidates are every address this dial may try, in the order to try them
	// (see orderDialCandidates: IPv4, then routable IPv6, then link-local/ULA).
	// Empty means "just Addr" — read it through dialCandidates, never directly.
	//
	// One name legitimately resolves to several addresses, and on a network that
	// hands out fresh DHCP leases the first one is regularly the wrong one. The
	// ladder used to be handed a single pre-resolved address, so a device that
	// was perfectly reachable at its second address was reported as unreachable
	// — and, when pinned, reported as an identity problem.
	Candidates []string
	// Expected constrains the peer certificate. Non-nil only when a pin (or a
	// cloud-seeded value) names a specific asset.
	Expected *certs.WendyIdentity
}

// dialCandidates returns the addresses the ladder must walk, always at least
// Addr. Candidates is consulted through here so that a hand-built dialTarget
// (and every existing caller that sets only Addr) behaves exactly as it did
// before multi-address dialing existed.
func (t dialTarget) dialCandidates() []string {
	if len(t.Candidates) > 0 {
		return t.Candidates
	}
	if t.Addr == "" {
		return nil
	}
	return []string{t.Addr}
}

// refusalKey is the name a refusal for this dial must print: the key the pin is
// filed under when one governs, else the name the user asked for. Falling back
// to PinKey keeps a hand-built dialTarget (and any future caller that sets only
// PinKey) naming something rather than an empty string.
func (t dialTarget) refusalKey() string {
	if t.PinnedKey != "" {
		return t.PinnedKey
	}
	return t.PinKey
}

// pinned reports whether a pin governs this dial, answered entirely from what
// newDialTarget already resolved. A pinned host has been reached over mTLS
// before, so the ladder must not offer it the plaintext rung.
//
// It reads the target rather than re-reading pin state because one connect must
// make ONE decision about what the pin says. The guard used to call back into
// the config for a second, independent answer, which could disagree with the
// first — a cloud seeding or an unpin landing from another process mid-ladder
// would have the plaintext rung consult a pin state that never produced this
// target's Expected or refusalKey. Deriving both from the same resolution makes
// that disagreement unrepresentable.
//
// PinnedKey is non-empty for exactly the dials lookupPin found a pin for, and
// never empty when it did: the key comes from pinCandidateKeys, which drops
// empty candidates, so "a pin governs" and "we know the key it is filed under"
// are the same fact. Expected is deliberately NOT consulted — it is set only
// when a pin names an asset, so a pin without one would read as unpinned, and
// the constraint Expected carries is enforced in VerifyConnection, not here.
func (t dialTarget) pinned() bool {
	return t.PinnedKey != ""
}

// loadConfigForPinFn is a seam over config.Load for tests.
var loadConfigForPinFn = config.Load

// plaintextConnectFn is a seam over grpcclient.Connect — the ladder's last,
// unauthenticated rung — so a test can prove that rung was never reached.
var plaintextConnectFn = grpcclient.Connect

// identityMismatchFn is a seam over the ladder's reading of a wrong-device
// rejection. The flag itself can only be set by a real TLS handshake — the
// VerifyConnection sink owns the unexported field — so seaming the ladder's
// CONSUMPTION of it is what puts the abort under test without a live ML-DSA
// peer. That abort matters on its own: once a cloud-seeded Expected can exist
// for a host with no config pin, it is the only thing standing between a wrong
// device and the plaintext rung.
var identityMismatchFn = (*grpcclient.AgentConnection).IdentityMismatch

// pinMismatchFn is the same seam for the SPKI store's rejection. It is seamed
// for the same reason: only a real handshake against a device whose key rotated
// can set the flag, so testing the ladder's REACTION to it — abort, no
// plaintext rung, a message naming `wendy device unpin` — needs the read
// stubbed rather than a live ML-DSA peer with a rotated keypair.
var pinMismatchFn = (*grpcclient.AgentConnection).PinMismatch

// newDialTarget resolves the pin for pinKey and returns a target constrained by
// it. Key resolution deliberately may read discovery-derived names: choosing
// the wrong key can only ever produce a mismatch — a stricter outcome — never
// a bypass, because the trust decision itself stays on the certificate.
func newDialTarget(pinKey, addr string) dialTarget {
	return newDialTargetCandidates(pinKey, []string{addr})
}

// newDialTargetCandidates is newDialTarget for a name that resolved to several
// addresses. The pin resolution is identical and deliberately so: which device
// is acceptable is decided once, from the name the user asked for, and cannot
// vary between candidates. Only the routing differs.
func newDialTargetCandidates(pinKey string, addrs []string) dialTarget {
	target := dialTarget{PinKey: pinKey}
	if len(addrs) > 0 {
		target.Addr = addrs[0]
		if len(addrs) > 1 {
			target.Candidates = addrs
		}
	}
	pin, key, ok := governingPin(pinKey)
	if !ok {
		return target
	}
	target.PinnedKey = key
	target.Expected = expectedIdentityForPin(pin)
	return target
}

// governingPin resolves the pin that governs pinKey across every name the
// device answers to, returning it and the key it is actually filed under. It is
// the single reader of pin state on the dial path, so the identity a dial
// enforces, the fact that it is pinned, and the key a refusal names can never
// disagree about which pin they are talking about.
func governingPin(pinKey string) (config.DevicePin, string, bool) {
	if pinKey == "" {
		return config.DevicePin{}, "", false
	}
	cfg, err := loadConfigForPinFn()
	if err != nil {
		return config.DevicePin{}, "", false
	}
	return lookupPin(cfg, pinKey)
}

// pinKeyForAddr extracts the pin key from the address a caller was asked to
// reach, BEFORE any resolution: the host as the user named it. That is the same
// key enforceDevicePin records under, so the two agree on what "this device"
// means. A resolved IP is deliberately never used as a key — it changes on
// ordinary DHCP churn — but an address the user typed as a literal IP is the
// name they asked for, so it keys a pin like any other host.
//
// Loopback is the one exception, because there the host names no device:
// every local VM and every port forward answers on it. A loopback address is
// keyed per endpoint, by its normalised host and port (localhost:50051,
// 127.0.0.1:50051), with one refinement: the literal text 127.0.0.1 — the one
// address QEMU's user-mode forward binds — on the forwarded plaintext agent
// port of a running local VM is keyed as that VM, vm:<name>, the key its alias
// already uses (not the mTLS port beside it; see runningVMOnLoopbackPort).
// Nothing else is: localhost can resolve to ::1 first; ::1, 127.0.0.x and
// IPv4-mapped addresses are other sockets, which something other than the VM
// can answer on; and "127.0.0.1." or a padded " 127.0.0.1" is not dialled as
// that address at all (the resolver looks it up as a name).
//
// No loopback key is ever empty, so the plaintext-downgrade guard stays armed;
// pins older CLIs filed under the bare host are not orphaned (pinCandidateKeys
// still consults them for a port-qualified key, and enforceDeviceIdentity
// moves one onto its endpoint once the device it names is the one answering);
// a VM's two forwarded endpoints are pinned beside its vm:<name> key (see
// vmEndpointPinKeys), so they stay pinned while the VM is stopped; and a
// loopback connection also pins the endpoint that actually answered it —
// usually the mTLS port after the one dialled, which a reconnect after an
// agent update dials (see endpointPinKeys). A direct dial of a running VM's
// mTLS forward is re-aimed at its agent forward before this is consulted (see
// vmForwardDialAddr), so it is keyed as the VM too. Non-loopback hosts are
// unchanged: one device per host, whatever the port.
func pinKeyForAddr(addr string) string {
	// SplitHostPort accepts non-numeric service names, so vm:dev would
	// otherwise become just "vm" when set-default/unpin derives its key.
	if name, matched, err := simulatorName(addr); err == nil && matched {
		return vmDeviceIDPrefix + name
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return strings.TrimSpace(addr)
	}
	if !isLoopbackHost(host) {
		return host
	}
	p, convErr := strconv.Atoi(port)
	// Compared as typed, before normalisation: only the literal address
	// reaches the forward.
	if convErr == nil && host == vmForwardHost {
		if name, ok := loopbackVMNameFn(p); ok {
			return vmDeviceIDPrefix + name
		}
	}
	host = normalizeLoopbackHost(host)
	if convErr != nil {
		return net.JoinHostPort(host, port)
	}
	return net.JoinHostPort(host, strconv.Itoa(p))
}

// vmForwardHost is the only address a user-mode VM's agent forward listens on:
// the launcher binds hostfwd to 127.0.0.1 explicitly (vm.NetConfig's QEMU
// arguments), never to localhost, ::1 or the rest of 127/8.
const vmForwardHost = "127.0.0.1"

// normalizeLoopbackHost is the spelling a loopback host takes in a pin key —
// lowercased, trailing dot dropped: the same normalisation isLoopbackHost
// matches under, so every spelling it accepts as one host keys one pin.
func normalizeLoopbackHost(host string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
}

// loopbackVMNameFn names the running user-mode VM whose agent is forwarded to
// a loopback port. A seam over the VM store for tests.
var loopbackVMNameFn = runningVMOnLoopbackPort

// runningVMOnLoopbackPort reports which running user-mode VM forwards its
// plaintext agent port to port on 127.0.0.1. It is consulted for
// vmForwardHost only. The mTLS port beside it (AgentPort+1) does not name the
// VM: the dial ladder tries the given port and the one after it, and
// AgentPort+2 is not forwarded — something else can listen there — so a dial
// at AgentPort+1 is never made under the VM's key. A direct dial there is
// re-aimed at AgentPort first (vmForwardDialAddr); the key AgentPort+1 itself
// is per endpoint, where the VM's identity is pinned by recordEndpointPin, so
// it stays pinned once the VM stops. A run record counts only while its VM
// holds the run lock (vm.Store.Status reaps stale ones), and two records
// claiming one port name no VM: the key then falls back to the endpoint rather
// than a guess.
func runningVMOnLoopbackPort(port int) (string, bool) {
	statuses, err := vmStatusesFn()
	if err != nil {
		return "", false
	}
	match := ""
	for _, st := range statuses {
		if !st.Running || st.State.NetMode != vm.NetUser || st.State.AgentPort == 0 {
			continue
		}
		if port != st.State.AgentPort {
			continue
		}
		if match != "" {
			return "", false
		}
		match = st.Name
	}
	return match, match != ""
}

// runningVMAgentPortFn reports the plaintext agent port a running user-mode VM
// forwards on 127.0.0.1. A seam over the VM store for tests.
var runningVMAgentPortFn = runningVMAgentPort

// runningVMAgentPort reports name's plaintext agent port while it runs in user
// mode — the port QEMU forwards on 127.0.0.1, with its mTLS port beside it.
func runningVMAgentPort(name string) (int, bool) {
	statuses, err := vmStatusesFn()
	if err != nil {
		return 0, false
	}
	for _, st := range statuses {
		if st.Name == name && st.Running && st.State.NetMode == vm.NetUser && st.State.AgentPort != 0 {
			return st.State.AgentPort, true
		}
	}
	return 0, false
}

// dialPinKeyForDevice is the pin key a dial to device — as typed for --device
// or set-default — is checked under. It adds the default agent port and
// re-aims a running VM's mTLS forward first, exactly as resolveDeviceAddress
// does, because for loopback the port is part of the key: "127.0.0.1" is
// dialled, and pinned, as 127.0.0.1:50051.
func dialPinKeyForDevice(device string) string {
	if _, matched, err := simulatorName(device); matched || err != nil {
		return pinKeyForAddr(device)
	}
	if _, _, err := net.SplitHostPort(device); err != nil {
		device = hostPort(device, defaultAgentPort)
	}
	return pinKeyForAddr(vmForwardDialAddr(device))
}

// legacyLoopbackPinKey returns the bare host a port-qualified loopback key's
// pin was filed under before loopback endpoints were keyed by port
// ("127.0.0.1" for "127.0.0.1:50051"), normalised like the key itself, or ""
// for any other key.
func legacyLoopbackPinKey(key string) string {
	host, _, err := net.SplitHostPort(key)
	if err != nil || !isLoopbackHost(host) {
		return ""
	}
	return normalizeLoopbackHost(host)
}

// vmEndpointPinKeys are the endpoints a connection judged under pinKey may
// also be pinned under (see recordEndpointPin), when pinKey is a VM's
// vm:<name> key and dialAddr is on the literal vmForwardHost (QEMU's forward
// answers there only): both ports QEMU forwards for the VM —
// 127.0.0.1:AgentPort and the mTLS port beside it — when dialAddr is one of
// them. A VM the store cannot place yields just the address dialled. Nil for
// any other key or address, a port the store says is not this VM's, and an
// unknown dial address — including a connection some fallback substituted for
// the dial.
//
// 127.0.0.1:AgentPort is keyed as the VM only while the VM runs, and
// AgentPort+1 never is (pinKeyForAddr). Without pins of their own there,
// anything that binds either port once the VM stops — or AgentPort+2, which a
// dial at AgentPort+1 also tries — would be a first use, where the bare
// "127.0.0.1" key older CLIs used would have refused it.
func vmEndpointPinKeys(pinKey, dialAddr string) []string {
	name, ok := strings.CutPrefix(pinKey, vmDeviceIDPrefix)
	if !ok {
		return nil
	}
	host, port, err := net.SplitHostPort(dialAddr)
	if err != nil || host != vmForwardHost {
		return nil
	}
	p, err := strconv.Atoi(port)
	if err != nil {
		return nil
	}
	agentPort, known := runningVMAgentPortFn(name)
	if !known {
		return []string{net.JoinHostPort(vmForwardHost, strconv.Itoa(p))}
	}
	if p != agentPort && p != agentPort+agentMTLSPortOffset {
		return nil
	}
	return []string{
		net.JoinHostPort(vmForwardHost, strconv.Itoa(agentPort)),
		net.JoinHostPort(vmForwardHost, strconv.Itoa(agentPort+agentMTLSPortOffset)),
	}
}

// vmAgentForwardAddr maps a VM's mTLS forward, 127.0.0.1:AgentPort+1 — what
// conn.Addr holds after a provisioned connection, and so what a reconnect
// after an agent update passes — back to its plaintext agent forward. The
// dial ladder tries the given port and the one after it, so dialling the mTLS
// forward would also try AgentPort+2, which QEMU does not forward. Any other
// address, or a VM the store cannot place, is returned unchanged.
func vmAgentForwardAddr(name, addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil || host != vmForwardHost {
		return addr
	}
	p, err := strconv.Atoi(port)
	if err != nil {
		return addr
	}
	agentPort, ok := runningVMAgentPortFn(name)
	if !ok || p != agentPort+agentMTLSPortOffset {
		return addr
	}
	return net.JoinHostPort(vmForwardHost, strconv.Itoa(agentPort))
}

// vmForwardDialAddr is vmAgentForwardAddr for a direct dial that does not know
// it is reaching a VM: a typed --device address, or the conn.Addr a reconnect
// after an agent update dials. The literal forward address 127.0.0.1 on the
// mTLS port of a running user-mode VM (AgentPort+1, a port that is not itself
// a VM's agent port) becomes that VM's agent forward, 127.0.0.1:AgentPort, so
// the connection is keyed vm:<name> by pinKeyForAddr and records both of the
// VM's forwards, exactly like a dial of the agent port or the vm:<name> alias.
//
// Keyed as its own endpoint instead, a first connection there would move (or
// adopt) a legacy bare pin onto AgentPort+1 alone, leaving AgentPort unpinned
// once the VM stops; and its ladder would also try AgentPort+2, which QEMU
// does not forward. Every other address is returned unchanged — without
// reading the VM store unless the host is the literal forward address.
func vmForwardDialAddr(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil || host != vmForwardHost {
		return addr
	}
	p, err := strconv.Atoi(port)
	if err != nil || p <= agentMTLSPortOffset {
		return addr
	}
	// A VM's own agent port keys as that VM already.
	if _, ok := loopbackVMNameFn(p); ok {
		return addr
	}
	name, ok := loopbackVMNameFn(p - agentMTLSPortOffset)
	if !ok {
		return addr
	}
	return vmAgentForwardAddr(name, addr)
}

// endpointPinKeys are the loopback endpoints a connection judged under pinKey
// and dialled at dialAddr also records its accepted identity under (see
// recordEndpointPin), so each stays pinned when it is dialled on its own:
//
//   - for a VM's vm:<name> key, both of the VM's forwards (vmEndpointPinKeys);
//   - for vm:<name> or a port-qualified loopback key, the endpoint the
//     connection actually answered on — answeredAddr, the connection's
//     conn.Addr — when that is another port-qualified loopback key. One agent
//     answers a dial of port P on P+1 once it is provisioned (the ladder's
//     mTLS rung), and the reconnect after an agent update dials exactly that
//     address. On main both were the bare "127.0.0.1" key; without its own
//     pin P+1 would be a first use, its plaintext rung open.
//
// A vm:<name> answered endpoint is never recorded: that port keys as the VM
// while it runs. Nil for every other key, including every non-loopback one,
// and when dialAddr is "" — a connection some fallback substituted for the
// dial answered nowhere that was dialled.
func endpointPinKeys(pinKey, dialAddr, answeredAddr string) []string {
	if dialAddr == "" || !recordsEndpointPins(pinKey) {
		return nil
	}
	keys := vmEndpointPinKeys(pinKey, dialAddr)
	if answeredAddr == "" {
		return keys
	}
	answered := pinKeyForAddr(answeredAddr)
	if answered == pinKey || legacyLoopbackPinKey(answered) == "" || slices.Contains(keys, answered) {
		return keys
	}
	return append(keys, answered)
}

// recordsEndpointPins reports whether a connection judged under key records
// its identity at loopback endpoints too: a VM's vm:<name> key, or a
// port-qualified loopback key. Never a non-loopback key.
func recordsEndpointPins(key string) bool {
	return strings.HasPrefix(key, vmDeviceIDPrefix) || legacyLoopbackPinKey(key) != ""
}

// identityPinKey is the key enforceDeviceIdentity judges a connection to
// hostname against: hostname's own pin when it has one, else a pin its
// loopback endpoint (a port-qualified loopback key — never vm:<name>) still
// has under the bare host. It is the post-connect half of pinCandidateKeys'
// legacy candidate, so the dial and the pin check agree on which pin governs.
func identityPinKey(cfg *config.Config, hostname string) string {
	if _, ok := cfg.DevicePinFor(hostname); ok {
		return hostname
	}
	if legacy := legacyLoopbackPinKey(hostname); legacy != "" {
		if _, ok := cfg.DevicePinFor(legacy); ok {
			return legacy
		}
	}
	return hostname
}

// retireLegacyLoopbackPins drops the bare loopback pin of the host key's
// connection reached — "127.0.0.1" for 127.0.0.1:PORT and for a VM's vm:<name>
// (its forward listens on vmForwardHost only), "localhost" for localhost:PORT —
// when it names the same device as the pin now filed under key. Such a pin
// identifies exactly one device, which is now pinned under its own key; left
// behind, it would only constrain every OTHER endpoint of that host to that
// device — the collision per-endpoint keys exist to end.
//
// A bare pin under any other spelling is left alone even when it names the
// same device: a connection at 127.0.0.1 proves nothing about localhost, which
// can resolve to ::1 — another socket, which something else can listen on —
// and retiring that pin would leave localhost:PORT unpinned on the very port
// the device uses. So is a bare pin naming a different device, or no device
// (no asset id). applyDeviceIdentity calls this only for a certificate that
// named its asset: an asset-less match proves an organisation, not which
// device answered.
func retireLegacyLoopbackPins(cfg *config.Config, key string) bool {
	host := legacyLoopbackPinKey(key)
	if strings.HasPrefix(key, vmDeviceIDPrefix) {
		host = vmForwardHost
	}
	if host == "" {
		return false
	}
	current, ok := cfg.DevicePinFor(key)
	if !ok {
		return false
	}
	legacy, ok := cfg.DevicePinFor(host)
	if !ok || !sameConfigPinIdentity(legacy, current) {
		return false
	}
	cfg.ClearDevicePin(host)
	return true
}

// isLoopbackHost reports whether host names this machine. "localhost" is
// matched by name because net.ParseIP does not resolve it, and it is the form
// people actually type at a forwarded port.
func isLoopbackHost(host string) bool {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = normalizeLoopbackHost(host)
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// expectedIdentityFor returns the asset identity pinned for pinKey, or nil when
// the host is unpinned or its pin predates asset ids. Nil means "first contact
// is permissive" — the posture that keeps legacy and unprovisioned devices
// working; the pin is written on the first successful connect.
//
// The lookup goes through lookupPin, so a pin recorded under any of the
// device's names — hostname, mesh name, or display name — is honoured here.
func expectedIdentityFor(pinKey string) *certs.WendyIdentity {
	pin, _, ok := governingPin(pinKey)
	if !ok {
		return nil
	}
	return expectedIdentityForPin(pin)
}

// expectedIdentityForPin turns a stored pin into the identity a handshake must
// match, or nil when the pin names no device.
//
// A recorded tenant SPIFFE principal is preferred over the (org, asset) pair:
// certs.WendyIdentity.SameEntity compares principals when both sides carry one,
// and a pki-core-issued leaf carries no org at all, so comparing the pair alone
// would compare an id against a blank org and refuse the very device the pin
// was written from.
func expectedIdentityForPin(pin config.DevicePin) *certs.WendyIdentity {
	if pin.Principal != "" {
		if id, err := certs.ParsePrincipal(pin.Principal); err == nil {
			return &id
		}
	}
	if pin.AssetID == "" {
		return nil
	}
	return &certs.WendyIdentity{OrgID: int32(pin.OrgID), EntityType: certs.EntityAsset, EntityID: pin.AssetID}
}

// pinCandidateKeys returns the keys a pin for pinKey may have been recorded
// under, most-specific first: the name the caller dialed, then — from the
// discovery-cache entry whose hostname matches it — that device's mesh name and
// display name. One device answers to all three, and different surfaces record
// under different ones: enforceDevicePin records the dialed mDNS hostname
// (wendyos-calm-zinnia), cloud seeding records the asset name the roster
// carries (calm-zinnia, which is the cache's display name), and mesh dials name
// the device by its mesh name. A cloud Asset carries only {Id, Name} — no
// hostname — so the cloud side cannot record under the dial key, and the
// reconciliation has to happen here on lookup.
//
// Duplicates are dropped under the same normalisation the pin store applies, so
// an alias that is only a cosmetic variant of the dialed name never produces a
// second, redundant candidate, and empty names are never looked up.
//
// Reading discovery-derived names to pick a key is safe FOR LOOKUP in a way
// that reading them to make a trust decision would not be: consulting an extra
// candidate can only ever FIND a pin, never discard one, so an attacker-chosen
// alias can at most impose a constraint that the real device fails — a stricter
// outcome, not a bypass. The trust decision itself stays on the certificate.
//
// That justification is about lookup and does not carry to clearing. A caller
// that DELETES every candidate turns the same attacker-chosen alias into a way
// to drop another device's pin, which is a bypass — see clearPinsGoverning,
// which consumes this list but removes an alias's pin only when it names the
// same device as the governing one.
//
// A port-qualified loopback key also lists its bare host (see
// legacyLoopbackPinKey): consulting an extra key can only find a pin, never
// discard one.
func pinCandidateKeys(pinKey string) []string {
	if pinKey == "" {
		return nil
	}
	candidates := []string{pinKey}
	seen := map[string]bool{normalizeMDNSHost(pinKey): true}
	// A port-qualified loopback key's pin may still sit under the bare host,
	// from before loopback endpoints were keyed by port (see pinKeyForAddr).
	// Consulted after the endpoint's own key, so an existing pin keeps
	// governing until enforceDeviceIdentity moves it.
	if legacy := legacyLoopbackPinKey(pinKey); legacy != "" {
		candidates = append(candidates, legacy)
		seen[normalizeMDNSHost(legacy)] = true
	}
	// Best effort by construction: cachedDeviceHostEntry reports false for an
	// unopenable cache, an unreadable one, and a plain miss alike, and every
	// one of those degrades to exactly the candidates above.
	entry, ok := cachedDeviceHostEntry(pinKey)
	if !ok {
		return candidates
	}
	for _, alias := range []string{entry.MeshName, entry.DisplayName} {
		norm := normalizeMDNSHost(alias)
		if norm == "" || seen[norm] {
			continue
		}
		seen[norm] = true
		candidates = append(candidates, alias)
	}
	return candidates
}

// lookupPin resolves the pin governing pinKey across every candidate key,
// returning it, the key it was found under, and whether there was one.
//
// A cloud-sourced pin outranks a LAN-sourced one wherever each sits in the
// candidate order: cloud learned the binding from the org's cloud over an
// authenticated session, while a LAN pin records only what some host on the
// local network presented. Among pins of equal source the earliest candidate
// wins, which keeps the dialed name authoritative over an alias.
//
// That precedence yields to one invariant: an asset-less cloud pin never
// displaces an incumbent carrying an asset id. Cloud authority decides WHICH
// binding to believe, and applying it is worth nothing if it leaves
// expectedIdentityFor with no binding to enforce — a host constrained to one
// asset would become constrained to none, which is exactly the same-CA-host
// redirect this path exists to stop. An asset-less pin is a real state, not a
// hypothetical: config's EvaluateDevicePin carries a dedicated branch for a
// cloud-sourced pin with no asset id.
//
// Because the search only ever adds keys, a host pinned under pinKey stays
// pinned no matter what the cache says or fails to say — the property that lets
// the cache be consulted best-effort without a cache outage quietly switching
// enforcement off.
func lookupPin(cfg *config.Config, pinKey string) (config.DevicePin, string, bool) {
	if cfg == nil {
		return config.DevicePin{}, "", false
	}
	var best config.DevicePin
	var bestKey string
	found := false
	for _, key := range pinCandidateKeys(pinKey) {
		pin, ok := cfg.DevicePinFor(key)
		if !ok {
			continue
		}
		if !found {
			best, bestKey, found = pin, key, true
			continue
		}
		if best.Source != config.PinSourceCloud && pin.Source == config.PinSourceCloud &&
			(pin.AssetID != "" || best.AssetID == "") {
			best, bestKey = pin, key
		}
	}
	return best, bestKey, found
}

// errDeviceIdentityRefused is what every refusal in this package answers
// errors.Is to. It exists so a caller can tell "the device you asked for is not
// what answered" apart from "nothing answered" WITHOUT matching on message
// text: the picker's Bluetooth fallback turns on exactly that distinction, and
// a fallback that silently reaches the rejected device over a second transport
// is the refusal undone.
var errDeviceIdentityRefused = errors.New("device identity refused")

// deviceIdentityRefusalError carries a refusal's full user-facing text while
// staying recognisable to errors.Is. The text is the whole message rather than
// a wrap so the refusals read exactly as they did before this type existed.
type deviceIdentityRefusalError struct {
	msg        string
	diagnostic *devicePinDiagnostic
}

func (e *deviceIdentityRefusalError) Error() string { return e.msg }

func (e *deviceIdentityRefusalError) Is(target error) bool {
	return target == errDeviceIdentityRefused
}

// refuseIdentity builds a refusal that errors.Is(err, errDeviceIdentityRefused)
// recognises. Every refusal raised because the wrong device answered — here and
// in device_pin.go — must go through it.
func refuseIdentity(format string, args ...any) *deviceIdentityRefusalError {
	return &deviceIdentityRefusalError{msg: fmt.Sprintf(format, args...)}
}

// identityRefusal renders a wrong-device rejection. Same text in interactive,
// JSON, and non-interactive modes — there is deliberately no "trust this?"
// prompt, because a MITM warning that can be dismissed gets dismissed.
func identityRefusal(pinKey string, im *certs.IdentityMismatchError) error {
	got := "no wendy identity"
	if im.GotAsset != "" {
		got = fmt.Sprintf("asset %s in organization %d", im.GotAsset, im.GotOrg)
	}
	return refuseDevicePin(devicePinDiagnostic{
		hostname: pinKey,
		heading:  fmt.Sprintf("Connection blocked: device %q identity changed.", pinKey),
		details:  fmt.Sprintf("Saved: asset %s in organization %d\nNow:   %s", im.WantAsset, im.WantOrg, got),
	})
}

// errNoAuthenticatedEndpoint is what a "nothing answered" refusal answers
// errors.Is to.
//
// It is deliberately NOT errDeviceIdentityRefused. That sentinel means "the
// device you asked for is not what answered", and the picker's Bluetooth
// fallback turns on exactly that distinction: a fallback that reaches a
// *rejected* device over a second transport is the refusal undone. A device
// that could not be reached over IP at all is the opposite case — it is
// precisely when trying another transport is the right thing to do — so
// filing it under the same sentinel would suppress the fallback for the one
// population that needs it.
var errNoAuthenticatedEndpoint = errors.New("no authenticated endpoint answered")

// noAuthenticatedEndpointError carries the full user-facing text while staying
// recognisable to errors.Is, mirroring deviceIdentityRefusalError.
type noAuthenticatedEndpointError struct{ msg string }

func (e *noAuthenticatedEndpointError) Error() string { return e.msg }

func (e *noAuthenticatedEndpointError) Is(target error) bool {
	return target == errNoAuthenticatedEndpoint
}

// blocksUnauthenticatedFallback reports whether err forbids reaching this device
// over a transport that enforces nothing — today the picker's Bluetooth
// fallback, where attemptBLEConnect sets no ExpectedIdentity and
// enforceSelectedDevicePin is a no-op.
//
// BOTH refusals block it, for two different reasons, and keeping them distinct
// error types is exactly why this predicate has to exist rather than the gate
// matching one sentinel:
//
//   - errDeviceIdentityRefused: the wrong device answered. Reaching it over a
//     second transport is the refusal undone.
//   - errNoAuthenticatedEndpoint: nothing authenticated answered a host we hold
//     a PIN for — meaning we have reached this device over mTLS before and know
//     it authenticates. An unauthenticated BLE peer advertising its name is not
//     evidence of being that device, and BLE checks nothing, so accepting one
//     here would be the downgrade the pin exists to prevent. The error is
//     raised only under target.pinned(), so an unpinned device that simply did
//     not answer still gets its fallback — which is what that fallback is for.
//
// Splitting the sentinels is about what the user is TOLD (an unreachable device
// must not be told its identity is suspect, nor handed an `unpin` command) and
// about letting a caller tell the two facts apart. It is not licence to route a
// pinned device onto an unauthenticated transport.
func blocksUnauthenticatedFallback(err error) bool {
	return errors.Is(err, errDeviceIdentityRefused) || errors.Is(err, errNoAuthenticatedEndpoint)
}

// pinnedHostNoAuthenticatedEndpointError renders the honest version of what
// used to be a single message shared with genuine identity mismatches: every
// candidate address was dialed and none produced an authenticated endpoint, so
// no certificate ever arrived and no identity was ever compared against the
// pin.
//
// The pin is therefore not evidence of anything here, and this message
// deliberately contains no `wendy device unpin` command. Unpinning is the only
// irreversible action available at this prompt — it discards a trust binding
// that took a successful mTLS connection to establish — and the message that
// used to appear here recommended it for what is usually stale routing. On a
// network that rotates DHCP leases that is a standing invitation to throw the
// binding away on every lease change.
//
// It names every address tried and how many of each family, because "the CLI
// only ever tried one of the several addresses this name resolves to" is the
// fact that made the original report take hours to diagnose.
// certSeen must be true when ANY address rejected our certificate, so the
// message never claims nothing was compared when something was.
func pinnedHostNoAuthenticatedEndpointError(pinKey string, candidates []string, attempts []mtlsAttemptError, certSeen bool) error {
	var where string
	if tried := describeDialAttempts(attempts); len(tried) > 0 {
		where = fmt.Sprintf("at any address it resolves to: %s (each tried on both its plaintext and mTLS port)",
			strings.Join(tried, ", "))
	} else {
		// No mTLS rung ran at all — the CLI holds no usable client certificate
		// for this device. Still not an identity problem, so still no unpin.
		where = fmt.Sprintf("at %s, and no authenticated connection could be attempted (no usable client certificate for this device)",
			strings.Join(candidates, ", "))
	}
	// The diagnosis clause has to stay honest. If some address DID present a
	// certificate we refused, "no identity was compared" would be false, and
	// this message is the one the user acts on.
	diagnosis := "The pin is intact and no device identity was compared, so this is a reachability problem rather than an identity one"
	if certSeen {
		diagnosis = "The pin is intact; one address did present a certificate that was refused, so check the certificate diagnostics above as well as reachability"
	}
	v4, v6 := addrFamilyCounts(candidates)
	return &noAuthenticatedEndpointError{msg: fmt.Sprintf(
		"device %q is pinned to an enrolled identity and no authenticated endpoint answered %s. "+
			"%s — unpinning the device is not the fix. "+
			"Tried %s; if the device is only reachable over one address family, check that its agent is listening and that stale DNS/mDNS records are not holding the CLI to an address the device no longer has.",
		pinKey, where, diagnosis, describeAddrFamilies(v4, v6))}
}

// describeDialAttempts renders one entry per ADDRESS tried, in the order tried,
// carrying that address's last failure. Attempts arrive per address *and port*
// (the ladder tries the plaintext port and the mTLS port), so they are folded by
// host: six entries for three addresses would read as though the CLI had tried
// twice as many places as it did, and the count would then disagree with the
// family summary the user is being asked to act on.
func describeDialAttempts(attempts []mtlsAttemptError) []string {
	var order []string
	lastReason := map[string]string{}
	for _, a := range attempts {
		host := hostOnly(a.addr)
		if host == "" {
			continue
		}
		if _, seen := lastReason[host]; !seen {
			order = append(order, host)
		}
		if a.err != nil {
			lastReason[host] = condenseDialReason(a.err.Error())
		}
	}
	out := make([]string, 0, len(order))
	for _, host := range order {
		if reason := lastReason[host]; reason != "" {
			out = append(out, fmt.Sprintf("%s (%s)", host, reason))
		} else {
			out = append(out, host)
		}
	}
	return out
}

// condenseDialReason reduces a gRPC dial error to the one clause that tells the
// user what happened.
//
// The raw text arrives as
// `rpc error: code = Unavailable desc = connection error: desc = "transport:
// Error while dialing: dial tcp 10.0.0.5:50051: connect: connection refused"` —
// the two useful words are at the very END, so truncating the front (which is
// what a plain length cap does) throws away exactly the part being quoted. With
// several addresses to show, that produced a list of identical ellipses.
func condenseDialReason(reason string) string {
	flat := strings.Join(strings.Fields(reason), " ")
	// Timeouts are rewritten rather than quoted. connectToAgent classifies the
	// error it gets back by SUBSTRING (isReachabilityTimeoutError matches
	// "i/o timeout" / "deadline exceeded" / "connection timed out") to decide
	// whether to print "connection timed out; retrying" and re-run the whole
	// connect. Quoting the transport's own wording inside this message made a
	// black-holed address — the exact case this error exists for — trigger two
	// spurious full re-walks under a misleading banner. The wording below says
	// the same thing to a human and matches none of those classifiers.
	for _, timeout := range []string{"context deadline exceeded", "i/o timeout", "connection timed out"} {
		if strings.Contains(flat, timeout) {
			return "no response before the handshake budget elapsed"
		}
	}
	// Ordered so a more specific phrase wins over one it contains.
	for _, phrase := range []string{
		"connection refused",
		"no route to host",
		"host is unreachable",
		"network is unreachable",
		"connection reset by peer",
		"no such host",
		"tls: ",
	} {
		if idx := strings.Index(flat, phrase); idx >= 0 {
			return strings.TrimSuffix(strings.TrimSpace(flat[idx:]), `"`)
		}
	}
	const maxReason = 90
	if runes := []rune(flat); len(runes) > maxReason {
		return string(runes[:maxReason]) + "…"
	}
	return flat
}

// hostOnly strips the port from a host:port. Input without a port is returned
// trimmed rather than discarded — a bare host is legitimate here — so the ""
// result this can produce means only "nothing usable was given".
func hostOnly(addr string) string {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return strings.TrimSpace(addr)
	}
	return host
}

// addrFamilyCounts counts how many of addrs are IPv4 and how many IPv6, folding
// duplicates by host so the same address on two ports counts once.
//
// The fold keeps the zone: fe80::1%en0 and fe80::1%en1 are two candidates, dialed
// over two different interfaces, and either can fail while the other works — so
// the tally has to agree with describeDialAttempts, which lists them separately.
// Only the parse strips the zone, since net.ParseIP rejects a zoned literal.
func addrFamilyCounts(addrs []string) (v4, v6 int) {
	seen := map[string]bool{}
	for _, addr := range addrs {
		host := hostOnly(addr)
		if host == "" || seen[host] {
			continue
		}
		ip := net.ParseIP(stripZone(host))
		if ip == nil {
			continue
		}
		seen[host] = true
		if ip.To4() != nil {
			v4++
		} else {
			v6++
		}
	}
	return v4, v6
}

// describeAddrFamilies renders the family tally as a clause naming both
// families only when both were actually tried — "0 IPv6" invites the reader to
// debug an IPv6 problem that never happened.
func describeAddrFamilies(v4, v6 int) string {
	noun := func(n int) string {
		if n == 1 {
			return "address"
		}
		return "addresses"
	}
	switch {
	case v4 > 0 && v6 > 0:
		return fmt.Sprintf("%d IPv4 and %d IPv6 %s", v4, v6, noun(v4+v6))
	case v4 > 0:
		return fmt.Sprintf("%d IPv4 %s", v4, noun(v4))
	case v6 > 0:
		return fmt.Sprintf("%d IPv6 %s", v6, noun(v6))
	default:
		return "no resolved addresses"
	}
}

// spkiRefusal renders the OTHER pin's rejection: the SPKI store's, which is
// keyed by the certificate's own asset URN rather than by hostname and fires
// when a device's public key changes while its pinned certificate is still
// valid.
//
// Without this the store's PinMismatchError reached the user only as whatever
// text survived gRPC's handshake wrapper — a message that names neither the
// host as the user knows it nor any way out, leaving hand-editing
// known_devices.json as the only recovery. The SPKI store has no hostname in
// it, so the key comes from the dial: pinKey when there is one, the store's own
// display name otherwise.
//
// The command it prints names pm.Key — the SPKI store's own key — not the
// hostname, even though the hostname is what the user recognises. Naming the
// hostname is what made this refusal a dead end for the two populations that
// hit it most: the picker and `wendy device list` dial the IP first, so the
// message named an IP that keys nothing and whose asset nothing in local state
// can derive; and agents that never advertise `orgid` (the Swift macOS agent,
// Linux agents before 2026-07-18) leave the discovery cache with no identity to
// derive it from either. pm.Key is in hand at the moment of refusal and is
// exactly what the store is keyed by, so `wendy device unpin <urn>` reaches the
// entry directly — and clears any config pin naming the same identity with it.
func spkiRefusal(pinKey string, pm *devicepin.PinMismatchError) error {
	named := pinKey
	if named == "" {
		named = pm.DisplayName
	}
	// A store key is always present in a real mismatch; fall back to the name
	// only so a hand-built error still points at something.
	unpinArg := pm.Key
	if unpinArg == "" {
		unpinArg = named
	}
	return refuseIdentity(
		"device %q presented a different certificate key than the one pinned for %s (pinned %s, now %s); refusing to connect — if its certificate was legitimately reissued, run 'wendy device unpin %s'",
		named, pm.Key, pm.Want, pm.Got, unpinArg)
}
