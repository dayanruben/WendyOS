package flashpack

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeT234ManifestFixture(t *testing.T, schema int) string {
	t.Helper()
	root := t.TempDir()
	paths := []string{"stage1/br.bct", "stage1/mem.bct", "stage1/blob.bin", "stage2/flashpkg.ext4", "stage2/flash/initrd-flash.xml", "stage2/flash/config.img", "stage2/flash/rootfs.img"}
	files := map[string]any{}
	for _, rel := range paths {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if rel == "stage2/flashpkg.ext4" {
			file, err := os.Create(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := file.Truncate(t234FlashPackageSize); err != nil {
				file.Close()
				t.Fatal(err)
			}
			if err := file.Close(); err != nil {
				t.Fatal(err)
			}
		} else if err := os.WriteFile(path, []byte(rel), 0o644); err != nil {
			t.Fatal(err)
		}
		sum, _ := sha256File(path)
		info, _ := os.Stat(path)
		files[rel] = map[string]any{"sha256": sum, "size": info.Size()}
	}
	m := map[string]any{
		"schema": schema, "family": "t234", "protocol": T234ProtocolMassStorage,
		"usb_product_id": "0x7023", "wendyos_version": "0.18.0", "rootfs_device": "nvme0n1",
		"target":     map[string]any{"device": "jetson-orin-nano", "storage": "nvme", "module_id": "3767", "module_sku": "0005", "carrier_id": "3768", "carrier_sku": "0000"},
		"rcm_phases": []any{[]any{map[string]any{"type": "bct_br", "file": "stage1/br.bct"}}, []any{map[string]any{"type": "bct_mem", "file": "stage1/mem.bct"}, map[string]any{"type": "blob", "file": "stage1/blob.bin"}}},
		"layout":     map[string]any{"stage1": "stage1", "flash_package_image": "stage2/flashpkg.ext4", "flash_package_status": "flashpkg/status", "flash_package_logs": "flashpkg/logs", "flash_images": "stage2/flash", "partition_layout": "stage2/flash/initrd-flash.xml", "config_image": "stage2/flash/config.img"},
		"files":      files,
	}
	data, _ := json.Marshal(m)
	if err := os.WriteFile(filepath.Join(root, "manifest.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestT234SchemaV1Rejected(t *testing.T) {
	_, err := open(writeT234ManifestFixture(t, 1))
	if err == nil || !strings.Contains(err.Error(), "unsafe/unsupported") {
		t.Fatalf("schema-v1 error = %v", err)
	}
}

func TestT234SchemaV2VerifiesEveryFile(t *testing.T) {
	root := writeT234ManifestFixture(t, 2)
	fp, err := open(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := fp.verifyIntegrity(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "stage2/flash/rootfs.img"), []byte("tampered"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := fp.verifyIntegrity(); err == nil || !strings.Contains(err.Error(), "wrong size") {
		t.Fatalf("tampered-file error = %v", err)
	}
}

func TestT234SchemaV2RejectsUnsupportedTargetMapping(t *testing.T) {
	root := writeT234ManifestFixture(t, 2)
	data, err := os.ReadFile(filepath.Join(root, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest map[string]any
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	manifest["rootfs_device"] = "mmcblk0"
	data, _ = json.Marshal(manifest)
	if err := os.WriteFile(filepath.Join(root, "manifest.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := open(root); err == nil || !strings.Contains(err.Error(), "unsupported identity/rootfs mapping") {
		t.Fatalf("target-mapping error = %v", err)
	}
}

func TestT234SchemaV2RejectsUnhashedStagedImage(t *testing.T) {
	root := writeT234ManifestFixture(t, 2)
	if err := os.WriteFile(filepath.Join(root, "stage2/flash/unhashed.img"), []byte("untrusted"), 0o644); err != nil {
		t.Fatal(err)
	}
	fp, err := open(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := fp.verifyIntegrity(); err == nil || !strings.Contains(err.Error(), "omits staged image") {
		t.Fatalf("unhashed-image error = %v", err)
	}
}

func TestResolveRecoveryRejectsVersionMismatch(t *testing.T) {
	cache := t.TempDir()
	ref := RecoveryRef{Device: "jetson-orin-nano", Storage: "nvme", Version: "0.19.0"}
	dest := RecoveryExtractedCachePath(cache, ref)
	if err := os.Rename(writeT234ManifestFixture(t, 2), dest); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveRecovery(cache, ref); err == nil || !strings.Contains(err.Error(), "version") {
		t.Fatalf("version-mismatch error = %v", err)
	}
}

func TestUntaggedThorSchemaV1StillAccepted(t *testing.T) {
	root := t.TempDir()
	data := []byte(`{"schema":1,"wendyos_version":"0.18.0","layout":{"stage1":"stage1","flash_workspace":"stage2/out/flash_workspace"},"files":{}}`)
	if err := os.WriteFile(filepath.Join(root, "manifest.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := open(root); err != nil {
		t.Fatalf("legacy Thor rejected: %v", err)
	}
}
