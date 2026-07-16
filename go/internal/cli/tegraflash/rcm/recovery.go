package rcm

import "fmt"

// RecoveryDevice identifies a Jetson sitting in USB recovery mode. PathKey is the
// physical USB location (bus + parent-port chain); it is stable across the
// re-enumeration the device undergoes between RCM boot and the ADB flashing gadget,
// so it is the right handle for "flash this specific board".
type RecoveryDevice struct {
	PathKey string // e.g. "20-1.2" (bus 20, hub port 1 → port 2)
	Product uint16 // USB PID: 0x7<module>23 for T234 modules, 0x7026 Thor
	ECID    string // chip BR_CID read over EP0 (may be empty if unreadable)
}

// IsThor reports whether the device is a T264 (AGX Thor).
func (r RecoveryDevice) IsThor() bool { return r.Product == uint16(ProductThor) }

// IsOrin reports whether the device is a T234 (Orin family: AGX Orin, Orin NX,
// Orin Nano — each module SKU has its own recovery PID).
func (r RecoveryDevice) IsOrin() bool { return IsT234RecoveryPID(r.Product) }

// IsOrinAGX reports whether the device is an AGX Orin module.
func (r RecoveryDevice) IsOrinAGX() bool {
	return r.Product == uint16(ProductOrinAGX32) || r.Product == uint16(ProductOrinAGX64)
}

// IsOrinNano reports whether the device is an Orin Nano module.
func (r RecoveryDevice) IsOrinNano() bool {
	return r.Product == uint16(ProductOrinNano8) || r.Product == uint16(ProductOrinNano4)
}

// Describe returns a one-line human label for pickers/logs.
func (r RecoveryDevice) Describe() string {
	chip := "Jetson"
	if r.IsThor() {
		chip = "AGX Thor (T264)"
	} else if name, ok := T234ModuleName(r.Product); ok {
		chip = name + " (T234)"
	}
	ecid := r.ECID
	if ecid == "" {
		ecid = "unknown"
	}
	return fmt.Sprintf("%s  [usb %s, ECID %s]", chip, r.PathKey, ecid)
}
