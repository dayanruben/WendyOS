//go:build !darwin && !linux

package commands

import (
	"context"
	"fmt"
)

// installOrin is macOS/Linux-only (USB recovery flashing uses gousb/libusb).
func installOrin(_ context.Context, opts t234InstallOptions) error {
	return fmt.Errorf("full USB recovery for %s is supported on macOS and Linux only; use --rootfs-only on Windows to write an explicit SD/NVMe image", opts.DeviceType)
}
