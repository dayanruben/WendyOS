//go:build windows

package t234

import "fmt"

// Windows discovery of the flashing initrd's USB mass-storage LUNs.
//
// PLACEHOLDER: real SetupAPI/DeviceIoControl implementations follow.

func listUMSDisks() ([]UMSDisk, error) {
	return nil, fmt.Errorf("USB mass-storage discovery is not implemented on Windows yet")
}

func rawUMSInquiry() string { return "" }

func tegraUSBHint() string {
	return "USB device identity reporting is not implemented on Windows yet."
}

func unmountUMSDisk(UMSDisk) {}

func ejectUMSDisk(UMSDisk) {}
