package commands

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/wendylabsinc/wendy/go/internal/shared/appconfig"
	"github.com/wendylabsinc/wendy/go/internal/shared/config"
	"github.com/wendylabsinc/wendy/go/proto/gen/cloudpb"
)

func newFleetRunCmd() *cobra.Command {
	var opts runOptions
	var group string
	var cloudGRPC, brokerURL string

	cmd := &cobra.Command{
		Use:   "run --group <group>",
		Short: "Build and deploy the current project to every device in a group",
		Long: "Deploys the current project (the wendy.json in the working directory) to every\n" +
			"device in a named group, one invocation instead of a per-device loop.\n\n" +
			"Runs detached (it does not stream logs — there are many devices). With\n" +
			"--keep-going a device that fails to deploy does not abort the rest.\n\n" +
			"Note: v1 deploys to each device in turn, reusing the single-device pipeline.\n" +
			"Build-once + parallel fan-out, and placement-aware deploys for fleet manifests\n" +
			"(WDY-1755 components/target), are follow-ups.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			// A fleet deploy targets many devices: never prompt, never stream logs.
			opts.yes = true
			opts.detach = true
			return runFleetRun(cmd.Context(), opts, group, cloudGRPC, brokerURL)
		},
	}

	cmd.Flags().StringVar(&group, "group", "", "Device group to deploy to (required)")
	_ = cmd.MarkFlagRequired("group")
	cmd.Flags().StringVar(&cloudGRPC, "cloud-grpc", "", "Cloud gRPC endpoint (optional when a default session is set via 'wendy auth use')")
	cmd.Flags().StringVar(&brokerURL, "broker-url", os.Getenv("WENDY_BROKER_URL"), "Tunnel broker host:port")
	cmd.Flags().BoolVar(&opts.keepGoing, "keep-going", false, "Deploy to the remaining devices even if one fails, instead of stopping at the first error")

	// A focused subset of `wendy run` build flags (logs/streaming/picker flags
	// don't apply to a fan-out deploy).
	cmd.Flags().StringVar(&opts.buildType, "build-type", "", "Build type when ambiguous: docker, swift, or python")
	cmd.Flags().StringVar(&opts.dockerfile, "dockerfile", "", "Dockerfile/Containerfile to build from")
	cmd.Flags().StringVar(&opts.builder, "builder", "", "Image builder to force: docker or apple-container")
	cmd.Flags().BoolVar(&opts.debug, "debug", false, "Enable debug logging + host networking")
	cmd.Flags().StringVar(&opts.service, "service", "", "Build and deploy only the named service and its dependencies")
	cmd.Flags().StringSliceVar(&opts.userArgs, "user-args", nil, "Extra arguments to pass to the container")
	cmd.Flags().BoolVar(&opts.restartUnlessStopped, "restart-unless-stopped", false, "Restart unless manually stopped")
	cmd.Flags().BoolVar(&opts.restartOnFailure, "restart-on-failure", false, "Restart on failure")
	cmd.Flags().BoolVar(&opts.noRestart, "no-restart", false, "Do not restart on exit")
	cmd.Flags().StringVar(&opts.prefix, "prefix", "", "Project directory to deploy from instead of the current working directory")
	cmd.Flags().StringVar(&opts.chunking, "chunking", chunkingAuto, "Deploy path: auto, force (chunk-diff only), or off (registry push only)")

	return cmd
}

// fleetRunResult is the per-device outcome of a fan-out deploy, for --json.
type fleetRunResult struct {
	Device  string `json:"device"`
	AssetID int32  `json:"assetId"`
	OK      bool   `json:"ok"`
	Error   string `json:"error,omitempty"`
}

func runFleetRun(ctx context.Context, opts runOptions, group, cloudGRPC, brokerURL string) error {
	if err := validateGroupName(group); err != nil {
		return err
	}
	if _, err := normalizeImageBuilder(opts.builder); err != nil {
		return err
	}
	if err := validateChunkingMode(opts.chunking); err != nil {
		return err
	}

	// Load + validate the project once, before connecting to any device, so a
	// bad project fails fast rather than once per device.
	cwd, err := resolveRunWorkingDir(opts)
	if err != nil {
		return fmt.Errorf("resolving working directory: %w", err)
	}
	projectType, err := resolveRunProjectType(cwd, opts.buildType)
	if err != nil {
		return err
	}
	if projectType == "compose" {
		return fmt.Errorf("compose projects are not supported by 'wendy fleet run' yet")
	}
	if projectType == "docker" && opts.dockerfile == "" {
		resolved, err := resolveDockerfile(cwd, opts.dockerfile, !opts.yes && isInteractiveTerminal())
		if err != nil {
			return err
		}
		opts.dockerfile = resolved
	}

	cfgPath := filepath.Join(cwd, "wendy.json")
	missing, err := appConfigFileMissing(cfgPath)
	if err != nil {
		return fmt.Errorf("checking wendy.json: %w", err)
	}
	if missing {
		return fmt.Errorf("no wendy.json in %s — 'wendy fleet run' deploys an existing project to a group", cwd)
	}
	appCfg, err := ensureAppConfig(cfgPath, opts.yes)
	if err != nil {
		return fmt.Errorf("loading wendy.json: %w", err)
	}
	if err := appCfg.Validate(); err != nil {
		return fmt.Errorf("invalid wendy.json: %w", err)
	}
	if err := warnAppConfigFile(cfgPath); err != nil {
		return fmt.Errorf("reading wendy.json warnings: %w", err)
	}
	if opts.debug {
		applyDebugHostNetworking(appCfg)
	}

	// Resolve the group to its member devices.
	auth, err := pickAuthEntry(cloudGRPC)
	if err != nil {
		return err
	}
	assets, err := fetchCloudAssetsFiltered(ctx, auth, false)
	if err != nil {
		return err
	}
	members := assetsInGroup(assets, group)
	if len(members) == 0 {
		return fmt.Errorf("group %q has no devices; add some with 'wendy fleet group add %s <device>...'", group, group)
	}

	fmt.Printf("Deploying %s to %d device(s) in group %q\n", appCfg.AppID, len(members), group)

	// v1: deploy to each device in turn, reusing the proven single-device
	// pipeline (build+push+create+start per device). Sequential keeps build
	// output legible and avoids concurrent builder contention; build-once +
	// parallel fan-out is a follow-up.
	results := make([]fleetRunResult, 0, len(members))
	failures := 0
	for _, asset := range members {
		res := fleetRunResult{Device: asset.GetName(), AssetID: asset.GetId()}
		fmt.Printf("\n── %s (id %d) ──\n", asset.GetName(), asset.GetId())

		if derr := deployToCloudAsset(ctx, auth, asset, brokerURL, cwd, appCfg, opts); derr != nil {
			res.Error = derr.Error()
			failures++
			results = append(results, res)
			fmt.Fprintf(os.Stderr, "  ✗ %s: %v\n", asset.GetName(), derr)
			if !opts.keepGoing {
				return fmt.Errorf("deploy to %s failed: %w (use --keep-going to continue past failures)", asset.GetName(), derr)
			}
			continue
		}
		res.OK = true
		results = append(results, res)
	}

	if jsonOutput {
		if err := printJSON(results); err != nil {
			return err
		}
	} else {
		fmt.Printf("\nDeployed to %d/%d device(s) in group %q\n", len(members)-failures, len(members), group)
	}
	if failures > 0 {
		return fmt.Errorf("%d of %d device(s) failed", failures, len(members))
	}
	return nil
}

// deployToCloudAsset connects to one enrolled asset over the cloud tunnel and
// runs the standard agent deploy pipeline against it.
func deployToCloudAsset(ctx context.Context, auth *config.AuthConfig, asset *cloudpb.Asset, brokerURL, cwd string, appCfg *appconfig.AppConfig, opts runOptions) error {
	conn, err := connectCloudAsset(ctx, auth, asset, brokerURL)
	if err != nil {
		return fmt.Errorf("connecting: %w", err)
	}
	defer conn.Close()
	return runWithAgent(ctx, conn, cwd, appCfg, opts)
}

// applyDebugHostNetworking mirrors runCommand's --debug handling: enable debug
// and ensure a host-mode network entitlement for remote debugger access.
func applyDebugHostNetworking(appCfg *appconfig.AppConfig) {
	appCfg.Debug = true
	for i, e := range appCfg.Entitlements {
		if e.Type == appconfig.EntitlementNetwork {
			appCfg.Entitlements[i].Mode = "host"
			return
		}
	}
	appCfg.Entitlements = append(appCfg.Entitlements, appconfig.Entitlement{
		Type: appconfig.EntitlementNetwork,
		Mode: "host",
	})
}
