package commands

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	"github.com/wendylabsinc/wendy/internal/shared/config"
	"github.com/wendylabsinc/wendy/internal/shared/models"
	"github.com/wendylabsinc/wendy/proto/gen/agentpb"
)

func TestLANAgentAddressesPrefersIPAddress(t *testing.T) {
	dev := models.LANDevice{
		IPAddress: "192.168.1.23",
		Hostname:  "wendyos-otter.local",
		Port:      defaultAgentPort,
	}

	got := lanAgentAddresses(dev)
	want := []string{
		"192.168.1.23:50051",
		"wendyos-otter.local:50051",
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("lanAgentAddresses() = %v, want %v", got, want)
	}
}

func TestLANAgentAddressesDeduplicatesIdenticalHosts(t *testing.T) {
	dev := models.LANDevice{
		IPAddress: "192.168.1.23",
		Hostname:  "192.168.1.23",
		Port:      defaultAgentPort,
	}

	got := lanAgentAddresses(dev)
	want := []string{"192.168.1.23:50051"}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("lanAgentAddresses() = %v, want %v", got, want)
	}
}

func TestLANAgentAddressesFallsBackToDefaultPort(t *testing.T) {
	dev := models.LANDevice{
		IPAddress: "192.168.1.23",
		Hostname:  "wendyos-otter.local",
	}

	got := lanAgentAddresses(dev)
	want := []string{
		"192.168.1.23:50051",
		"wendyos-otter.local:50051",
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("lanAgentAddresses() = %v, want %v", got, want)
	}
}

func TestResolveLANAgentVersionFallsBackAcrossAddresses(t *testing.T) {
	orig := getAgentVersionAtAddress
	defer func() { getAgentVersionAtAddress = orig }()

	var (
		mu    sync.Mutex
		calls []string
	)
	getAgentVersionAtAddress = func(_ context.Context, address string) (*agentpb.GetAgentVersionResponse, error) {
		mu.Lock()
		calls = append(calls, address)
		mu.Unlock()

		if address == "192.168.1.23:50051" {
			return nil, errors.New("dial tcp 192.168.1.23:50051: i/o timeout")
		}
		return &agentpb.GetAgentVersionResponse{Version: "1.2.3"}, nil
	}

	dev := models.LANDevice{
		IPAddress: "192.168.1.23",
		Hostname:  "wendyos-otter.local",
		Port:      defaultAgentPort,
	}

	address, resp, err := resolveLANAgentVersion(context.Background(), dev)
	if err != nil {
		t.Fatalf("resolveLANAgentVersion() error = %v", err)
	}

	if address != "wendyos-otter.local:50051" {
		t.Fatalf("resolveLANAgentVersion() address = %q, want %q", address, "wendyos-otter.local:50051")
	}
	if resp.GetVersion() != "1.2.3" {
		t.Fatalf("resolveLANAgentVersion() version = %q, want %q", resp.GetVersion(), "1.2.3")
	}

	wantCalls := []string{
		"192.168.1.23:50051",
		"wendyos-otter.local:50051",
	}
	if !reflect.DeepEqual(calls, wantCalls) {
		t.Fatalf("resolveLANAgentVersion() calls = %v, want %v", calls, wantCalls)
	}
}

// setTempConfig writes a Config to a temp dir and sets HOME so config.Load
// reads from it. It returns a cleanup function that restores the original HOME.
func setTempConfig(t *testing.T, cfg *config.Config) func() {
	t.Helper()
	origHome := os.Getenv("HOME")
	tmp := t.TempDir()
	wendyDir := filepath.Join(tmp, ".wendy")
	if err := os.MkdirAll(wendyDir, 0o700); err != nil {
		t.Fatal(err)
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wendyDir, "config.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	os.Setenv("HOME", tmp)
	return func() { os.Setenv("HOME", origHome) }
}

func TestResolveDeviceAddress_Flag(t *testing.T) {
	origFlag := deviceFlag
	defer func() { deviceFlag = origFlag }()
	deviceFlag = "my-device.local"

	addr, isDefault, err := resolveDeviceAddress()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if isDefault {
		t.Fatal("expected isDefault=false when --device flag is set")
	}
	if addr != "my-device.local:50051" {
		t.Fatalf("addr = %q, want %q", addr, "my-device.local:50051")
	}
}

func TestResolveDeviceAddress_DefaultDevice(t *testing.T) {
	origFlag := deviceFlag
	defer func() { deviceFlag = origFlag }()
	deviceFlag = ""

	cleanup := setTempConfig(t, &config.Config{DefaultDevice: "wendy-thor.local"})
	defer cleanup()

	addr, isDefault, err := resolveDeviceAddress()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !isDefault {
		t.Fatal("expected isDefault=true when using default device from config")
	}
	if addr != "wendy-thor.local:50051" {
		t.Fatalf("addr = %q, want %q", addr, "wendy-thor.local:50051")
	}
}

func TestResolveDeviceAddress_NoDevice(t *testing.T) {
	origFlag := deviceFlag
	defer func() { deviceFlag = origFlag }()
	deviceFlag = ""

	cleanup := setTempConfig(t, &config.Config{})
	defer cleanup()

	_, _, err := resolveDeviceAddress()
	if err == nil {
		t.Fatal("expected error when no device is specified")
	}
}

func TestResolveLANVersionsKeepsDevicesWhenMetadataLookupFails(t *testing.T) {
	orig := getAgentVersionAtAddress
	defer func() { getAgentVersionAtAddress = orig }()

	getAgentVersionAtAddress = func(_ context.Context, address string) (*agentpb.GetAgentVersionResponse, error) {
		return nil, errors.New("unreachable: " + address)
	}

	devices := []models.LANDevice{
		{
			DisplayName: "Wendy One",
			Hostname:    "wendy-one.local",
			IPAddress:   "192.168.1.10",
			Port:        defaultAgentPort,
		},
		{
			DisplayName: "Wendy Two",
			Hostname:    "wendy-two.local",
			IPAddress:   "192.168.1.11",
			Port:        defaultAgentPort,
		},
	}

	expected := make([]models.LANDevice, len(devices))
	copy(expected, devices)

	got := resolveLANVersions(context.Background(), devices)

	if len(got) != len(expected) {
		t.Fatalf("resolveLANVersions() returned %d devices, want %d", len(got), len(expected))
	}
	for i := range expected {
		if got[i].DisplayName != expected[i].DisplayName {
			t.Fatalf("resolveLANVersions()[%d].DisplayName = %q, want %q", i, got[i].DisplayName, expected[i].DisplayName)
		}
		if got[i].IPAddress != expected[i].IPAddress {
			t.Fatalf("resolveLANVersions()[%d].IPAddress = %q, want %q", i, got[i].IPAddress, expected[i].IPAddress)
		}
		if got[i].AgentVersion != "" {
			t.Fatalf("resolveLANVersions()[%d].AgentVersion = %q, want empty", i, got[i].AgentVersion)
		}
	}
}
