//go:build darwin || linux || windows

package commands

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/wendylabsinc/wendy/go/internal/cli/grpcclient"
	"github.com/wendylabsinc/wendy/go/proto/gen/agentpb"
	agentpbv2 "github.com/wendylabsinc/wendy/go/proto/gen/agentpb/v2"
)

type preparedInstallDriver struct {
	spec *agentpbv2.DriverSpec
	path string
}

type osInstallDrivers struct {
	deviceType, version, kernel string
	drivers                     []preparedInstallDriver
}

// Resolve and download before flashing. The OS and add-ons remain pinned even
// if the nightly catalog changes while the image is being written.
func prepareOSInstallDrivers(ctx context.Context, deviceType, version string, nightly bool, pr int, names []string) (*osInstallDrivers, error) {
	if len(names) == 0 {
		return nil, nil
	}
	dm, version, err := resolveDeviceArtifactContext(ctx, deviceType, version, nightly, pr)
	if err != nil {
		return nil, err
	}
	v, ok := dm.Versions[version]
	if !ok {
		return nil, fmt.Errorf("no manifest entry for %s on %s", version, deviceType)
	}
	kernel := ""
	if v.Devkit != nil {
		kernel = v.Devkit.KernelVersion
	}
	if kernel == "" {
		for _, e := range v.Extensions {
			if e.KernelVersion == "" {
				continue
			}
			if kernel != "" && kernel != e.KernelVersion {
				return nil, fmt.Errorf("cannot select --driver builds: %s publishes several kernels without a devkit identifying the image kernel", version)
			}
			kernel = e.KernelVersion
		}
	}
	if kernel == "" {
		return nil, fmt.Errorf("%s on %s publishes no driver kernel metadata", version, deviceType)
	}
	p := &osInstallDrivers{deviceType: deviceType, version: version, kernel: kernel}
	seen := map[string]bool{}
	for _, name := range names {
		if seen[name] {
			continue
		}
		seen[name] = true
		e, ok := selectExtension(v.Extensions, name, kernel)
		if !ok {
			p.close()
			return nil, fmt.Errorf("no driver %q published for %s on %s, kernel %s", name, version, deviceType, kernel)
		}
		spec, err := publishedDriverSpec(e)
		if err != nil {
			p.close()
			return nil, err
		}
		cliLogln("Downloading %s for installation after first boot...", name)
		path, err := downloadDriver(ctx, spec)
		if err != nil {
			p.close()
			return nil, err
		}
		spec.ArtifactUrl = ""
		p.drivers = append(p.drivers, preparedInstallDriver{spec: spec, path: path})
	}
	return p, nil
}

func (p *osInstallDrivers) close() {
	if p == nil {
		return
	}
	for _, d := range p.drivers {
		_ = os.Remove(d.path)
	}
}

var connectInstallDriverDevice = connectWithAutoTLS

// Only dial the name provisioned by this install, never a default or the first
// discovered board. A matching hostname, board and kernel must precede writes.
func (p *osInstallDrivers) installAfterBoot(ctx context.Context, name string) error {
	if p == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	address := name + ".local:50051"
	cliLogln("Waiting for %s to boot. Keep its USB network cable connected; driver installation will continue automatically.", name)
	var lastErr error
	for {
		probeCtx, stopProbe := context.WithTimeout(ctx, 10*time.Second)
		conn, err := connectInstallDriverDevice(probeCtx, address)
		if err == nil {
			err = p.checkDevice(probeCtx, conn, name)
		}
		stopProbe()
		if err == nil {
			defer conn.Close()
			for _, d := range p.drivers {
				if err := installDriver(ctx, conn, d.spec, d.path); err != nil {
					return fmt.Errorf("OS written, but installing driver %s after first boot failed: %w", d.spec.GetName(), err)
				}
			}
			cliLogln("Requested drivers installed on %s.", name)
			return nil
		}
		if conn != nil {
			_ = conn.Close()
		}
		lastErr = err
		select {
		case <-ctx.Done():
			return fmt.Errorf("OS written, but drivers were not installed on %s: %w (last check: %v); install the requested drivers with --device %s using the same catalog", name, ctx.Err(), lastErr, name)
		case <-time.After(2 * time.Second):
		}
	}
}

func (p *osInstallDrivers) checkDevice(ctx context.Context, conn *grpcclient.AgentConnection, name string) error {
	v, err := conn.AgentService.GetAgentVersion(ctx, &agentpb.GetAgentVersionRequest{})
	if err != nil {
		return err
	}
	if strings.TrimSuffix(v.GetHostname(), ".local") != name || v.GetDeviceType() != p.deviceType {
		return fmt.Errorf("device identity does not match the requested hostname and board")
	}
	dl, err := conn.DriverService.ListDrivers(ctx, &agentpbv2.ListDriversRequest{})
	if err != nil {
		return driverServiceErr(err)
	}
	if dl.GetKernelVersion() != p.kernel {
		return fmt.Errorf("device runs kernel %s; waiting for image kernel %s", dl.GetKernelVersion(), p.kernel)
	}
	return nil
}
