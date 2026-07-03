//go:build linux

package main

import (
	"github.com/containernetworking/cni/pkg/skel"
	"github.com/containernetworking/cni/pkg/version"

	"github.com/wendylabsinc/wendy/go/internal/agent/cni/bridge"
	"github.com/wendylabsinc/wendy/go/internal/agent/cni/hostlocal"
)

// cniSupportedVersions lists the CNI spec versions the vendored bridge and
// host-local plugins advertise. Keep in sync with the "cniVersion" field
// buildBridgeCNIConfig writes in internal/agent/containerd/cni.go.
var cniSupportedVersions = version.PluginSupports("0.3.0", "0.3.1", "0.4.0", "1.0.0")

// runCNIPlugin dispatches to the vendored bridge or host-local CNI plugin
// logic via the upstream CNI skel package, which reads CNI_COMMAND and the
// NetConf JSON from the environment/stdin per the CNI spec and calls
// os.Exit itself. It only returns (with a nonzero code) if name does not
// match a known plugin, which should be unreachable given cniPluginName's
// contract.
func runCNIPlugin(name string) int {
	switch name {
	case "bridge":
		skel.PluginMainFuncs(skel.CNIFuncs{
			Add:    bridge.CmdAdd,
			Check:  bridge.CmdCheck,
			Del:    bridge.CmdDel,
			Status: bridge.CmdStatus,
		}, cniSupportedVersions, "wendy-agent CNI bridge (vendored containernetworking/plugins)")
	case "host-local":
		skel.PluginMainFuncs(skel.CNIFuncs{
			Add:   hostlocal.CmdAdd,
			Check: hostlocal.CmdCheck,
			Del:   hostlocal.CmdDel,
		}, cniSupportedVersions, "wendy-agent CNI host-local IPAM (vendored containernetworking/plugins)")
	}
	return 1
}
