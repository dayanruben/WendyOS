//go:build windows

package t234

import (
	"fmt"
	"os"
)

// blockDeviceSize reports the size of dev in bytes. Regular files short-circuit
// to Stat like the other platforms.
//
// PLACEHOLDER: the IOCTL_DISK_GET_LENGTH_INFO path for \\.\PhysicalDriveN
// handles follows.
func blockDeviceSize(dev *os.File) (int64, error) {
	info, err := dev.Stat()
	if err != nil {
		return 0, err
	}
	if info.Mode().IsRegular() {
		return info.Size(), nil
	}
	return 0, fmt.Errorf("raw disk sizing is not implemented on Windows yet")
}
