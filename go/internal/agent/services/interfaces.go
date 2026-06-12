// Package services implements the gRPC service handlers for the wendy-agent.
package services

import (
	"context"
	"io"

	"github.com/wendylabsinc/wendy/go/internal/shared/appconfig"
	agentpb "github.com/wendylabsinc/wendy/go/proto/gen/agentpb"
)

// NetworkManager abstracts WiFi management operations (typically backed by nmcli).
type NetworkManager interface {
	ListWiFiNetworks(ctx context.Context) ([]*agentpb.ListWiFiNetworksResponse_WiFiNetwork, error)
	ConnectToWiFi(ctx context.Context, req *agentpb.ConnectToWiFiRequest) error
	GetWiFiStatus(ctx context.Context) (connected bool, ssid string, err error)
	DisconnectWiFi(ctx context.Context) error
	ListKnownWiFiNetworks(ctx context.Context) ([]*agentpb.ListKnownWiFiNetworksResponse_KnownWiFiNetwork, error)
	SetWiFiNetworkPriority(ctx context.Context, ssid string, priority int32) error
	ReorderKnownWiFiNetworks(ctx context.Context, orderedSSIDs []string) error
	ForgetWiFiNetwork(ctx context.Context, ssid string) error
}

// HardwareDiscoverer discovers hardware capabilities by probing sysfs, /dev, /proc, etc.
type HardwareDiscoverer interface {
	Discover(ctx context.Context, categoryFilter string) ([]*agentpb.ListHardwareCapabilitiesResponse_HardwareCapability, error)
}

// BluetoothManager abstracts Bluetooth peripheral management.
type BluetoothManager interface {
	Scan(ctx context.Context) (<-chan []*agentpb.DiscoveredBluetoothPeripheral, error)
	Connect(ctx context.Context, address string, pair, trust bool) error
	Disconnect(ctx context.Context, address string) error
	Forget(ctx context.Context, address string) error
}

// ProgressFunc is called by CreateContainer to report progress during
// image pull, unpack, and container creation. The caller may be nil.
type ProgressFunc func(progress *agentpb.CreateContainerProgress)

// ContainerdClient abstracts interactions with the containerd runtime.
type ContainerdClient interface {
	ListLayers(ctx context.Context) ([]*agentpb.LayerHeader, error)
	WriteLayer(ctx context.Context, digest string, reader io.Reader, size int64) error
	AssembleImage(ctx context.Context, imageName string, layers []*agentpb.RunContainerLayerHeader) error
	CreateContainer(ctx context.Context, req *agentpb.CreateContainerRequest, appCfg *appconfig.AppConfig) error
	CreateContainerWithProgress(ctx context.Context, req *agentpb.CreateContainerRequest, appCfg *appconfig.AppConfig, onProgress ProgressFunc) error
	StartContainer(ctx context.Context, appName, postStartAgentCommand string, restartPolicy *agentpb.RestartPolicy) (<-chan ContainerOutput, error)
	StartContainerWithStdin(ctx context.Context, appName string, stdin io.Reader, postStartAgentCommand string, restartPolicy *agentpb.RestartPolicy) (<-chan ContainerOutput, error)
	StopContainer(ctx context.Context, appName string) error
	DeleteContainer(ctx context.Context, appName string, deleteImage bool) error
	// ContainerIDsForApp returns the containerd container IDs for all services
	// belonging to appID (one for single-container, one per service for
	// multi-service apps). Used by the service layer to mark every container in
	// the monitor before issuing a stop or delete.
	ContainerIDsForApp(ctx context.Context, appID string) ([]string, error)
	ListContainers(ctx context.Context) ([]*agentpb.AppContainer, error)
	GetContainerStats(ctx context.Context) ([]*agentpb.ContainerStats, error)
	GetContainerMetrics(ctx context.Context, appName string) (ContainerMetrics, error)
	GetContainerMCPPort(ctx context.Context, appName string) (uint32, error)
	// GetContainerRestartPolicyLabel returns the raw restart policy label stored on
	// the container (e.g. "unless-stopped", "on-failure:5", "no"). An empty string
	// is returned when the container exists but has no restart policy label.
	GetContainerRestartPolicyLabel(ctx context.Context, appName string) (string, error)
}

// Restart policy constants mirror container.RestartPolicy values and are used
// as the policy argument to ContainerMonitorRegistrar.Register.
const (
	RestartPolicyNo            = 0 // never restart
	RestartPolicyUnlessStopped = 1 // restart unless explicitly stopped
	RestartPolicyOnFailure     = 2 // restart only on non-zero exit
	RestartPolicyAlways        = 3 // always restart
)

// ContainerMonitorRegistrar is the subset of container.ContainerMonitor used by
// ContainerService. It is declared here (rather than importing the container
// package) to avoid a circular dependency: container imports services.
type ContainerMonitorRegistrar interface {
	// Register adds appName to the monitor with the given restart policy.
	// policy values mirror container.RestartPolicy: use the RestartPolicy*
	// constants defined in this package.
	Register(appName string, policy int, maxRetries int)
	// Unregister removes appName from the monitor.
	Unregister(appName string)
	// MarkExplicitStop marks appName as intentionally stopped so it won't be
	// automatically restarted by an unless-stopped or on-failure policy.
	MarkExplicitStop(appName string)
	// ClearExplicitStop reverts a prior MarkExplicitStop call, re-enabling
	// automatic restarts for appName. Used to undo a pre-emptive mark when the
	// stop operation itself fails.
	ClearExplicitStop(appName string)
}

type ContainerOutput struct {
	Stdout []byte
	Stderr []byte
	Done   bool
}

type ContainerMetrics struct {
	UserCPUNanos int64 // cumulative user-mode CPU time in nanoseconds
	SysCPUNanos  int64 // cumulative kernel-mode CPU time in nanoseconds
	MemBytes     int64 // current memory usage in bytes
}

// ROS2Target describes a Wendy-managed container carrying the
// sh.wendy/entitlement.ros2 label (WDY-884, WDY-1332).
type ROS2Target struct {
	ContainerID string
	AppID       string
	Distro      string // e.g. "humble"
	DomainID    int    // resolved ROS_DOMAIN_ID
	Running     bool
	TaskPID     uint32 // pid of the container's init process; 0 when not running
}

// ROS2Sidecar describes the running ROS 2 CLI sidecar container.
type ROS2Sidecar struct {
	Distro   string
	DomainID int // default DDS domain, taken from the anchor app container
}

// ROS2ExecOptions configures a single `ros2` invocation inside the sidecar.
type ROS2ExecOptions struct {
	DomainID int      // ROS_DOMAIN_ID for this invocation
	Args     []string // arguments after `ros2`, passed without shell interpretation
}

// ROS2Runtime abstracts the containerd-side ROS 2 sidecar plumbing used by
// the ROS2Service gRPC handlers (WDY-1332).
type ROS2Runtime interface {
	// FindROS2Containers returns all containers labelled with a ros2 config.
	FindROS2Containers(ctx context.Context) ([]ROS2Target, error)
	// EnsureROS2Sidecar starts or reuses the CLI sidecar and returns its
	// distro and the default DDS domain.
	EnsureROS2Sidecar(ctx context.Context) (ROS2Sidecar, error)
	// StopROS2Sidecar stops and removes the sidecar if present.
	StopROS2Sidecar(ctx context.Context) error
	// ExecROS2 runs `ros2 <args>` in the sidecar, streaming output to the
	// writers, and returns the exit code. Cancelling ctx sends SIGINT first
	// so commands like `ros2 bag record` can finalize.
	ExecROS2(ctx context.Context, opts ROS2ExecOptions, stdout, stderr io.Writer) (int, error)
}
