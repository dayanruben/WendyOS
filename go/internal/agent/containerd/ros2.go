package containerd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"sync/atomic"
	"syscall"
	"time"

	containerd "github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/pkg/cio"
	"github.com/containerd/containerd/v2/pkg/oci"
	"github.com/containerd/errdefs"
	"go.uber.org/zap"

	localoci "github.com/wendylabsinc/wendy/go/internal/agent/oci"
	"github.com/wendylabsinc/wendy/go/internal/agent/services"
	"github.com/wendylabsinc/wendy/go/internal/shared/appconfig"
)

const (
	// ros2SidecarName is the fixed containerd ID of the ROS 2 CLI sidecar.
	// A single well-known name makes the sidecar idempotent: every
	// `wendy device ros2` command reuses the same container (WDY-1332).
	ros2SidecarName = "wendy-ros2-cli-sidecar"

	// ROS2BagDir is the host directory where rosbag2 recordings are stored.
	// It is bind-mounted into the sidecar at the same path so the agent can
	// list and stream bags without exec round-trips.
	ROS2BagDir = "/var/wendy/ros2-bags"

	// labelKeyROS2Sidecar marks the sidecar container and stores its distro.
	// The sidecar intentionally has no sh.wendy/app.version label so it never
	// appears in ListContainers output.
	labelKeyROS2Sidecar = "sh.wendy/ros2.sidecar"

	// labelKeyROS2AnchorID and labelKeyROS2AnchorPID record which app
	// container's network namespace the sidecar joined. When the anchor
	// changes (app restarted), the sidecar is stale and must be recreated.
	labelKeyROS2AnchorID  = "sh.wendy/ros2.anchor.id"
	labelKeyROS2AnchorPID = "sh.wendy/ros2.anchor.pid"

	// ros2ExecStopGrace is how long ExecROS2 waits after SIGINT before
	// escalating to SIGKILL. `ros2 bag record` needs the grace period to
	// finalize the bag on disk.
	ros2ExecStopGrace = 10 * time.Second
)

// ros2DistroPattern validates ROS 2 distro names read from container labels
// before they are interpolated into an image reference and a shell command
// inside the sidecar (SOC2-CC6, ISO27001-A.8, NIST-SI-10).
var ros2DistroPattern = regexp.MustCompile(`^[a-z][a-z0-9]*$`)

// ros2ExecCounter disambiguates concurrent exec IDs within the agent process.
var ros2ExecCounter atomic.Uint64

// FindROS2Containers returns all Wendy-managed containers carrying the
// sh.wendy/entitlement.ros2 label, with their parsed distro and DDS domain.
func (c *Client) FindROS2Containers(ctx context.Context) ([]services.ROS2Target, error) {
	ctx = c.withNamespace(ctx)
	ctrs, err := c.client.Containers(ctx, fmt.Sprintf("labels.%q", appconfig.ROS2AnnotationKey))
	if err != nil {
		return nil, fmt.Errorf("listing ROS2 containers: %w", err)
	}
	var targets []services.ROS2Target
	for _, ctr := range ctrs {
		labels, err := ctr.Labels(ctx)
		if err != nil {
			continue
		}
		distro, domainID, ok := appconfig.ParseROS2Annotation(labels[appconfig.ROS2AnnotationKey])
		if !ok {
			c.logger.Warn("Skipping container with malformed ros2 label",
				zap.String("container", ctr.ID()),
				zap.String("value", labels[appconfig.ROS2AnnotationKey]))
			continue
		}
		target := services.ROS2Target{
			ContainerID: ctr.ID(),
			AppID:       labels[labelKeyAppID],
			Distro:      distro,
			DomainID:    domainID,
		}
		if task, terr := ctr.Task(ctx, nil); terr == nil {
			if st, serr := task.Status(ctx); serr == nil && st.Status == containerd.Running {
				target.Running = true
				target.TaskPID = task.Pid()
			}
		}
		targets = append(targets, target)
	}
	return targets, nil
}

// EnsureROS2Sidecar starts (or reuses) the ROS 2 CLI sidecar container. The
// sidecar runs the official ros:<distro> image, joins the network namespace
// of the first running ROS 2 app container so it sees every node in the DDS
// domain, and idles on `sleep infinity` while commands are exec'd into it.
func (c *Client) EnsureROS2Sidecar(ctx context.Context) (services.ROS2Sidecar, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	ctx = c.withNamespace(ctx)

	targets, err := c.FindROS2Containers(ctx)
	if err != nil {
		return services.ROS2Sidecar{}, err
	}
	var anchor *services.ROS2Target
	for i := range targets {
		if targets[i].Running {
			anchor = &targets[i]
			break
		}
	}
	if anchor == nil {
		// Lazily satisfy the "sidecar stops when all ROS2 containers stop"
		// lifecycle: tear down any leftover sidecar before failing.
		_ = c.stopROS2SidecarLocked(ctx)
		return services.ROS2Sidecar{}, fmt.Errorf("no running ROS 2 containers found; deploy an app with a frameworks.ros2 config first")
	}
	if !ros2DistroPattern.MatchString(anchor.Distro) {
		return services.ROS2Sidecar{}, fmt.Errorf("invalid ROS 2 distro %q in container label", anchor.Distro)
	}

	sidecar := services.ROS2Sidecar{Distro: anchor.Distro, DomainID: anchor.DomainID}

	// Reuse the existing sidecar when it is running and still anchored to the
	// current network namespace of a live ROS 2 container.
	if existing, lerr := c.client.LoadContainer(ctx, ros2SidecarName); lerr == nil {
		labels, _ := existing.Labels(ctx)
		anchorAlive := labels[labelKeyROS2AnchorID] == anchor.ContainerID &&
			labels[labelKeyROS2AnchorPID] == strconv.FormatUint(uint64(anchor.TaskPID), 10) &&
			labels[labelKeyROS2Sidecar] == anchor.Distro
		if anchorAlive {
			if task, terr := existing.Task(ctx, nil); terr == nil {
				if st, serr := task.Status(ctx); serr == nil && st.Status == containerd.Running {
					return sidecar, nil
				}
			}
		}
		c.logger.Info("Recreating stale ROS 2 sidecar",
			zap.String("anchor", anchor.ContainerID), zap.Bool("anchor_alive", anchorAlive))
		if derr := c.deleteROS2Sidecar(ctx, existing); derr != nil {
			return services.ROS2Sidecar{}, fmt.Errorf("removing stale ROS 2 sidecar: %w", derr)
		}
	}

	imageName := "docker.io/library/ros:" + anchor.Distro
	image, err := c.client.GetImage(ctx, imageName)
	if err != nil {
		c.logger.Info("Pulling ROS 2 sidecar image", zap.String("image", imageName))
		image, err = c.client.Pull(ctx, imageName, containerd.WithPullUnpack)
		if err != nil {
			return services.ROS2Sidecar{}, fmt.Errorf("pulling ROS 2 sidecar image %q: %w", imageName, err)
		}
	}
	if unpacked, uerr := image.IsUnpacked(ctx, ""); uerr == nil && !unpacked {
		if uerr := c.UnpackImage(ctx, image, nil); uerr != nil {
			return services.ROS2Sidecar{}, fmt.Errorf("unpacking ROS 2 sidecar image: %w", uerr)
		}
	}

	if err := os.MkdirAll(ROS2BagDir, 0o755); err != nil {
		return services.ROS2Sidecar{}, fmt.Errorf("creating bag directory: %w", err)
	}

	spec := localoci.DefaultSpec("rootfs", []string{"sleep", "infinity"})
	spec.Process.Env = append(spec.Process.Env, "ROS_LOCALHOST_ONLY=1")
	spec.Mounts = append(spec.Mounts, localoci.Mount{
		Destination: ROS2BagDir,
		Type:        "bind",
		Source:      ROS2BagDir,
		Options:     []string{"rbind", "rw", "nosuid", "nodev"},
	})

	// Join the anchor's network (and uts) namespace so the sidecar shares
	// localhost — and therefore the DDS domain — with the app's ROS 2 nodes.
	nsAnchors, err := localoci.JoinGroupNamespaces(spec, anchor.TaskPID, "shared-network")
	if err != nil {
		return services.ROS2Sidecar{}, fmt.Errorf("joining ROS 2 app network namespace: %w", err)
	}
	defer func() {
		for _, f := range nsAnchors {
			f.Close()
		}
	}()

	specJSON, err := json.Marshal(spec)
	if err != nil {
		return services.ROS2Sidecar{}, fmt.Errorf("marshaling sidecar OCI spec: %w", err)
	}

	labels := map[string]string{
		labelKeyROS2Sidecar:   anchor.Distro,
		labelKeyROS2AnchorID:  anchor.ContainerID,
		labelKeyROS2AnchorPID: strconv.FormatUint(uint64(anchor.TaskPID), 10),
	}
	container, err := c.client.NewContainer(ctx, ros2SidecarName,
		containerd.WithImage(image),
		containerd.WithNewSnapshot(ros2SidecarName, image),
		containerd.WithContainerLabels(labels),
		containerd.WithNewSpec(oci.WithSpecFromBytes(specJSON)),
	)
	if err != nil {
		return services.ROS2Sidecar{}, fmt.Errorf("creating ROS 2 sidecar container: %w", err)
	}

	task, err := container.NewTask(ctx, cio.NullIO)
	if err != nil {
		_ = container.Delete(ctx, containerd.WithSnapshotCleanup)
		return services.ROS2Sidecar{}, fmt.Errorf("creating ROS 2 sidecar task: %w", err)
	}
	if err := task.Start(ctx); err != nil {
		_, _ = task.Delete(ctx, containerd.WithProcessKill)
		_ = container.Delete(ctx, containerd.WithSnapshotCleanup)
		return services.ROS2Sidecar{}, fmt.Errorf("starting ROS 2 sidecar: %w", err)
	}

	c.logger.Info("ROS 2 CLI sidecar started",
		zap.String("image", imageName),
		zap.String("anchor", anchor.ContainerID),
		zap.Int("domain_id", anchor.DomainID))
	return sidecar, nil
}

// StopROS2Sidecar stops and removes the ROS 2 CLI sidecar if present.
func (c *Client) StopROS2Sidecar(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.stopROS2SidecarLocked(c.withNamespace(ctx))
}

// stopROS2SidecarLocked removes the sidecar. Caller must hold c.mu and pass a
// namespaced ctx.
func (c *Client) stopROS2SidecarLocked(ctx context.Context) error {
	container, err := c.client.LoadContainer(ctx, ros2SidecarName)
	if err != nil {
		if errdefs.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("loading ROS 2 sidecar: %w", err)
	}
	return c.deleteROS2Sidecar(ctx, container)
}

func (c *Client) deleteROS2Sidecar(ctx context.Context, container containerd.Container) error {
	if task, err := container.Task(ctx, nil); err == nil {
		_ = task.Kill(ctx, syscall.SIGKILL)
		_, _ = task.Delete(ctx, containerd.WithProcessKill)
	}
	if err := container.Delete(ctx, containerd.WithSnapshotCleanup); err != nil && !errdefs.IsNotFound(err) {
		return err
	}
	return nil
}

// ExecROS2 runs `ros2 <args...>` inside the CLI sidecar, streaming stdout and
// stderr to the given writers, and returns the command's exit code. When ctx
// is cancelled the process receives SIGINT and, after a grace period, SIGKILL
// — the SIGINT-first order lets `ros2 bag record` finalize its output.
// EnsureROS2Sidecar must have succeeded before calling ExecROS2.
func (c *Client) ExecROS2(ctx context.Context, opts services.ROS2ExecOptions, stdout, stderr io.Writer) (int, error) {
	nctx := c.withNamespace(context.WithoutCancel(ctx))

	container, err := c.client.LoadContainer(nctx, ros2SidecarName)
	if err != nil {
		return -1, fmt.Errorf("ROS 2 sidecar not available: %w", err)
	}
	labels, err := container.Labels(nctx)
	if err != nil {
		return -1, fmt.Errorf("reading ROS 2 sidecar labels: %w", err)
	}
	distro := labels[labelKeyROS2Sidecar]
	if !ros2DistroPattern.MatchString(distro) {
		return -1, fmt.Errorf("invalid distro %q on ROS 2 sidecar", distro)
	}
	if opts.DomainID < appconfig.ROS2DomainIDMin || opts.DomainID > appconfig.ROS2DomainIDMax {
		return -1, fmt.Errorf("domain ID %d out of range [%d,%d]", opts.DomainID, appconfig.ROS2DomainIDMin, appconfig.ROS2DomainIDMax)
	}

	task, err := container.Task(nctx, nil)
	if err != nil {
		return -1, fmt.Errorf("ROS 2 sidecar task not running: %w", err)
	}

	spec, err := container.Spec(nctx)
	if err != nil {
		return -1, fmt.Errorf("reading ROS 2 sidecar spec: %w", err)
	}
	pspec := spec.Process
	// The ROS environment lives in /opt/ros/<distro>/setup.bash; the "$@"
	// indirection keeps user-supplied args out of shell interpretation
	// (SOC2-CC6, ISO27001-A.8, NIST-SI-10).
	pspec.Terminal = false
	pspec.Args = append([]string{
		"/bin/bash", "-c",
		fmt.Sprintf("source /opt/ros/%s/setup.bash >/dev/null 2>&1 && exec ros2 \"$@\"", distro),
		"ros2",
	}, opts.Args...)
	pspec.Env = append(pspec.Env,
		"ROS_DOMAIN_ID="+strconv.Itoa(opts.DomainID),
		"ROS_LOCALHOST_ONLY=1",
	)

	execID := fmt.Sprintf("ros2-exec-%d-%d", time.Now().UnixNano(), ros2ExecCounter.Add(1))
	proc, err := task.Exec(nctx, execID, pspec, cio.NewCreator(cio.WithStreams(nil, stdout, stderr)))
	if err != nil {
		return -1, fmt.Errorf("exec into ROS 2 sidecar: %w", err)
	}
	defer func() {
		_, _ = proc.Delete(nctx, containerd.WithProcessKill)
	}()

	statusC, err := proc.Wait(nctx)
	if err != nil {
		return -1, fmt.Errorf("waiting on ROS 2 exec: %w", err)
	}
	if err := proc.Start(nctx); err != nil {
		return -1, fmt.Errorf("starting ROS 2 exec: %w", err)
	}

	select {
	case st := <-statusC:
		return int(st.ExitCode()), st.Error()
	case <-ctx.Done():
		_ = proc.Kill(nctx, syscall.SIGINT)
		select {
		case st := <-statusC:
			return int(st.ExitCode()), ctx.Err()
		case <-time.After(ros2ExecStopGrace):
			_ = proc.Kill(nctx, syscall.SIGKILL)
			st := <-statusC
			return int(st.ExitCode()), ctx.Err()
		}
	}
}
