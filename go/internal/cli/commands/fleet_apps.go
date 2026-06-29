package commands

import (
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"sync"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"

	"github.com/wendylabsinc/wendy/go/internal/shared/config"
	"github.com/wendylabsinc/wendy/go/proto/gen/agentpb"
	"github.com/wendylabsinc/wendy/go/proto/gen/cloudpb"
)

// fleetAppsMaxConcurrency bounds how many device tunnels we open at once when
// gathering inventory across a group.
const fleetAppsMaxConcurrency = 8

func newFleetAppsCmd() *cobra.Command {
	var group string
	var cloudGRPC, brokerURL string

	cmd := &cobra.Command{
		Use:   "apps",
		Short: "List the apps running across a group (or the whole org)",
		Long: "Fans out to every device in a group (or all enrolled devices when --group is\n" +
			"omitted) and prints one row per app: which device it runs on, its version, and\n" +
			"its state. Devices that can't be reached are reported inline rather than failing\n" +
			"the whole command.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runFleetApps(cmd.Context(), group, cloudGRPC, brokerURL)
		},
	}
	cmd.Flags().StringVar(&group, "group", "", "Limit to devices in this group (default: all enrolled devices)")
	cmd.Flags().StringVar(&cloudGRPC, "cloud-grpc", "", "Cloud gRPC endpoint (optional when a default session is set via 'wendy auth use')")
	cmd.Flags().StringVar(&brokerURL, "broker-url", os.Getenv("WENDY_BROKER_URL"), "Tunnel broker host:port")
	return cmd
}

// fleetAppRow is one row of fleet inventory (a single app on a single device),
// and the --json element shape. A row with Error set and App empty represents a
// device that could not be reached.
type fleetAppRow struct {
	Device  string `json:"device"`
	AssetID int32  `json:"assetId"`
	App     string `json:"app,omitempty"`
	Version string `json:"version,omitempty"`
	State   string `json:"state,omitempty"`
	Errors  uint32 `json:"errors,omitempty"`
	Error   string `json:"error,omitempty"`
}

func runFleetApps(ctx context.Context, group, cloudGRPC, brokerURL string) error {
	if group != "" {
		if err := validateGroupName(group); err != nil {
			return err
		}
	}
	auth, err := pickAuthEntry(cloudGRPC)
	if err != nil {
		return err
	}
	assets, err := fetchCloudAssetsFiltered(ctx, auth, false)
	if err != nil {
		return err
	}
	if group != "" {
		assets = assetsInGroup(assets, group)
	}
	if len(assets) == 0 {
		if group != "" {
			return fmt.Errorf("group %q has no devices", group)
		}
		return fmt.Errorf("no enrolled devices found for this org")
	}

	rows := gatherFleetApps(ctx, auth, assets, brokerURL)
	sortFleetAppRows(rows)

	if jsonOutput || !isInteractiveTerminal() {
		return printJSON(rows)
	}

	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(tw, "DEVICE\tAPP\tVERSION\tSTATE\tERRORS")
	for _, r := range rows {
		if r.Error != "" {
			fmt.Fprintf(tw, "%s\t(unreachable: %s)\t\t\t\n", r.Device, r.Error)
			continue
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%d\n", r.Device, r.App, dash(r.Version), r.State, r.Errors)
	}
	return tw.Flush()
}

// gatherFleetApps queries every asset's container list concurrently and flattens
// the result into per-app rows. A device that errors contributes a single row
// with its Error set; one bad device never fails the whole sweep.
func gatherFleetApps(ctx context.Context, auth *config.AuthConfig, assets []*cloudpb.Asset, brokerURL string) []fleetAppRow {
	var (
		mu   sync.Mutex
		rows []fleetAppRow
	)
	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(fleetAppsMaxConcurrency)

	for _, asset := range assets {
		asset := asset
		g.Go(func() error {
			containers, err := listAssetContainers(ctx, auth, asset, brokerURL)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				rows = append(rows, fleetAppRow{Device: asset.GetName(), AssetID: asset.GetId(), Error: err.Error()})
				return nil
			}
			if len(containers) == 0 {
				// Reachable but no apps — still worth a row so the device shows up.
				rows = append(rows, fleetAppRow{Device: asset.GetName(), AssetID: asset.GetId(), App: "—", State: "—"})
				return nil
			}
			for _, c := range containers {
				rows = append(rows, fleetAppRow{
					Device:  asset.GetName(),
					AssetID: asset.GetId(),
					App:     c.GetAppName(),
					Version: c.GetAppVersion(),
					State:   runningStateString(c.GetRunningState()),
					Errors:  c.GetFailureCount(),
				})
			}
			return nil
		})
	}
	_ = g.Wait() // per-device errors are captured as rows; Wait never returns one.
	return rows
}

// listAssetContainers opens a tunnel to one asset and drains its container list.
func listAssetContainers(ctx context.Context, auth *config.AuthConfig, asset *cloudpb.Asset, brokerURL string) ([]*agentpb.AppContainer, error) {
	conn, err := connectCloudAsset(ctx, auth, asset, brokerURL)
	if err != nil {
		return nil, fmt.Errorf("connecting: %w", err)
	}
	defer conn.Close()

	stream, err := conn.ContainerService.ListContainers(ctx, &agentpb.ListContainersRequest{})
	if err != nil {
		return nil, fmt.Errorf("listing containers: %w", err)
	}
	var containers []*agentpb.AppContainer
	for {
		resp, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("receiving container list: %w", err)
		}
		if c := resp.GetContainer(); c != nil {
			containers = append(containers, c)
		}
	}
	return containers, nil
}

// sortFleetAppRows orders rows by device, then app, for stable output.
func sortFleetAppRows(rows []fleetAppRow) {
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Device != rows[j].Device {
			return rows[i].Device < rows[j].Device
		}
		return rows[i].App < rows[j].App
	})
}

func runningStateString(s agentpb.AppRunningState) string {
	if s == agentpb.AppRunningState_RUNNING {
		return "running"
	}
	return "stopped"
}

func dash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}
