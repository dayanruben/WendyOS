//go:build (darwin || linux) && !ios

package commands

import "github.com/google/gousb"

func scanInstallUSBDevices() ([]installUSBDevice, error) {
	ctx := gousb.NewContext()
	defer ctx.Close()
	var devices []installUSBDevice
	_, err := ctx.OpenDevices(func(d *gousb.DeviceDesc) bool {
		device := installUSBDevice{vendor: uint16(d.Vendor), product: uint16(d.Product)}
		if installUSBTarget(device) != "" {
			devices = append(devices, device)
		}
		return false // Enumerate descriptors without opening any device.
	})
	return devices, err
}
