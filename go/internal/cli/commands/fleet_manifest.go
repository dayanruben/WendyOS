package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/wendylabsinc/wendy/go/internal/shared/appconfig"
	"github.com/wendylabsinc/wendy/go/internal/shared/models"
)

// fleetPeer is one discovered endpoint handed to a consuming component, matching
// the snapshot shape the platform injects under a discovers "as" env var:
//
//	[{"name":"camera-01","url":"http://10.0.0.4:8000/stream","group":"camera-*","status":"ready"}, ...]
type fleetPeer struct {
	Name   string `json:"name"`
	URL    string `json:"url"`
	Group  string `json:"group"`
	Status string `json:"status"`
}

// runFleetManifest deploys a fleet manifest (a wendy.json with a components map)
// across the LAN: each "group" component fans out to the devices matching its
// pattern, and each "central" component is deployed once — to --central, or, if
// omitted, its peer snapshot is printed so the operator can run it themselves.
func runFleetManifest(ctx context.Context, opts runOptions, projectCwd string, appCfg *appconfig.AppConfig, lan bool, central, cloudGRPC, brokerURL string, timeout time.Duration) error {
	if !lan {
		return fmt.Errorf("fleet manifests are LAN-only for now; re-run with --lan")
	}

	var edgeNames, centralNames []string
	for name, comp := range appCfg.Components {
		if comp.Target != nil && comp.Target.Central {
			centralNames = append(centralNames, name)
		} else {
			edgeNames = append(edgeNames, name)
		}
	}
	sort.Strings(edgeNames)
	sort.Strings(centralNames)

	// Resolve each edge component's group members once; reused for both the edge
	// deploy and the central component's discovery snapshot.
	edgeDevices := make(map[string][]models.LANDevice, len(edgeNames))
	for _, name := range edgeNames {
		comp := appCfg.Components[name]
		devices, err := lanGroupDevices(ctx, comp.Target.Group, timeout)
		if err != nil {
			return err
		}
		if len(devices) == 0 {
			return fmt.Errorf("component %q targets group %q, but no matching WendyOS devices were found on the LAN", name, comp.Target.Group)
		}
		edgeDevices[name] = devices
	}

	// 1. Edge components first, so their endpoints exist before a central
	//    component starts discovering them.
	for _, name := range edgeNames {
		comp := appCfg.Components[name]
		devices := edgeDevices[name]
		fmt.Printf("\n=== edge component %q → group %q (%d device(s)) ===\n", name, comp.Target.Group, len(devices))

		compCfg := componentAppConfig(appCfg, name, comp)
		compCwd := filepath.Join(projectCwd, comp.Context)
		targets := make([]fleetTarget, 0, len(devices))
		for _, dev := range devices {
			targets = append(targets, targetForDevice(dev))
		}
		if _, failures := deployToTargets(ctx, targets, compCwd, compCfg, opts); failures > 0 && !opts.keepGoing {
			return fmt.Errorf("edge component %q: %d device(s) failed", name, failures)
		}
	}

	// 2. Central components, with discovered peers injected as env vars.
	for _, name := range centralNames {
		comp := appCfg.Components[name]
		compCfg := componentAppConfig(appCfg, name, comp)
		compCwd := filepath.Join(projectCwd, comp.Context)

		env, err := discoveryEnv(comp, appCfg, edgeDevices)
		if err != nil {
			return fmt.Errorf("component %q: %w", name, err)
		}
		centralOpts := opts
		centralOpts.env = env
		// Env injection rides on CreateContainerRequest.Env, carried by the
		// registry (chunk-off) create path; force it so peers aren't dropped by
		// the chunk-diff RunContainer path.
		centralOpts.chunking = chunkingOff

		if central == "" {
			fmt.Printf("\n=== central component %q (no --central device) ===\n", name)
			printCentralInstructions(name, comp, env)
			continue
		}

		dev, err := resolveCentralDevice(ctx, central, timeout)
		if err != nil {
			return err
		}
		fmt.Printf("\n=== central component %q → %s ===\n", name, deviceShortName(dev))
		if derr := deployToTarget(ctx, targetForDevice(dev), compCwd, compCfg, centralOpts); derr != nil {
			return fmt.Errorf("central component %q on %s: %w", name, deviceShortName(dev), derr)
		}
	}

	fmt.Println("\nFleet deploy complete.")
	return nil
}

// componentAppConfig projects one fleet component into a standalone AppConfig
// the single-device deploy pipeline understands. The app id is namespaced by
// component so two components never collide on one device.
func componentAppConfig(appCfg *appconfig.AppConfig, name string, comp *appconfig.ComponentConfig) *appconfig.AppConfig {
	return &appconfig.AppConfig{
		AppID:        appCfg.AppID + "." + name,
		Version:      appCfg.Version,
		Platform:     appCfg.Platform,
		Entitlements: comp.Entitlements,
		Readiness:    comp.Readiness,
		Hooks:        comp.Hooks,
		Frameworks:   comp.Frameworks,
		Resources:    comp.Resources,
	}
}

// discoveryEnv builds the env vars injected into a central component for its
// declared discovers: each named var carries a JSON snapshot of the referenced
// component's live peer endpoints.
func discoveryEnv(central *appconfig.ComponentConfig, appCfg *appconfig.AppConfig, edgeDevices map[string][]models.LANDevice) ([]string, error) {
	var env []string
	for _, d := range central.Discovers {
		ref, ok := appCfg.Components[d.Component]
		if !ok {
			return nil, fmt.Errorf("discovers references unknown component %q", d.Component)
		}
		if ref.Expose == nil {
			return nil, fmt.Errorf("discovered component %q declares no 'expose' endpoint", d.Component)
		}
		data, err := json.Marshal(computePeers(ref, edgeDevices[d.Component]))
		if err != nil {
			return nil, err
		}
		env = append(env, d.As+"="+string(data))
	}
	return env, nil
}

// computePeers turns a discovered component's group members into peer endpoints
// using its exposed port/path.
func computePeers(comp *appconfig.ComponentConfig, devices []models.LANDevice) []fleetPeer {
	group := ""
	if comp.Target != nil {
		group = comp.Target.Group
	}
	peers := make([]fleetPeer, 0, len(devices))
	for _, dev := range devices {
		// url is the exposed endpoint's base origin; consumers append their own
		// paths (the discover contract — see the template dashboard's serve.py,
		// which does url+"/stream" and url+"/health"). expose.Path documents the
		// component's primary path but is not part of the discovery origin.
		peers = append(peers, fleetPeer{
			Name:   deviceShortName(dev),
			URL:    fmt.Sprintf("http://%s:%d", peerHost(dev), comp.Expose.Port),
			Group:  group,
			Status: "ready",
		})
	}
	return peers
}

// resolveCentralDevice resolves --central to exactly one LAN device.
func resolveCentralDevice(ctx context.Context, name string, timeout time.Duration) (models.LANDevice, error) {
	devices, err := lanGroupDevices(ctx, name, timeout)
	if err != nil {
		return models.LANDevice{}, err
	}
	switch len(devices) {
	case 0:
		return models.LANDevice{}, fmt.Errorf("no LAN device matches --central %q", name)
	case 1:
		return devices[0], nil
	default:
		shorts := make([]string, 0, len(devices))
		for _, d := range devices {
			shorts = append(shorts, deviceShortName(d))
		}
		return models.LANDevice{}, fmt.Errorf("--central %q is ambiguous, matched %d devices (%s); name one exactly", name, len(devices), strings.Join(shorts, ", "))
	}
}

// printCentralInstructions tells the operator how to run a central component
// themselves (e.g. on their laptop) when no --central device was chosen.
func printCentralInstructions(name string, comp *appconfig.ComponentConfig, env []string) {
	fmt.Printf("No --central device given, so %q was not deployed. To run it yourself\n", name)
	fmt.Printf("(e.g. on this machine) from %s/, export the discovered peers first:\n\n", comp.Context)
	for _, e := range env {
		fmt.Printf("  export %s\n", shellQuoteEnv(e))
	}
	fmt.Println("\nthen start the component's server (see its README/Dockerfile).")
}

// shellQuoteEnv renders a KEY=VALUE entry as a copy-pasteable `KEY='VALUE'`.
func shellQuoteEnv(kv string) string {
	i := strings.IndexByte(kv, '=')
	if i < 0 {
		return kv
	}
	key, val := kv[:i], kv[i+1:]
	return key + "='" + strings.ReplaceAll(val, "'", `'\''`) + "'"
}
