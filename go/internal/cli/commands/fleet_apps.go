package commands

import (
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"sync"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"

	"github.com/wendylabsinc/wendy/go/internal/cli/grpcclient"
	"github.com/wendylabsinc/wendy/go/proto/gen/agentpb"
)

// fleetAppsMaxConcurrency bounds how many device connections we open at once
// when gathering inventory across a group.
const fleetAppsMaxConcurrency = 8

func newFleetAppsCmd() *cobra.Command {
	var group string
	var cloudGRPC, brokerURL string
	var lan bool
	var timeout time.Duration

	cmd := &cobra.Command{
		Use:   "apps",
		Short: "List the apps running across a group (or every device)",
		Long: "Fans out to every device in a group (or all devices when --group is omitted)\n" +
			"and prints one row per app: which device it runs on, its version, and its\n" +
			"state. Devices that can't be reached are reported inline rather than failing\n" +
			"the whole command.\n\n" +
			"With --lan the group is resolved over the local network via mDNS instead of the\n" +
			"cloud: a group is a glob over device names (e.g. 'camera-*' or 'camera'), no\n" +
			"enrollment or cloud session required.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runFleetApps(cmd.Context(), group, cloudGRPC, brokerURL, lan, timeout)
		},
	}
	cmd.Flags().StringVar(&group, "group", "", "Limit to devices in this group (default: all devices)")
	cmd.Flags().BoolVar(&lan, "lan", false, "Resolve the group over the LAN (mDNS) instead of the cloud")
	cmd.Flags().DurationVar(&timeout, "discover-timeout", fleetLANDiscoverTimeout, "How long to browse for LAN devices (with --lan)")
	cmd.Flags().StringVar(&cloudGRPC, "cloud-grpc", "", "Cloud gRPC endpoint (optional when a default session is set via 'wendy auth use')")
	cmd.Flags().StringVar(&brokerURL, "broker-url", os.Getenv("WENDY_BROKER_URL"), "Tunnel broker host:port")
	return cmd
}

// fleetAppRow is one row of fleet inventory (a single app on a single device),
// and the --json element shape. A row with Error set and App empty represents a
// device that could not be reached.
type fleetAppRow struct {
	Device  string `json:"device"`
	App     string `json:"app,omitempty"`
	Version string `json:"version,omitempty"`
	State   string `json:"state,omitempty"`
	Errors  uint32 `json:"errors,omitempty"`
	Error   string `json:"error,omitempty"`
}

func runFleetApps(ctx context.Context, group, cloudGRPC, brokerURL string, lan bool, timeout time.Duration) error {
	targets, err := resolveFleetTargets(ctx, group, lan, cloudGRPC, brokerURL, timeout)
	if err != nil {
		return err
	}
	if len(targets) == 0 {
		if group != "" {
			return fmt.Errorf("group %q has no devices", group)
		}
		if lan {
			return fmt.Errorf("no WendyOS devices found on the LAN")
		}
		return fmt.Errorf("no enrolled devices found for this org")
	}

	rows := gatherFleetApps(ctx, targets)
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

// gatherFleetApps queries every target's container list concurrently and
// flattens the result into per-app rows. A device that errors contributes a
// single row with its Error set; one bad device never fails the whole sweep.
func gatherFleetApps(ctx context.Context, targets []fleetTarget) []fleetAppRow {
	var (
		mu   sync.Mutex
		rows []fleetAppRow
	)
	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(fleetAppsMaxConcurrency)

	for _, target := range targets {
		target := target
		g.Go(func() error {
			containers, err := listTargetContainers(ctx, target)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				rows = append(rows, fleetAppRow{Device: target.Name, Error: err.Error()})
				return nil
			}
			if len(containers) == 0 {
				// Reachable but no apps — still worth a row so the device shows up.
				rows = append(rows, fleetAppRow{Device: target.Name, App: "—", State: "—"})
				return nil
			}
			for _, c := range containers {
				rows = append(rows, fleetAppRow{
					Device:  target.Name,
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

// listTargetContainers connects to one target and drains its container list.
func listTargetContainers(ctx context.Context, target fleetTarget) ([]*agentpb.AppContainer, error) {
	conn, err := target.connect(ctx)
	if err != nil {
		return nil, fmt.Errorf("connecting: %w", err)
	}
	defer conn.Conn.Close()
	return listContainersOnConn(ctx, conn)
}

// listContainersOnConn drains the agent's container list over an open connection.
func listContainersOnConn(ctx context.Context, conn *grpcclient.AgentConnection) ([]*agentpb.AppContainer, error) {
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
