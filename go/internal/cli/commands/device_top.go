package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"github.com/wendylabsinc/wendy/go/internal/cli/grpcclient"
	"github.com/wendylabsinc/wendy/go/proto/gen/agentpb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// topSample is a normalized snapshot used to compute CPU% from deltas.
type topSample struct {
	host         *agentpb.HostStats
	containers   map[string]uint64 // container ID -> cumulative cpu nanos
	mem          map[string]int64  // container ID -> memory bytes
	takenAtNanos int64
}

func newTopSample(resp *agentpb.GetResourceStatsResponse, atNanos int64) topSample {
	s := topSample{
		host:         resp.GetHost(),
		containers:   make(map[string]uint64),
		mem:          make(map[string]int64),
		takenAtNanos: atNanos,
	}
	for _, c := range resp.GetContainers() {
		s.containers[c.GetAppName()] = c.GetCpuUsageNanos()
		s.mem[c.GetAppName()] = c.GetMemoryBytes()
	}
	return s
}

// hostCPUPercent returns busy CPU percentage (0-100) across the whole machine,
// computed from the idle/total jiffy deltas between two samples.
func hostCPUPercent(prev, cur topSample) float64 {
	if prev.host == nil || cur.host == nil {
		return 0
	}
	totalΔ := int64(cur.host.GetCpuTotalJiffies()) - int64(prev.host.GetCpuTotalJiffies())
	idleΔ := int64(cur.host.GetCpuIdleJiffies()) - int64(prev.host.GetCpuIdleJiffies())
	if totalΔ <= 0 {
		return 0
	}
	busy := (1 - float64(idleΔ)/float64(totalΔ)) * 100
	if busy < 0 {
		return 0
	}
	return busy
}

// containerCPUPercent returns a container's CPU usage as a percentage of the
// whole machine (0-100 across all cores), from the CPU-nanos delta over elapsed
// wall time. cpuCount normalizes to "share of total machine".
func containerCPUPercent(prev, cur topSample, id string, cpuCount uint32) float64 {
	wallΔ := cur.takenAtNanos - prev.takenAtNanos
	if wallΔ <= 0 || cpuCount == 0 {
		return 0
	}
	prevNanos, ok := prev.containers[id]
	if !ok {
		return 0
	}
	curNanos := cur.containers[id]
	if curNanos < prevNanos {
		return 0 // counter reset / container restarted
	}
	pct := float64(curNanos-prevNanos) / (float64(wallΔ) * float64(cpuCount)) * 100
	if pct < 0 {
		return 0
	}
	return pct
}

// topRow is one display row (app, group header, or service subrow).
type topRow struct {
	name          string // app ID; "" for subrows
	displayName   string
	cpuPercent    float64
	memBytes      int64
	hasCPU        bool
	isGroupHeader bool
	isSubrow      bool
}

// buildTopRows groups containers by app (mirroring buildDashboardRows) with CPU%
// and memory columns. Top-level apps are sorted by the active key descending;
// service subrows stay under their group header.
func buildTopRows(containers []*agentpb.AppContainer, cpuByID map[string]float64, memByID map[string]int64, sortByCPU bool) []topRow {
	type appAgg struct {
		container *agentpb.AppContainer
		cpu       float64
		mem       int64
	}
	aggs := make([]appAgg, 0, len(containers))
	for _, c := range containers {
		appName := c.GetAppName()
		var cpu float64
		var mem int64
		if len(c.GetServices()) > 1 {
			for _, svc := range c.GetServices() {
				key := appName + "_" + svc.GetName()
				cpu += cpuByID[key]
				mem += memByID[key]
			}
		} else {
			cpu = cpuByID[appName]
			mem = memByID[appName]
		}
		aggs = append(aggs, appAgg{container: c, cpu: cpu, mem: mem})
	}

	sort.SliceStable(aggs, func(i, j int) bool {
		if sortByCPU {
			return aggs[i].cpu > aggs[j].cpu
		}
		return aggs[i].mem > aggs[j].mem
	})

	var rows []topRow
	for _, a := range aggs {
		c := a.container
		appName := c.GetAppName()
		if len(c.GetServices()) > 1 {
			rows = append(rows, topRow{
				name:          appName,
				displayName:   appName + " [group]",
				cpuPercent:    a.cpu,
				memBytes:      a.mem,
				hasCPU:        true,
				isGroupHeader: true,
			})
			for _, svc := range c.GetServices() {
				key := appName + "_" + svc.GetName()
				rows = append(rows, topRow{
					displayName: "  ↳ " + svc.GetName(),
					cpuPercent:  cpuByID[key],
					memBytes:    memByID[key],
					hasCPU:      true,
					isSubrow:    true,
				})
			}
		} else {
			rows = append(rows, topRow{
				name:        appName,
				displayName: appName,
				cpuPercent:  a.cpu,
				memBytes:    a.mem,
				hasCPU:      true,
			})
		}
	}
	return rows
}

// errResourceStatsUnimplemented marks an agent too old to support device top.
var errResourceStatsUnimplemented = fmt.Errorf("the device's agent does not support resource stats; update it with 'wendy device update'")

func sampleResourceStats(ctx context.Context, conn *grpcclient.AgentConnection) (*agentpb.GetResourceStatsResponse, error) {
	resp, err := conn.ContainerService.GetResourceStats(ctx, &agentpb.GetResourceStatsRequest{})
	if err != nil {
		if status.Code(err) == codes.Unimplemented {
			return nil, errResourceStatsUnimplemented
		}
		return nil, err
	}
	return resp, nil
}

func listAppContainers(ctx context.Context, conn *grpcclient.AgentConnection) ([]*agentpb.AppContainer, error) {
	stream, err := conn.ContainerService.ListContainers(ctx, &agentpb.ListContainersRequest{})
	if err != nil {
		return nil, err
	}
	var out []*agentpb.AppContainer
	for {
		resp, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if c := resp.GetContainer(); c != nil {
			out = append(out, c)
		}
	}
	return out, nil
}

type topJSONHost struct {
	CPUPercent    float64      `json:"cpuPercent"`
	CPUCount      uint32       `json:"cpuCount"`
	MemUsedBytes  int64        `json:"memUsedBytes"`
	MemTotalBytes int64        `json:"memTotalBytes"`
	GPUs          []topJSONGPU `json:"gpus,omitempty"`
}

type topJSONGPU struct {
	Index         uint32   `json:"index"`
	Name          string   `json:"name"`
	UtilPercent   float64  `json:"utilPercent"`
	MemUsedBytes  int64    `json:"memUsedBytes"`
	MemTotalBytes int64    `json:"memTotalBytes"`
	TempC         *float64 `json:"tempC,omitempty"`
	PowerW        *float64 `json:"powerW,omitempty"`
}

type topJSONContainer struct {
	Name       string  `json:"name"`
	State      string  `json:"state"`
	CPUPercent float64 `json:"cpuPercent"`
	MemBytes   int64   `json:"memBytes"`
}

type topJSONOutput struct {
	Host       topJSONHost        `json:"host"`
	Containers []topJSONContainer `json:"containers"`
}

func buildTopJSON(prev, cur topSample, containers []*agentpb.AppContainer) topJSONOutput {
	out := topJSONOutput{}
	if cur.host != nil {
		out.Host.CPUPercent = hostCPUPercent(prev, cur)
		out.Host.CPUCount = cur.host.GetCpuCount()
		out.Host.MemTotalBytes = cur.host.GetMemTotalBytes()
		out.Host.MemUsedBytes = cur.host.GetMemTotalBytes() - cur.host.GetMemAvailableBytes()
		for _, g := range cur.host.GetGpus() {
			out.Host.GPUs = append(out.Host.GPUs, topJSONGPU{
				Index:         g.GetIndex(),
				Name:          g.GetName(),
				UtilPercent:   g.GetUtilPercent(),
				MemUsedBytes:  g.GetMemUsedBytes(),
				MemTotalBytes: g.GetMemTotalBytes(),
				TempC:         g.TempC,
				PowerW:        g.PowerW,
			})
		}
	}
	cpuCount := uint32(1)
	if cur.host != nil && cur.host.GetCpuCount() > 0 {
		cpuCount = cur.host.GetCpuCount()
	}
	cpuByID := map[string]float64{}
	for id := range cur.containers {
		cpuByID[id] = containerCPUPercent(prev, cur, id, cpuCount)
	}
	rows := buildTopRows(containers, cpuByID, cur.mem, false)
	for _, r := range rows {
		if r.isSubrow {
			continue
		}
		out.Containers = append(out.Containers, topJSONContainer{
			Name:       r.displayName,
			CPUPercent: r.cpuPercent,
			MemBytes:   r.memBytes,
		})
	}
	return out
}

func runTopSnapshot(ctx context.Context, conn *grpcclient.AgentConnection, asJSON bool) error {
	containers, err := listAppContainers(ctx, conn)
	if err != nil {
		return fmt.Errorf("listing containers: %w", err)
	}
	first, err := sampleResourceStats(ctx, conn)
	if err != nil {
		return err
	}
	prev := newTopSample(first, time.Now().UnixNano())
	time.Sleep(250 * time.Millisecond)
	second, err := sampleResourceStats(ctx, conn)
	if err != nil {
		return err
	}
	cur := newTopSample(second, time.Now().UnixNano())

	if asJSON {
		data, err := json.MarshalIndent(buildTopJSON(prev, cur, containers), "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
		return nil
	}

	// Plain table.
	cpuCount := uint32(1)
	if cur.host != nil && cur.host.GetCpuCount() > 0 {
		cpuCount = cur.host.GetCpuCount()
	}
	if cur.host != nil {
		fmt.Printf("CPU: %.1f%%  MEM: %s / %s\n",
			hostCPUPercent(prev, cur),
			formatBytes(cur.host.GetMemTotalBytes()-cur.host.GetMemAvailableBytes()),
			formatBytes(cur.host.GetMemTotalBytes()))
		for _, g := range cur.host.GetGpus() {
			fmt.Printf("GPU%d %s: %.0f%%  %s / %s\n", g.GetIndex(), g.GetName(),
				g.GetUtilPercent(), formatBytes(g.GetMemUsedBytes()), formatBytes(g.GetMemTotalBytes()))
		}
	}
	cpuByID := map[string]float64{}
	for id := range cur.containers {
		cpuByID[id] = containerCPUPercent(prev, cur, id, cpuCount)
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "APP\tCPU%\tMEM")
	for _, r := range buildTopRows(containers, cpuByID, cur.mem, false) {
		fmt.Fprintf(tw, "%s\t%.1f\t%s\n", r.displayName, r.cpuPercent, formatBytes(r.memBytes))
	}
	return tw.Flush()
}

func newTopCmd() *cobra.Command {
	var interval time.Duration
	cmd := &cobra.Command{
		Use:   "top",
		Short: "Live CPU, memory, and GPU usage for the device and its containers",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			conn, err := connectToAgent(ctx)
			if err != nil {
				return err
			}
			defer conn.Close()

			if jsonOutput || !isInteractiveTerminal() {
				return runTopSnapshot(ctx, conn, jsonOutput)
			}
			return runTopDashboard(ctx, conn, interval)
		},
	}
	cmd.Flags().DurationVar(&interval, "interval", 2*time.Second, "Refresh interval for the live view")
	return cmd
}

// TEMP: replaced by full implementation in Task 8.
func runTopDashboard(ctx context.Context, conn *grpcclient.AgentConnection, interval time.Duration) error {
	return nil
}
