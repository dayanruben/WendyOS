//go:build linux

package commands

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// markFATVolumesUnindexed writes the Spotlight marker to every vfat partition
// of d, so a card flashed here does not hang its next flash on a Mac.
func markFATVolumesUnindexed(d drive) error {
	runWithTimeout(markerTimeout, "sudo", "-n", "partprobe", d.DevicePath) //nolint:errcheck
	runWithTimeout(markerTimeout, "udevadm", "settle")                     //nolint:errcheck
	out, err := runWithTimeout(markerTimeout, "lsblk", "-o", "NAME,FSTYPE", "-n", "-r", d.DevicePath)
	if err != nil {
		return fmt.Errorf("lsblk %s: %w", d.DevicePath, err)
	}
	var errs []error
	for _, part := range parseLsblkFAT(string(out)) {
		if err := markLinuxVolume(part); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", part, err))
		}
	}
	return errors.Join(errs...)
}

func markLinuxVolume(part string) error {
	dir, err := os.MkdirTemp("", "wendyos-fat-*")
	if err != nil {
		return err
	}
	// os.Remove, not RemoveAll: if the unmount fails, RemoveAll would empty
	// the volume through the mount.
	defer os.Remove(dir) //nolint:errcheck

	opts := fmt.Sprintf("uid=%d,gid=%d", os.Getuid(), os.Getgid())
	if out, err := runWithTimeout(markerTimeout, "sudo", "-n", "mount", "-t", "vfat", "-o", opts, part, dir); err != nil {
		return fmt.Errorf("mount: %s: %w", strings.TrimSpace(string(out)), err)
	}
	defer runWithTimeout(markerTimeout, "sudo", "-n", "umount", dir) //nolint:errcheck

	return os.WriteFile(filepath.Join(dir, spotlightMarker), nil, 0o644)
}
