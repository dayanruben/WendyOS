//go:build windows

package t234

import "fmt"

// ReleaseUSB forces a USB-level disconnect of the flashing gadget.
//
// PLACEHOLDER: the devnode-removal implementation follows.
func ReleaseUSB(serial, port string) error {
	return fmt.Errorf("USB release is not implemented on Windows yet")
}
