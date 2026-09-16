package commands

import (
	"fmt"
	"io"
	"path/filepath"

	diskfs "github.com/diskfs/go-diskfs"
	"github.com/diskfs/go-diskfs/disk"
	"github.com/diskfs/go-diskfs/filesystem"
	"github.com/wendylabsinc/wendy/go/internal/cli/qdl"
	"github.com/wendylabsinc/wendy/go/internal/shared/wendyconf"
)

const (
	// What wendyos-config-init.sh looks for to decide the partition already
	// holds a seed; any other label and it reformats on first boot.
	dragonwingConfigLabel = "config"

	dragonwingConfigImageName = "wendy-config.img"
)

// resolveSeedAgent fetches the agent to seed: best-effort, and the latest stable
// one whatever image is being flashed, as every other board does. It is the only
// network step in a flash, so an offline re-flash must still seed wifi and
// enrollment. The warning it returns is for the caller to surface — a step's own
// output only reaches the screen when that step fails.
func resolveSeedAgent(out io.Writer, detail func(string)) ([]byte, string) {
	detail("downloading agent")
	agent, version, _, err := resolveAgentArtifact("linux", "arm64", false)
	if err != nil {
		// Written to out as well, so the flash log keeps it for a post-mortem.
		fmt.Fprintf(out, "warning: could not download wendy-agent (%v)\n", err)
		return nil, fmt.Sprintf("could not download wendy-agent (%v); the board keeps the agent baked into its image", err)
	}
	detail("agent " + version)
	return agent, ""
}

// newConfigFS creates and formats a bare FAT32 config image. The caller closes
// the returned disk, and that Close is the flush.
//
// Size and sector size both come from the descriptor: the payload must match the
// declared size exactly, and Linux refuses to mount a FAT whose logical sector
// size is under the device's.
func newConfigFS(path string, size int64, sectorSize int64) (*disk.Disk, filesystem.FileSystem, error) {
	d, err := diskfs.Create(path, size, diskfs.SectorSize(sectorSize))
	if err != nil {
		return nil, nil, fmt.Errorf("creating config image: %w", err)
	}
	fs, err := d.CreateFilesystem(disk.FilesystemSpec{
		Partition:   0, // no partition table: the image IS the partition
		FSType:      filesystem.TypeFat32,
		VolumeLabel: dragonwingConfigLabel,
	})
	if err != nil {
		_ = d.Close()
		return nil, nil, fmt.Errorf("formatting config image: %w", err)
	}
	return d, fs, nil
}

// buildDragonwingConfigImage writes a seeded config-partition image into dir and
// returns its path. The Dragonwing bundle ships no config image to inject into —
// its descriptor declares the partition with an empty filename — so unlike Thor
// and Orin the host has to make one.
func buildDragonwingConfigImage(dir string, plan *qdl.FlashPlan, agent []byte,
	creds []wendyconf.WifiCredential, deviceName string, provJSON []byte) (string, error) {
	declared, err := plan.Declared(dragonwingConfigLabel)
	if err != nil {
		return "", err
	}
	if declared.SizeKB <= 0 {
		return "", fmt.Errorf("the flash descriptor declares no size for partition %q", dragonwingConfigLabel)
	}
	path := filepath.Join(dir, dragonwingConfigImageName)
	d, fs, err := newConfigFS(path, int64(declared.SizeKB*1024), int64(declared.SectorSize))
	if err != nil {
		return "", err
	}
	if err := writeConfigFilesTo(fatWriter{fs}, agent, creds, deviceName, provJSON); err != nil {
		_ = d.Close()
		return "", err
	}
	if err := d.Close(); err != nil {
		return "", fmt.Errorf("finishing config image: %w", err)
	}
	return path, nil
}
