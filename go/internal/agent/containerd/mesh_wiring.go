package containerd

import (
	"fmt"
	"net"
	"os"
	"path/filepath"

	"go.uber.org/zap"

	"github.com/wendylabsinc/wendy/go/internal/agent/hostnetwork"
	"github.com/wendylabsinc/wendy/go/internal/agent/mesh"
	"github.com/wendylabsinc/wendy/go/internal/shared/appconfig"
)

// meshResolvConfDir holds per-app resolv.conf files pointing meshed
// containers at the mesh DNS server on their bridge gateway.
const meshResolvConfDir = "/run/wendy/mesh"

// findMeshEntitlement returns the network entitlement with mode "mesh" from
// entitlements, if one is present. Apps without the mesh entitlement (no
// network entitlement, or network mode host/host-admin/none) get ok == false,
// which callers must treat as a complete no-op — mesh wiring must never run
// for a container that did not request it.
func findMeshEntitlement(entitlements []appconfig.Entitlement) (ent appconfig.Entitlement, ok bool) {
	for _, e := range entitlements {
		if e.Type == appconfig.EntitlementNetwork && e.Mode == "mesh" {
			return e, true
		}
	}
	return appconfig.Entitlement{}, false
}

// normalizeCIDR parses s as a CIDR and returns the canonical form (network
// address + prefix length) via (*net.IPNet).String(). This guards against a
// serviceCIDR with host bits set (e.g. "10.99.0.5/16") being passed as-is to
// `ip route replace` or iptables, which would silently narrow the intended
// match to a single mis-aligned network (C3a-review Minor #1).
func normalizeCIDR(s string) (string, error) {
	_, ipNet, err := net.ParseCIDR(s)
	if err != nil {
		return "", fmt.Errorf("parsing CIDR %q: %w", s, err)
	}
	return ipNet.String(), nil
}

// meshGateway derives the mesh gateway address for appID: the first host
// address (".1") of the /28 subnet the CNI bridge plugin already allocated
// for this app. It calls allocateSubnet rather than deriving the subnet
// independently so it always agrees with the subnet the bridge actually
// configured as isGateway:true (see buildBridgeCNIConfig) — allocateSubnet
// is idempotent and returns the existing registry entry for an appID that
// already has one, so this never allocates a second, different subnet.
func meshGateway(appID string) (string, error) {
	subnet, err := allocateSubnet(appID)
	if err != nil {
		return "", fmt.Errorf("resolving mesh gateway subnet: %w", err)
	}
	_, ipNet, err := net.ParseCIDR(subnet)
	if err != nil {
		return "", fmt.Errorf("parsing allocated subnet %q: %w", subnet, err)
	}
	gateway := make(net.IP, len(ipNet.IP))
	copy(gateway, ipNet.IP)
	gateway[len(gateway)-1] |= 1
	return gateway.String(), nil
}

// meshEgressParams bundles the values needed to wire (or tear down) mesh
// egress for one container: the mesh gateway derived from the app's CNI
// subnet, and the serviceCIDR normalized once so every downstream call
// (SetMeshRoute, AddMeshRule, RemoveMeshRule) sees an identical string.
type meshEgressParams struct {
	gateway string
	cidr    string
}

// writeMeshResolvConfIn writes the resolv.conf for one app under baseDir and
// returns its path. Split from writeMeshResolvConf for testability: tests
// pass a temp directory instead of the real meshResolvConfDir, which lives
// under /run and is not writable in a non-root test sandbox.
func writeMeshResolvConfIn(baseDir, appID string) (string, error) {
	gw, err := meshGateway(appID)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(baseDir, appID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "resolv.conf")
	content := fmt.Sprintf("nameserver %s\noptions ndots:1\n", gw)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// writeMeshResolvConf writes appID's resolv.conf under the real
// meshResolvConfDir. Called from the container create path to produce the
// file bind-mounted into meshed containers at /etc/resolv.conf.
func writeMeshResolvConf(appID string) (string, error) {
	return writeMeshResolvConfIn(meshResolvConfDir, appID)
}

// resolveMeshEgress checks entitlements for a network/mesh entry and, if
// found, computes the gateway + normalized CIDR needed to wire mesh egress.
// It returns ok == false as a complete no-op signal for apps without the
// mesh entitlement — callers must not touch iptables/routes in that case.
func resolveMeshEgress(entitlements []appconfig.Entitlement, appID string) (params meshEgressParams, ok bool, err error) {
	ent, found := findMeshEntitlement(entitlements)
	if !found {
		return meshEgressParams{}, false, nil
	}
	cidr, err := normalizeCIDR(ent.ServiceCIDR)
	if err != nil {
		return meshEgressParams{}, true, fmt.Errorf("mesh entitlement has invalid serviceCIDR: %w", err)
	}
	gateway, err := meshGateway(appID)
	if err != nil {
		return meshEgressParams{}, true, fmt.Errorf("deriving mesh gateway: %w", err)
	}
	return meshEgressParams{gateway: gateway, cidr: cidr}, true, nil
}

// applyMeshEgress wires mesh egress for a just-started container: a route
// inside its netns toward the mesh service CIDR via the app's bridge
// gateway, and a host iptables rule scoping egress to exactly that CIDR for
// exactly this container's IP. It is a complete no-op — no route, no rule,
// no error — for any app without a network entitlement in mode "mesh"
// (SOC2-CC6: least privilege, opt-in only).
//
// This is fail-closed: if either the route or the rule cannot be installed,
// an error is returned and any partially-applied state is best-effort rolled
// back (the rule, if the route succeeded but the rule failed) so a meshed
// container never runs believing it has egress it does not actually have.
// The caller MUST fail container start on a non-nil error.
func (c *Client) applyMeshEgress(entitlements []appconfig.Entitlement, appID, netnsPath, ip string) error {
	params, ok, err := resolveMeshEgress(entitlements, appID)
	if err != nil {
		return fmt.Errorf("mesh egress: %w", err)
	}
	if !ok {
		return nil
	}

	if err := hostnetwork.SetMeshRoute(netnsPath, params.cidr, params.gateway); err != nil {
		return fmt.Errorf("mesh egress: setting route for app %q: %w", appID, err)
	}

	if err := hostnetwork.AddMeshRule(ip, params.cidr); err != nil {
		// The route lives in the container's netns and needs no explicit
		// cleanup here — it disappears automatically when the netns is torn
		// down as part of the failed start. Only the host-side iptables rule
		// could leak, and AddMeshRule failed to install it in the first
		// place, so there is nothing to remove. This branch exists so a
		// future change to what AddMeshRule partially applies on error does
		// not silently skip cleanup.
		if rmErr := hostnetwork.RemoveMeshRule(ip, params.cidr); rmErr != nil {
			c.logger.Warn("mesh egress: best-effort rule cleanup after failed AddMeshRule also failed",
				zap.String("app_id", appID), zap.String("ip", ip), zap.Error(rmErr))
		}
		return fmt.Errorf("mesh egress: adding iptables rule for app %q: %w", appID, err)
	}

	if err := hostnetwork.AddMeshRedirect(ip, params.cidr, mesh.ProxyPort); err != nil {
		// Roll back what we installed; the start must fail closed. The
		// DNS listener has not been touched yet at this point (EnsureListener
		// runs after this check succeeds), so there is nothing to release here.
		if rmErr := hostnetwork.RemoveMeshRule(ip, params.cidr); rmErr != nil {
			c.logger.Warn("mesh egress: rollback of ACCEPT rule after failed REDIRECT rule also failed",
				zap.String("app_id", appID), zap.String("ip", ip), zap.Error(rmErr))
		}
		return fmt.Errorf("mesh egress: adding REDIRECT rule for app %q: %w", appID, err)
	}

	// DNS is best-effort: without it, device-N.cloud.wendy.dev hostnames fail
	// to resolve but VIP literals still work over the REDIRECT/route wired
	// above, so a DNS listener failure must not fail container start.
	// EnsureListener is the last fallible step in this function — nothing
	// below can fail once it returns, so there is no rollback path in this
	// function that needs to release it; the paired ReleaseListener lives in
	// teardownMeshEgress, invoked unconditionally from stopOne for every
	// container that reaches a running state.
	if c.meshDNS != nil {
		if err := c.meshDNS.EnsureListener(params.gateway); err != nil {
			c.logger.Warn("mesh: DNS listener unavailable; device-N hostnames will not resolve",
				zap.String("app_id", appID), zap.String("gateway", params.gateway), zap.Error(err))
		}
	}

	c.logger.Info("mesh egress applied",
		zap.String("app_id", appID), zap.String("ip", ip), zap.String("service_cidr", params.cidr))
	return nil
}

// teardownMeshEgress removes the host iptables rule installed by
// applyMeshEgress for a stopping container. It is a no-op for apps without
// the mesh entitlement, and a no-op if ip is empty (the container's IP could
// not be recovered — see stopOne for how it is normally recovered from
// c.serviceIPs). The netns route needs no explicit cleanup: it is destroyed
// automatically when the network namespace is torn down with the container.
//
// Errors are logged but not returned — mirroring CNIDel's best-effort
// contract, so a host-side iptables failure never blocks a container stop.
func (c *Client) teardownMeshEgress(entitlements []appconfig.Entitlement, appID, ip string) {
	if ip == "" {
		return
	}
	ent, found := findMeshEntitlement(entitlements)
	if !found {
		return
	}
	cidr, err := normalizeCIDR(ent.ServiceCIDR)
	if err != nil {
		c.logger.Warn("mesh egress teardown: invalid serviceCIDR in entitlement, skipping rule removal",
			zap.String("app_id", appID), zap.Error(err))
		return
	}
	if err := hostnetwork.RemoveMeshRule(ip, cidr); err != nil {
		c.logger.Warn("mesh egress teardown: RemoveMeshRule failed (non-fatal)",
			zap.String("app_id", appID), zap.String("ip", ip), zap.Error(err))
	}
	if err := hostnetwork.RemoveMeshRedirect(ip, cidr, mesh.ProxyPort); err != nil {
		c.logger.Warn("mesh egress teardown: RemoveMeshRedirect failed (non-fatal)",
			zap.String("app_id", appID), zap.String("ip", ip), zap.Error(err))
	}
	// Pairs with the EnsureListener call in applyMeshEgress: every container
	// whose start reached EnsureListener (mesh entitlement present, route +
	// ACCEPT + REDIRECT all installed) ends up here exactly once via stopOne,
	// so this release exactly balances that one increment. If EnsureListener
	// itself failed (no refcount bump), ReleaseListener on an unknown gateway
	// is a documented no-op, so calling it unconditionally here is safe either
	// way. Recomputing the gateway is safe: meshGateway is idempotent and
	// returns the same value applyMeshEgress used to acquire the listener.
	if c.meshDNS != nil {
		if gw, err := meshGateway(appID); err == nil {
			c.meshDNS.ReleaseListener(gw)
		} else {
			c.logger.Warn("mesh egress teardown: could not derive gateway to release DNS listener (non-fatal, listener may leak until agent restart)",
				zap.String("app_id", appID), zap.Error(err))
		}
	}
}
