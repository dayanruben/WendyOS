//go:build !linux

package commands

import (
	"fmt"
	"runtime"

	"github.com/spf13/cobra"
)

// usbSetupRun is unsupported off Linux. macOS brings up the USB-C gadget link
// automatically; Windows users configure the gadget network adapter manually.
func usbSetupRun(cmd *cobra.Command, opts usbSetupOptions) error {
	_ = opts
	switch runtime.GOOS {
	case "darwin":
		fmt.Fprintln(cmd.OutOrStdout(),
			"macOS brings up the USB-C link to a Wendy device automatically — no setup needed.\n"+
				"If the device isn't found, check System Settings ▸ Network for the USB adapter, then run 'wendy discover'.")
		return nil
	default:
		return fmt.Errorf("'wendy device usb-setup' configures a Linux host's USB-C link and is not supported on %s.\n"+
			"  Configure the USB gadget network adapter manually, then run 'wendy discover'.", runtime.GOOS)
	}
}
