package services

import (
	"context"
	"fmt"
	"testing"

	"go.uber.org/zap"
	agentpb "github.com/wendylabsinc/wendy/proto/gen/agentpb"
)

func TestListV4L2Devices_TwoDevices(t *testing.T) {
	origGlob := globVideoDevices
	origRead := readDeviceName
	defer func() {
		globVideoDevices = origGlob
		readDeviceName = origRead
	}()

	globVideoDevices = func() ([]string, error) {
		return []string{"/dev/video0", "/dev/video1"}, nil
	}
	readDeviceName = func(base string) (string, error) {
		names := map[string]string{"video0": "USB Camera", "video1": "Integrated Camera"}
		if name, ok := names[base]; ok {
			return name, nil
		}
		return base, nil
	}

	devices, err := listV4L2Devices()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(devices) != 2 {
		t.Fatalf("expected 2 devices, got %d", len(devices))
	}
	if devices[0].GetId() != 0 || devices[0].GetName() != "USB Camera" || devices[0].GetPath() != "/dev/video0" {
		t.Errorf("device 0: got id=%d name=%q path=%q", devices[0].GetId(), devices[0].GetName(), devices[0].GetPath())
	}
	if devices[1].GetId() != 1 || devices[1].GetName() != "Integrated Camera" || devices[1].GetPath() != "/dev/video1" {
		t.Errorf("device 1: got id=%d name=%q path=%q", devices[1].GetId(), devices[1].GetName(), devices[1].GetPath())
	}
}

func TestListV4L2Devices_NoDevices(t *testing.T) {
	origGlob := globVideoDevices
	defer func() { globVideoDevices = origGlob }()

	globVideoDevices = func() ([]string, error) { return nil, nil }

	devices, err := listV4L2Devices()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(devices) != 0 {
		t.Fatalf("expected 0 devices, got %d", len(devices))
	}
}

func TestListV4L2Devices_SysfsReadFailFallsBackToPath(t *testing.T) {
	origGlob := globVideoDevices
	origRead := readDeviceName
	defer func() {
		globVideoDevices = origGlob
		readDeviceName = origRead
	}()

	globVideoDevices = func() ([]string, error) { return []string{"/dev/video0"}, nil }
	readDeviceName = func(base string) (string, error) { return "", fmt.Errorf("no sysfs") }

	devices, err := listV4L2Devices()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(devices) != 1 {
		t.Fatalf("expected 1 device, got %d", len(devices))
	}
	if devices[0].GetName() != "video0" {
		t.Errorf("expected fallback name 'video0', got %q", devices[0].GetName())
	}
}

func TestVideoService_ListVideoDevices(t *testing.T) {
	origGlob := globVideoDevices
	origRead := readDeviceName
	defer func() {
		globVideoDevices = origGlob
		readDeviceName = origRead
	}()

	globVideoDevices = func() ([]string, error) { return []string{"/dev/video0"}, nil }
	readDeviceName = func(base string) (string, error) { return "Test Camera", nil }

	svc := NewVideoService(zap.NewNop())
	resp, err := svc.ListVideoDevices(context.Background(), &agentpb.ListVideoDevicesRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.GetDevices()) != 1 {
		t.Fatalf("expected 1 device, got %d", len(resp.GetDevices()))
	}
	d := resp.GetDevices()[0]
	if d.GetId() != 0 || d.GetName() != "Test Camera" || d.GetPath() != "/dev/video0" {
		t.Errorf("unexpected device: id=%d name=%q path=%q", d.GetId(), d.GetName(), d.GetPath())
	}
}
