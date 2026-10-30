//go:build windows

package commands

import (
	"errors"
	"strings"

	"github.com/wendylabsinc/wendy/go/internal/cli/tegraflash/rcm"
	"github.com/wendylabsinc/wendy/go/internal/cli/tegraflash/winusb"
)

func scanInstallUSBDevices() ([]installUSBDevice, error) {
	var devices []installUSBDevice
	var scanErr error
	for _, vendor := range []uint16{rcm.VendorNVIDIA, 0x05c6, 0x303a} {
		found, err := winusb.ListVendor(vendor)
		scanErr = errors.Join(scanErr, err)
		for _, d := range found {
			// Composite interface devnodes repeat the parent's VID/PID. Count
			// only the physical USB device, as libusb does on macOS/Linux.
			if strings.Contains(strings.ToUpper(d.InstanceID), "&MI_") {
				continue
			}
			device := installUSBDevice{vendor: d.VID, product: d.PID}
			if installUSBTarget(device) != "" {
				devices = append(devices, device)
			}
		}
	}
	return devices, scanErr
}
