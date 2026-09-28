package mcp

import (
	"net/netip"
	"slices"
	"strconv"
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
