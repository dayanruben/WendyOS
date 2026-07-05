package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/wendylabsinc/wendy/go/internal/shared/appconfig"
	"github.com/wendylabsinc/wendy/go/internal/shared/models"
)

// fleetPeer is one discovered endpoint handed to a consuming component, matching
// the snapshot shape injected under a discovers "as" env var:
//
//	[{"name":"camera-01","url":"http://10.0.0.4:8000","group":"camera-*","status":"ready"}, ...]
type fleetPeer struct {
	Name   string `json:"name"`
	URL    string `json:"url"`
	Group  string `json:"group"`
	Status string `json:"status"`
}

// runFleetManifest deploys a wendy-fleet.json. Each component references an app
// directory (its own wendy.json holds the build/runtime config) and lists tags.
//
// By default components are placed by CLOUD asset tags (assigned with
// 'wendy fleet group add') and deployed over the cloud tunnel. With --lan they
// are placed by matching device names over mDNS instead. Cross-component
// discovery (discovers -> WENDY_FLEET_PEERS) is wired in LAN mode; over the
// cloud it needs per-peer reachability (the mesh, WDY-1778) and is deferred, so
// cloud mode does placement only.
func runFleetManifest(ctx context.Context, opts runOptions, projectCwd string, manifest *appconfig.FleetManifest, lan bool, central, cloudGRPC, brokerURL string, timeout time.Duration) error {
	// Resolve targets per component from the chosen backend. In LAN mode also
	// keep the matched devices, for the discovery snapshot.
	targetsByComp := make(map[string][]fleetTarget, len(manifest.Components))
	lanDevices := make(map[string][]models.LANDevice)

	if lan {
		for name, comp := range manifest.Components {
			devs, err := lanDevicesForTags(ctx, comp.Tags, timeout)
			if err != nil {
				return err
			}
			lanDevices[name] = devs
			for _, dev := range devs {
				targetsByComp[name] = append(targetsByComp[name], targetForDevice(dev))
			}
		}
	} else {
		auth, err := pickAuthEntry(cloudGRPC)
		if err != nil {
			return err
		}
		assets, err := fetchCloudAssetsFiltered(ctx, auth, false)
		if err != nil {
			return err
		}
		for name, comp := range manifest.Components {
			targetsByComp[name] = cloudTargetsForTags(auth, assets, comp.Tags, brokerURL)
		}
	}

	// Producers (no discovers) before consumers (with discovers) so discovered
	// endpoints are up first.
	var producers, consumers []string
	for name, comp := range manifest.Components {
		if len(comp.Discovers) > 0 {
			consumers = append(consumers, name)
		} else {
			producers = append(producers, name)
		}
	}
	sort.Strings(producers)
	sort.Strings(consumers)

	for _, name := range append(producers, consumers...) {
		comp := manifest.Components[name]
		appDir := filepath.Join(projectCwd, comp.Path)
		appCfg, err := loadComponentApp(appDir, opts)
		if err != nil {
			return fmt.Errorf("component %q: %w", name, err)
		}

		compOpts := opts
		var lanEnv []string
		if len(comp.Discovers) > 0 {
			if lan {
				lanEnv, err = discoveryEnv(comp, manifest, lanDevices)
				if err != nil {
					return fmt.Errorf("component %q: %w", name, err)
				}
				if len(lanEnv) > 0 {
					// Env injection rides on CreateContainerRequest.Env, carried by
					// the registry (chunk-off) create path; force it so peers aren't
					// dropped.
					compOpts.env = lanEnv
					compOpts.chunking = chunkingOff
				}
			} else {
				fmt.Fprintf(os.Stderr, "  note: %q declares discovers; cross-device discovery over the cloud is deferred to the mesh (WDY-1778) — deploying placement only\n", name)
			}
		}

		targets := targetsByComp[name]
		if len(targets) == 0 {
			// LAN: a component matching no device can run on --central, or (if it
			// consumes discovery) off-fleet — print how to run it.
			if lan && central != "" {
				dev, derr := resolveCentralDevice(ctx, central, timeout)
				if derr != nil {
					return derr
				}
				fmt.Printf("\n=== component %q (no device matched %v) → --central %s ===\n", name, comp.Tags, deviceShortName(dev))
				if err := deployToTarget(ctx, targetForDevice(dev), appDir, appCfg, compOpts); err != nil {
					return fmt.Errorf("component %q on %s: %w", name, deviceShortName(dev), err)
				}
				continue
			}
			if lan && len(comp.Discovers) > 0 {
				fmt.Printf("\n=== component %q (no device matched %v) ===\n", name, comp.Tags)
				printCentralInstructions(name, comp, lanEnv)
				continue
			}
			fmt.Fprintf(os.Stderr, "  warning: no devices carry tags %v for %q; assign with 'wendy fleet group add <tag> <device>' and re-run\n", comp.Tags, name)
			continue
		}

		fmt.Printf("\n=== component %q → tags %v (%d device(s)) ===\n", name, comp.Tags, len(targets))
		if _, failures := deployToTargets(ctx, targets, appDir, appCfg, compOpts); failures > 0 && !opts.keepGoing {
			return fmt.Errorf("component %q: %d device(s) failed", name, failures)
		}
	}

	fmt.Println("\nFleet deploy complete.")
	return nil
}

// loadComponentApp loads and validates the app's own wendy.json from its
// directory — the fleet manifest references the app; the app defines itself.
func loadComponentApp(appDir string, opts runOptions) (*appconfig.AppConfig, error) {
	cfgPath := filepath.Join(appDir, "wendy.json")
	missing, err := appConfigFileMissing(cfgPath)
	if err != nil {
		return nil, fmt.Errorf("checking %s: %w", cfgPath, err)
	}
	if missing {
		return nil, fmt.Errorf("no wendy.json in app directory %s", appDir)
	}
	appCfg, err := ensureAppConfig(cfgPath, opts.yes)
	if err != nil {
		return nil, fmt.Errorf("loading %s: %w", cfgPath, err)
	}
	if err := appCfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid %s: %w", cfgPath, err)
	}
	return appCfg, nil
}

// discoveryEnv builds the env vars injected into a component for its declared
// discovers: each named var carries a JSON snapshot of the referenced
// component's live peer endpoints (from the devices its tags matched).
func discoveryEnv(consumer *appconfig.ComponentConfig, manifest *appconfig.FleetManifest, matched map[string][]models.LANDevice) ([]string, error) {
	var env []string
	for _, d := range consumer.Discovers {
		ref, ok := manifest.Components[d.Component]
		if !ok {
			return nil, fmt.Errorf("discovers references unknown component %q", d.Component)
		}
		if ref.Expose == nil {
			return nil, fmt.Errorf("discovered component %q declares no 'expose' endpoint", d.Component)
		}
		data, err := json.Marshal(computePeers(ref, matched[d.Component]))
		if err != nil {
			return nil, err
		}
		env = append(env, d.As+"="+string(data))
	}
	return env, nil
}

// computePeers turns a component's matched devices into peer endpoints using its
// exposed port. url is the endpoint's base origin; consumers append their own
// path (the discover contract — see the template dashboard's serve.py).
func computePeers(comp *appconfig.ComponentConfig, devices []models.LANDevice) []fleetPeer {
	tag := ""
	if len(comp.Tags) > 0 {
		tag = comp.Tags[0]
	}
	peers := make([]fleetPeer, 0, len(devices))
	for _, dev := range devices {
		peers = append(peers, fleetPeer{
			Name:   deviceShortName(dev),
			URL:    fmt.Sprintf("http://%s:%d", peerHost(dev), comp.Expose.Port),
			Group:  tag,
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

// printCentralInstructions tells the operator how to run a component themselves
// (e.g. on their laptop) when its tags matched no device and no --central given.
func printCentralInstructions(name string, comp *appconfig.ComponentConfig, env []string) {
	fmt.Printf("No device matched, so %q was not deployed. To run it yourself\n", name)
	fmt.Printf("(e.g. on this machine) from %s/, export the discovered peers first:\n\n", comp.Path)
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
