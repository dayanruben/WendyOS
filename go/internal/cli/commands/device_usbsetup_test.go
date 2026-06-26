package commands

import "testing"

func TestNewDeviceUSBSetupCmd_Flags(t *testing.T) {
	cmd := newDeviceUSBSetupCmd()
	if cmd.Use != "usb-setup" {
		t.Fatalf("Use = %q, want usb-setup", cmd.Use)
	}
	for _, f := range []string{"iface", "shared", "dry-run", "check", "undo"} {
		if cmd.Flags().Lookup(f) == nil {
			t.Errorf("missing flag --%s", f)
		}
	}
}
