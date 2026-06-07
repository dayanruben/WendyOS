package containerd

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strings"

	"go.uber.org/zap"

	"github.com/wendylabsinc/wendy/go/internal/shared/appconfig"
)

const (
	cniPluginDir = "/opt/cni/bin"
	cniStateDir  = "/run/wendy/cni"
)

// cniResult is a minimal subset of the CNI ADD result.
type cniResult struct {
	IPs []struct {
		Address string `json:"address"` // "10.89.X.Y/24"
	} `json:"ips"`
}

// netnsPathPattern accepts the two netns path forms used in this package:
//   - /proc/{pid}/ns/net  — direct procfs reference
//   - /proc/self/fd/{n}   — fd-anchored reference (prevents PID-reuse races)
var netnsPathPattern = regexp.MustCompile(`^(/proc/\d+/ns/net|/proc/self/fd/\d+)$`)

// containerIDPattern is an allowlist for CNI containerID values. Wendy
// container IDs are either a bare appID or "{appID}@{serviceName}", so valid
// characters are letters, digits, dot, underscore, hyphen, and '@'.
// This allowlist replaces a previous denylist that blocked only a few
// characters; an allowlist is more robust against unforeseen CNI plugin
// behaviour (SOC2-CC6, ISO27001-A.8, NIST-SI-10).
var containerIDPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._@-]{0,319}$`)

// allocateSubnet deterministically maps an appID to a /28 subnet within
// 10.0.0.0/8 using three bytes of a SHA-256 digest (~1M possible subnets).
// Birthday-paradox collision probability: ~0.05% at 30 apps, ~0.5% at 100.
// The SHA-256 digest ensures uniform distribution; it does not provide
// resistance against a caller who can iterate appIDs to find a collision
// (SOC2-CC6, ISO27001-A.8, NIST-SC-7).
func allocateSubnet(appID string) string {
	h := sha256.Sum256([]byte(appID))
	// Use three bytes from the digest: second octet, third octet, /28 boundary.
	b2 := h[0]
	b3 := h[1]
	b4 := (h[2] & 0xf) << 4 // /28 boundary: 0, 16, 32, …, 240
	return fmt.Sprintf("10.%d.%d.%d/28", b2, b3, b4)
}

// bridgeName returns a Linux network interface name for the app's CNI bridge.
// The kernel limit is 15 chars (IFNAMSIZ-1). Short appIDs that fit are embedded
// directly; longer ones fall back to an 8-hex-digit SHA-256 prefix for uniform
// distribution (birthday collision probability ~0.12% at 100 apps).
func bridgeName(appID string) string {
	const prefix = "wendy-br-"
	if len(prefix)+len(appID) <= 15 {
		return prefix + appID
	}
	h := sha256.Sum256([]byte(appID))
	return fmt.Sprintf("wendy-%08x", binary.BigEndian.Uint32(h[:4]))
}

// validateCNIInputs provides defence-in-depth validation of the values that
// reach the CNI exec environment, guarding against any future caller that
// bypasses the RPC-layer ValidateAppID check (SOC2-CC6, NIST-SI-10).
func validateCNIInputs(appID, containerID, netnsPath string) error {
	if err := appconfig.ValidateAppID(appID); err != nil {
		return fmt.Errorf("CNI: %w", err)
	}
	if !containerIDPattern.MatchString(containerID) {
		return fmt.Errorf("CNI: containerID %q does not match allowed pattern (letters, digits, '.', '_', '@', '-'; 1–320 chars)", containerID)
	}
	if !netnsPathPattern.MatchString(netnsPath) {
		return fmt.Errorf("CNI: netnsPath %q does not match expected pattern", netnsPath)
	}
	return nil
}

// buildBridgeCNIConfig returns the JSON config string for the CNI bridge plugin.
func buildBridgeCNIConfig(appID, subnet string) string {
	cfg := map[string]interface{}{
		"cniVersion": "0.4.0",
		"name":       "wendy-" + appID,
		"type":       "bridge",
		"bridge":     bridgeName(appID), // capped at 15 chars (IFNAMSIZ-1)
		"isGateway":  true,
		"ipMasq":     true,
		"ipam": map[string]interface{}{
			"type":    "host-local",
			"subnet":  subnet,
			"dataDir": cniStateDir + "/" + appID,
		},
	}
	b, _ := json.Marshal(cfg)
	return string(b)
}

// CNIAdd calls the CNI bridge plugin ADD for a container, returning its
// assigned IP address. netnsPath is the container's network namespace path
// (e.g. /proc/{pid}/ns/net).
func (c *Client) CNIAdd(ctx context.Context, appID, containerID, netnsPath string) (string, error) {
	if err := validateCNIInputs(appID, containerID, netnsPath); err != nil {
		return "", err
	}
	subnet := allocateSubnet(appID)
	cfgJSON := buildBridgeCNIConfig(appID, subnet)

	cmd := exec.CommandContext(ctx, cniPluginDir+"/bridge")
	cmd.Stdin = strings.NewReader(cfgJSON)
	cmd.Env = []string{
		"CNI_COMMAND=ADD",
		"CNI_CONTAINERID=" + containerID,
		"CNI_NETNS=" + netnsPath,
		"CNI_IFNAME=eth0",
		"CNI_PATH=" + cniPluginDir,
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("CNI ADD failed for %s/%s: %w (stderr: %s)", appID, containerID, err, stderr.String())
	}

	var result cniResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		return "", fmt.Errorf("parsing CNI ADD result: %w", err)
	}
	if len(result.IPs) == 0 {
		return "", fmt.Errorf("CNI ADD returned no IPs for %s/%s", appID, containerID)
	}
	ip, _, _ := strings.Cut(result.IPs[0].Address, "/")
	c.logger.Info("CNI ADD: assigned IP",
		zap.String("app_id", appID),
		zap.String("container_id", containerID),
		zap.String("ip", ip))
	return ip, nil
}

// writeHostsFile writes a hosts-format file at path with entries for each
// service name → IP mapping. Always includes 127.0.0.1 localhost.
// Entries are written in sorted order for determinism.
func writeHostsFile(path string, serviceIPs map[string]string) error {
	var sb strings.Builder
	sb.WriteString("127.0.0.1\tlocalhost\n")
	names := make([]string, 0, len(serviceIPs))
	for n := range serviceIPs {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		fmt.Fprintf(&sb, "%s\t%s\n", serviceIPs[name], name)
	}
	return os.WriteFile(path, []byte(sb.String()), 0o644)
}

// CNIDel calls the CNI bridge plugin DEL to release a container's IP.
// Errors are logged as warnings but not returned — DEL is best-effort.
func (c *Client) CNIDel(ctx context.Context, appID, containerID, netnsPath string) error {
	if err := validateCNIInputs(appID, containerID, netnsPath); err != nil {
		c.logger.Warn("CNI DEL skipped: invalid inputs", zap.Error(err))
		return nil
	}
	subnet := allocateSubnet(appID)
	cfgJSON := buildBridgeCNIConfig(appID, subnet)

	cmd := exec.CommandContext(ctx, cniPluginDir+"/bridge")
	cmd.Stdin = strings.NewReader(cfgJSON)
	cmd.Env = []string{
		"CNI_COMMAND=DEL",
		"CNI_CONTAINERID=" + containerID,
		"CNI_NETNS=" + netnsPath,
		"CNI_IFNAME=eth0",
		"CNI_PATH=" + cniPluginDir,
	}
	if err := cmd.Run(); err != nil {
		c.logger.Warn("CNI DEL failed (non-fatal)",
			zap.String("app_id", appID),
			zap.String("container_id", containerID),
			zap.Error(err))
	}
	return nil
}
