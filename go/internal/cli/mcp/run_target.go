package mcp

import (
	"fmt"
	"net/netip"
	"slices"
	"strconv"
	"strings"
)

// runNoCloudFallbackEnv tells the spawned `wendy run` not to answer a failed
// direct connect with a cloud device of the same name
// (commands.cloudFallbackDisabled). The run tool deploys to the device this
// session selected, or fails.
const runNoCloudFallbackEnv = "WENDY_RUN_NO_CLOUD_FALLBACK"

// runChildEnvironment is the environment of the spawned `wendy run`.
func runChildEnvironment(base []string, target commandTarget) []string {
	env := append(slices.Clone(base), runNoCloudFallbackEnv+"=1")
	if target.Selector != "" && target.Transport == "cloud" {
		env = runEnvironment(env, target)
	}
	return env
}

// mcpDefaultAgentPort is the CLI's plaintext agent port; the CLI derives the
// mTLS port from it, exactly as for the startup default device.
const mcpDefaultAgentPort = 50051

// withDefaultAgentPort renders a bare host as host:port.
func withDefaultAgentPort(host string) string {
	if addr, err := netip.ParseAddr(host); err == nil && addr.Is6() {
		return "[" + host + "]:" + strconv.Itoa(mcpDefaultAgentPort)
	}
	return host + ":" + strconv.Itoa(mcpDefaultAgentPort)
}

// runNextStep is the verification step for a deploy to target when that is not
// the session's connected target. container_list and telemetry_logs inspect
// the session's device, so the agent must connect to the deployed one first.
// It returns "" when target is the connected target.
func (s *mcpServer) runNextStep(target commandTarget) string {
	_, _, session := s.connectionSnapshot()
	deployed := runTargetIdentity(target)
	connect := fmt.Sprintf("device_connect(address=%q)", deployed)
	if target.Transport == "cloud" && target.Selector == "" {
		connect = fmt.Sprintf("cloud_connect(device_name=%q)", target.Device)
		if target.CloudGRPC != "" {
			connect = fmt.Sprintf("cloud_connect(device_name=%q, cloud_grpc=%q)", target.Device, target.CloudGRPC)
		}
	}
	const verify = " before container_list or telemetry_logs, then test the app's health endpoint or ROS interface. Deployment alone does not verify behavior."
	switch connected := runTargetIdentity(session); {
	case connected == "":
		return fmt.Sprintf("This session is not connected to %s. Call %s%s", deployed, connect, verify)
	case connected != deployed:
		return fmt.Sprintf("run deployed to %s, but this session is connected to %s. Call %s%s", deployed, connected, connect, verify)
	}
	return ""
}

// runTargetIdentity is the device a target replays to.
func runTargetIdentity(target commandTarget) string {
	if target.Selector != "" {
		return target.Selector
	}
	return target.Device
}

// runFailureNextStep points a failed explicit bare device name at
// cloud_connect. device_list reports cloud devices by name, but the spawned
// CLI resolves an explicit device only directly (it never falls back to the
// cloud), so a cloud name fails there with a resolution error.
func runFailureNextStep(target commandTarget, code errorCode) string {
	device := target.Device
	if target.Transport != "selector" || code != errCodeInternal || strings.Contains(device, ":") {
		return ""
	}
	return fmt.Sprintf("If the error above is a connection or name-resolution failure and %q is a cloud device (a device_list row with source \"cloud\"), call cloud_connect(device_name=%q), then run again without device. For a LAN device, pass its host:port address from device_list.", device, device)
}
