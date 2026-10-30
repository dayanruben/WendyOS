//go:build darwin || linux || windows

package commands

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/wendylabsinc/wendy/go/internal/cli/grpcclient"
	"github.com/wendylabsinc/wendy/go/proto/gen/agentpb"
	agentpbv2 "github.com/wendylabsinc/wendy/go/proto/gen/agentpb/v2"
)

func TestOSInstallDriversPreparePinsImageKernel(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TMPDIR", dir)
	previous := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = previous })
	digest := sha256.Sum256([]byte("driver payload"))
	downloads := 0
	http.DefaultTransport = driverManifestTransport(func(req *http.Request) (*http.Response, error) {
		var body string
		switch req.URL.Path {
		case "/wendyos-images-public/manifests/master.json":
			body = `{"devices":{"dragonwing-iq-9075":{"latest_nightly":"nightly-1","manifest_path":"manifests/iq9.json"}}}`
		case "/wendyos-images-public/manifests/iq9.json":
			body = fmt.Sprintf(`{"versions":{"nightly-1":{"devkit":{"kernel_version":"new-kernel"},"extensions":[
{"name":"intel-be202","kernel_version":"old-kernel","path":"wrong.raw"},
{"name":"intel-be202","kernel_version":"new-kernel","path":"intel.raw","sha256":%q,"signature":"AQID","modules_load":["iwlmld"]}
]}}}`, hex.EncodeToString(digest[:]))
		case "/wendyos-images-public/intel.raw":
			downloads++
			body = "driver payload"
		default:
			t.Fatalf("unexpected request %s", req.URL)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})
	p, err := prepareOSInstallDrivers(context.Background(), "dragonwing-iq-9075", "", true, 0, []string{"intel-be202", "intel-be202"})
	if err != nil {
		t.Fatal(err)
	}
	defer p.close()
	if p.version != "nightly-1" || p.kernel != "new-kernel" || len(p.drivers) != 1 || downloads != 1 {
		t.Fatalf("incorrect pinned selection: %+v, downloads=%d", p, downloads)
	}
	spec := p.drivers[0].spec
	if spec.GetArtifactUrl() != "" || spec.GetSha256() != hex.EncodeToString(digest[:]) || len(spec.GetSignature()) != 3 || spec.GetModulesLoad()[0] != "iwlmld" {
		t.Fatalf("published metadata not preserved: %v", spec)
	}
	p.close()
	// A missing second driver must clean up the first download before flashing.
	if _, err := prepareOSInstallDrivers(context.Background(), "dragonwing-iq-9075", "", true, 0, []string{"intel-be202", "missing"}); err == nil {
		t.Fatal("accepted missing driver")
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("downloads left behind: %v, %v", entries, err)
	}
}

func TestOSInstallDriversAfterBootTargetsOnlyNamedDevice(t *testing.T) {
	path := t.TempDir() + "/driver.raw"
	if err := os.WriteFile(path, []byte("payload"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, mismatch := range []string{"", "name", "board", "kernel"} {
		t.Run(mismatch, func(t *testing.T) {
			var installs []*fakeInstallStream
			deviceType := "dragonwing-iq-9075"
			hostname := "iq9-mesh"
			kernel := "expected-kernel"
			switch mismatch {
			case "name":
				hostname = "another-device"
			case "board":
				deviceType = "raspberry-pi-5"
			case "kernel":
				kernel = "old-kernel"
			}
			conn := &grpcclient.AgentConnection{
				AgentService:  &fakeAgentVersionClient{resp: &agentpb.GetAgentVersionResponse{DeviceType: &deviceType, Hostname: hostname}},
				DriverService: fakeDriverClient{installs: &installs, list: &agentpbv2.ListDriversResponse{KernelVersion: kernel}},
			}
			previous := connectInstallDriverDevice
			t.Cleanup(func() { connectInstallDriverDevice = previous })
			connectInstallDriverDevice = func(_ context.Context, address string) (*grpcclient.AgentConnection, error) {
				if address != "iq9-mesh.local:50051" {
					t.Fatalf("unexpected device %s", address)
				}
				return conn, nil
			}
			p := &osInstallDrivers{deviceType: "dragonwing-iq-9075", kernel: "expected-kernel", drivers: []preparedInstallDriver{{spec: &agentpbv2.DriverSpec{Name: "intel-be202"}, path: path}}}
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			err := p.installAfterBoot(ctx, "iq9-mesh")
			if mismatch != "" {
				if err == nil || len(installs) != 0 {
					t.Fatalf("mismatched device was modified: installs=%v err=%v", installs, err)
				}
			} else if err != nil || len(installs) != 1 || string(installs[0].data) != "payload" {
				t.Fatalf("driver upload failed: installs=%v err=%v", installs, err)
			}
		})
	}
}

func TestOSInstallDriversRequiresExplicitTarget(t *testing.T) {
	for _, args := range [][]string{
		{"--driver", "intel-be202"},
		{"--driver", "intel-be202", "--device-type", "dragonwing-iq-9075"},
		{"--driver", "intel-be202", "image.img", "/dev/disk99"},
	} {
		cmd := newOSInstallCmd()
		cmd.SetArgs(args)
		cmd.SetOut(io.Discard)
		cmd.SetErr(io.Discard)
		if err := cmd.Execute(); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
}
