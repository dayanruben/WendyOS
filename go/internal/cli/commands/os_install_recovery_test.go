//go:build darwin || linux

package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wendylabsinc/wendy/go/internal/cli/tegraflash/flashpack"
	"github.com/wendylabsinc/wendy/go/internal/cli/tegraflash/rcm"
)

func TestRecoveryFlagCombinationsFailBeforeManifestLookup(t *testing.T) {
	tests := [][]string{
		{"--device-type", orinDeviceType, "--drive", "/dev/disk9"},
		{"--device-type", orinNanoDeviceType, "--no-bmap"},
		{"--device-type", orinDeviceType, "--yes-overwrite-internal"},
		{"--device-type", orinDeviceType, "--rootfs-only", "--storage", "emmc"},
	}
	for _, args := range tests {
		cmd := newOSInstallCmd()
		cmd.SetArgs(args)
		if err := cmd.Execute(); err == nil {
			t.Fatalf("args %v unexpectedly accepted", args)
		}
	}
}

func TestChooseT234RecoveryStorage(t *testing.T) {
	if got, err := chooseT234RecoveryStorage(orinNanoDeviceType, ""); err != nil || got != "nvme" {
		t.Fatalf("Nano storage = %q, %v", got, err)
	}
	if _, err := chooseT234RecoveryStorage(orinNanoDeviceType, "emmc"); err == nil {
		t.Fatal("Nano eMMC recovery accepted")
	}
	if got, err := chooseT234RecoveryStorage(orinDeviceType, "emmc"); err != nil || got != "emmc" {
		t.Fatalf("AGX storage = %q, %v", got, err)
	}
}

func TestChooseT234RootfsOnlyStorage(t *testing.T) {
	tests := []struct {
		device, override, want string
		wantErr                bool
	}{
		{orinNanoDeviceType, "", "nvme", false},
		{orinNanoDeviceType, "sd", "sd", false},
		{orinNanoDeviceType, "nvme", "nvme", false},
		{orinDeviceType, "", "nvme", false},
		{orinDeviceType, "sd", "", true},
		{orinDeviceType, "emmc", "", true},
	}
	for _, tc := range tests {
		got, err := chooseT234RootfsOnlyStorage(tc.device, tc.override)
		if (err != nil) != tc.wantErr || got != tc.want {
			t.Errorf("chooseT234RootfsOnlyStorage(%q, %q) = %q, %v; want %q, err=%v", tc.device, tc.override, got, err, tc.want, tc.wantErr)
		}
	}
}

func TestRootfsOnlyManifestDoesNotFallBackToLegacy(t *testing.T) {
	dm := &deviceManifest{Versions: map[string]deviceVersion{"1": {InstallMode: "recovery", Path: "legacy.img", NVMEPath: "legacy-nvme.img"}}}
	if _, err := getRootfsOnlyImageInfo(dm, "1", "nvme"); err == nil {
		t.Fatal("rootfs-only resolver used a legacy image field")
	}
	dm.Versions["1"] = deviceVersion{InstallMode: "recovery", NVMERootfsOnlyPath: "rootfs.img", NVMERootfsOnlySizeBytes: 12}
	info, err := getRootfsOnlyImageInfo(dm, "1", "nvme")
	if err != nil || !strings.HasSuffix(info.DownloadURL, "/rootfs.img") {
		t.Fatalf("rootfs-only info = %+v, %v", info, err)
	}
}

func TestRecoveryManifestRoutesFlashpackByStorage(t *testing.T) {
	dm := &deviceManifest{Versions: map[string]deviceVersion{"1": {
		InstallMode:       "recovery",
		NVMEFlashpackPath: "nvme.flashpack", NVMEFlashpackChecksum: "n", NVMEFlashpackSizeBytes: 1,
		EMMCFlashpackPath: "emmc.flashpack", EMMCFlashpackChecksum: "e", EMMCFlashpackSizeBytes: 2,
	}}}
	nvme, err := getRecoveryFlashpackInfo(dm, orinDeviceType, "1", "nvme")
	if err != nil || !strings.HasSuffix(nvme.URL, "/nvme.flashpack") {
		t.Fatalf("NVMe flashpack = %+v, %v", nvme, err)
	}
	emmc, err := getRecoveryFlashpackInfo(dm, orinDeviceType, "1", "emmc")
	if err != nil || !strings.HasSuffix(emmc.URL, "/emmc.flashpack") {
		t.Fatalf("eMMC flashpack = %+v, %v", emmc, err)
	}
	dm.Versions["1"] = deviceVersion{EMMCFlashpackPath: "incomplete.flashpack"}
	if _, err := getRecoveryFlashpackInfo(dm, orinDeviceType, "1", "emmc"); err == nil {
		t.Fatal("incomplete eMMC flashpack metadata was accepted")
	}
}

func TestRecoverySelectorFiltersJetsonFamily(t *testing.T) {
	devs := []rcm.RecoveryDevice{{Product: rcm.ProductOrin}, {Product: rcm.ProductThor}}
	thor := filterRecoveryDevices(devs, func(d rcm.RecoveryDevice) bool { return d.IsThor() })
	if len(thor) != 1 || !thor[0].IsThor() {
		t.Fatalf("Thor filter = %+v", thor)
	}
	orin := filterRecoveryDevices(devs, func(d rcm.RecoveryDevice) bool { return d.IsOrin() })
	if len(orin) != 1 || !orin[0].IsOrin() {
		t.Fatalf("Orin filter = %+v", orin)
	}
}

func TestPrepareT234WorkspaceLeavesCacheImmutable(t *testing.T) {
	root := t.TempDir()
	images := filepath.Join(root, "stage2", "flash")
	if err := os.MkdirAll(images, 0o755); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(images, "config.img")
	if err := os.WriteFile(config, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(images, "rootfs.img"), []byte("rootfs"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(images, "layout.xml"), []byte("layout"), 0o644); err != nil {
		t.Fatal(err)
	}
	fp := &flashpack.Flashpack{Root: root}
	fp.Manifest.Layout.FlashImages = "stage2/flash"
	fp.Manifest.Layout.ConfigImage = "stage2/flash/config.img"
	fp.Manifest.Layout.PartitionLayout = "stage2/flash/layout.xml"
	workspace, _, err := prepareT234Workspace(fp)
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(workspace)
	if err := os.WriteFile(filepath.Join(workspace, "config.img"), []byte("changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(config)
	if string(data) != "original" {
		t.Fatalf("cached config was mutated: %q", data)
	}
}
