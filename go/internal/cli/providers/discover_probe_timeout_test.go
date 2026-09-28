package providers

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// shrinkDiscoverProbeTimeout makes the probe bound short enough for a test.
func shrinkDiscoverProbeTimeout(t *testing.T) {
	t.Helper()
	prev := discoverProbeTimeout
	discoverProbeTimeout = 200 * time.Millisecond
	t.Cleanup(func() { discoverProbeTimeout = prev })
}

// fakeDockerOnPath puts a `docker` shell script with the given body first on PATH.
func fakeDockerOnPath(t *testing.T, body string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake docker is a shell script")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "docker"), []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
}

// A hung Docker daemon used to block `wendy discover` for minutes: the
// `docker version` probe ran under a context with no deadline.
func TestDockerDiscoverDevices_HungDaemonIsBounded(t *testing.T) {
	shrinkDiscoverProbeTimeout(t)
	fakeDockerOnPath(t, "/bin/sleep 30\necho 28.0.0\n")

	start := time.Now()
	devices, err := (&DockerProvider{}).DiscoverDevices(context.Background())
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("DiscoverDevices: %v", err)
	}
	if len(devices) != 0 {
		t.Fatalf("devices = %+v, want none from an unresponsive daemon", devices)
	}
	if elapsed > 5*time.Second {
		t.Fatalf("DiscoverDevices took %s with a hung daemon; want it bounded by discoverProbeTimeout", elapsed)
	}
}

func TestDockerDiscoverDevices_ResponsiveDaemonListed(t *testing.T) {
	fakeDockerOnPath(t, "echo 28.5.1\n")

	devices, err := (&DockerProvider{}).DiscoverDevices(context.Background())
	if err != nil {
		t.Fatalf("DiscoverDevices: %v", err)
	}
	if len(devices) != 1 || devices[0].AgentVersion != "28.5.1" {
		t.Fatalf("devices = %+v, want one Docker device at 28.5.1", devices)
	}
}

func TestAppleContainerDiscoverDevices_HungCLIIsBounded(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses /bin/sleep")
	}
	shrinkDiscoverProbeTimeout(t)
	oldCmd, oldLook := appleContainerCommandContext, appleContainerLookPath
	oldGOOS, oldGOARCH := appleContainerHostGOOS, appleContainerHostGOARCH
	t.Cleanup(func() {
		appleContainerCommandContext, appleContainerLookPath = oldCmd, oldLook
		appleContainerHostGOOS, appleContainerHostGOARCH = oldGOOS, oldGOARCH
	})
	appleContainerHostGOOS = func() string { return "darwin" }
	appleContainerHostGOARCH = func() string { return "arm64" }
	appleContainerLookPath = func(string) (string, error) { return "/usr/local/bin/container", nil }
	appleContainerCommandContext = func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "/bin/sleep", "30")
	}

	start := time.Now()
	devices, err := (&AppleContainerProvider{}).DiscoverDevices(context.Background())
	elapsed := time.Since(start)

	if err != nil || len(devices) != 0 {
		t.Fatalf("DiscoverDevices = %+v, %v; want no devices and no error", devices, err)
	}
	if elapsed > 5*time.Second {
		t.Fatalf("DiscoverDevices took %s with a hung container CLI; want it bounded", elapsed)
	}
}
