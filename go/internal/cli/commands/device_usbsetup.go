package commands

import (
	"github.com/spf13/cobra"
)

// usbSetupOptions holds the flags for `wendy device usb-setup`.
type usbSetupOptions struct {
	iface  string // override the gadget interface; empty = auto-detect
	shared bool   // NM "shared" (host serves DHCP) instead of link-local
	dryRun bool   // print the plan, change nothing (also via --check)
	undo   bool   // remove the NM profile + udev rule installed by this command
}

func newDeviceUSBSetupCmd() *cobra.Command {
	var opts usbSetupOptions
	cmd := &cobra.Command{
		Use:   "usb-setup",
		Short: "Configure this Linux host's USB-C link to a Wendy device",
		Long: "Configure this Linux host so a USB-C-tethered Wendy device is reachable.\n\n" +
			"It detects the gadget network interface, brings it up via NetworkManager\n" +
			"(link-local by default, or --shared to serve DHCP to the device), and installs\n" +
			"a udev rule so ModemManager stops grabbing the gadget's serial console.\n\n" +
			"Modifies the system (NetworkManager + udev) and must run as root. Use --check\n" +
			"to preview the changes first, and --undo to remove them.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return usbSetupRun(cmd, opts)
		},
	}
	cmd.Flags().StringVar(&opts.iface, "iface", "", "USB interface to configure (default: auto-detect)")
	cmd.Flags().BoolVar(&opts.shared, "shared", false, "Serve DHCP to the device (NM shared) instead of link-local")
	cmd.Flags().BoolVar(&opts.dryRun, "dry-run", false, "Print the planned changes without applying them")
	cmd.Flags().BoolVar(&opts.dryRun, "check", false, "Alias for --dry-run")
	cmd.Flags().BoolVar(&opts.undo, "undo", false, "Remove the NM profile and udev rule installed by this command")
	return cmd
}
