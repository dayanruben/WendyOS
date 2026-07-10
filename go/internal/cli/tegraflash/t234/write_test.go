//go:build darwin || linux

package t234

import (
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"unicode/utf8"
)

// TestRawSyncError guards the macOS raw-device flush behavior: fsync on a raw
// character device (/dev/rdiskN) returns ENOTTY, which must be tolerated (the
// unbuffered writes have already landed), while any other error must surface.
func TestRawSyncError(t *testing.T) {
	if err := rawSyncError("/dev/rdisk42", nil); err != nil {
		t.Fatalf("nil sync error should be nil, got %v", err)
	}
	if err := rawSyncError("/dev/rdisk42", syscall.ENOTTY); err != nil {
		t.Fatalf("ENOTTY should be tolerated on a raw device, got %v", err)
	}
	// A wrapped ENOTTY (as os.File.Sync returns a *PathError) is still tolerated.
	if err := rawSyncError("/dev/rdisk42", &pathErr{syscall.ENOTTY}); err != nil {
		t.Fatalf("wrapped ENOTTY should be tolerated, got %v", err)
	}
	if err := rawSyncError("/dev/rdisk42", syscall.EIO); err == nil {
		t.Fatal("a real sync error (EIO) must not be swallowed")
	} else if !errors.Is(err, syscall.EIO) {
		t.Fatalf("returned error should wrap EIO, got %v", err)
	}
}

func TestClampLabel(t *testing.T) {
	cases := []struct {
		name, in string
		max      int
		want     string
	}{
		{"short-ext4", "UDA", ext4LabelMax, "UDA"},
		{"exact-ext4", "0123456789abcdef", ext4LabelMax, "0123456789abcdef"},
		{"long-ext4", "this-label-is-way-too-long", ext4LabelMax, "this-label-is-wa"},
		{"short-fat32", "ESP", fat32LabelMax, "ESP"},
		{"long-fat32", "config-partition", fat32LabelMax, "config-part"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := clampLabel(tc.in, tc.max); got != tc.want {
				t.Fatalf("clampLabel(%q, %d) = %q, want %q", tc.in, tc.max, got, tc.want)
			}
		})
	}
	// A multi-byte rune straddling the limit is dropped whole, not split.
	if got := clampLabel("ααααααααα", ext4LabelMax); len(got) > ext4LabelMax || !utf8.ValidString(got) {
		t.Fatalf("clampLabel truncated a rune: %q (len %d)", got, len(got))
	}
}

func TestRunWriterXMLPlanFileBackedDisk(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "rootfs.img"), []byte("ROOTFS-CONTENT"), 0o644); err != nil {
		t.Fatal(err)
	}
	layout := filepath.Join(dir, "initrd-flash.xml")
	xml := `<?xml version="1.0"?><partition_layout version="01.00.0000"><device type="external" instance="0" sector_size="512">
<partition name="APP" id="1" type="data"><allocation_policy>sequential</allocation_policy><filesystem_type>basic</filesystem_type><size>8388608</size><allocation_attribute>8</allocation_attribute><filename>rootfs.img</filename></partition>
<partition name="data" id="2" type="data"><allocation_policy>sequential</allocation_policy><filesystem_type>ext4</filesystem_type><size>67108864</size><allocation_attribute>8</allocation_attribute></partition>
</device></partition_layout>`
	if err := os.WriteFile(layout, []byte(xml), 0o644); err != nil {
		t.Fatal(err)
	}
	disk := filepath.Join(dir, "disk.img")
	f, err := os.Create(disk)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(96 << 20); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if err := RunWriter(WriterOptions{Device: disk, WritePlan: true, LayoutPath: layout, ImagesDir: dir, RootfsDevice: "nvme0n1"}); err != nil {
		t.Fatal(err)
	}
	p, err := LoadXMLPlan(layout, dir, "nvme0n1")
	if err != nil {
		t.Fatal(err)
	}
	dev, _ := os.Open(disk)
	defer dev.Close()
	buf := make([]byte, len("ROOTFS-CONTENT"))
	if _, err := dev.ReadAt(buf, p.Partitions[0].StartSector*sectorSize); err != nil || string(buf) != "ROOTFS-CONTENT" {
		t.Fatalf("rootfs content = %q, err=%v", buf, err)
	}
	magic := make([]byte, 2)
	if _, err := dev.ReadAt(magic, p.Partitions[1].StartSector*sectorSize+1024+56); err != nil || binary.LittleEndian.Uint16(magic) != ext4Magic {
		t.Fatalf("blank ext4 magic = %x, err=%v", magic, err)
	}
}

// pathErr mimics *os.PathError so errors.Is unwrapping is exercised.
type pathErr struct{ err error }

func (e *pathErr) Error() string { return "sync: " + e.err.Error() }
func (e *pathErr) Unwrap() error { return e.err }
