//go:build darwin

package commands

import (
	"errors"
	"fmt"
	"path/filepath"
)

// markFATVolumesUnindexed writes the Spotlight marker to every FAT slice of d.
// mountConfigPartition works for any FAT slice, not just config.
func markFATVolumesUnindexed(d drive) error {
	out, err := runWithTimeout(markerTimeout, "diskutil", "list", d.DevicePath)
	if err != nil {
		return fmt.Errorf("diskutil list %s: %w", d.DevicePath, err)
	}
	var errs []error
	for _, slice := range parseDiskutilSlices(string(out), d.DevicePath) {
		info, err := runWithTimeout(markerTimeout, "diskutil", "info", slice)
		if err != nil || parseBundleType(string(info)) != "msdos" {
			continue
		}
		if err := markDarwinVolume(slice); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", slice, err))
		}
	}
	return errors.Join(errs...)
}

func markDarwinVolume(slice string) error {
	m, err := mountConfigPartition(slice)
	if err != nil {
		return err
	}
	defer m.release()
	return touchMarker(m.path, m.elevated)
}

// touchMarker creates the marker from a subprocess, so a wedged FAT driver
// costs a timeout rather than the install.
func touchMarker(dir string, elevated bool) error {
	p := filepath.Join(dir, spotlightMarker)
	if elevated {
		_, err := runWithTimeout(markerTimeout, "sudo", "-n", "/usr/bin/touch", p)
		return err
	}
	_, err := runWithTimeout(markerTimeout, "/usr/bin/touch", p)
	return err
}
