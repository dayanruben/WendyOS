package commands

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"

	"github.com/spf13/cobra"
)

// usbSetupMode is how `wendy device usb-setup` obtains the root privileges the
// NetworkManager + udev changes need.
type usbSetupMode int

const (
	usbSetupUnsupported usbSetupMode = iota // not Linux: nothing to configure
	usbSetupDirect                          // already root: run in-process
	usbSetupSudo                            // re-exec the hidden __usb-setup helper under sudo
)

// usbSetupModeFor decides how usb-setup runs for the given platform and
// effective uid.
func usbSetupModeFor(goos string, euid int) usbSetupMode {
	switch {
	case goos != "linux":
		return usbSetupUnsupported
	case euid == 0:
		return usbSetupDirect
	default:
		return usbSetupSudo
	}
}

// usbSetupSudoArgs builds the arguments after "sudo" that re-run the
// privileged helper. Without a terminal to prompt on, -n makes sudo fail fast
// instead of waiting for a password nobody can type.
func usbSetupSudoArgs(self, iface string, interactive bool) []string {
	var args []string
	if !interactive {
		args = append(args, "-n")
	}
	args = append(args, self, "__usb-setup")
	if iface != "" {
		args = append(args, "--iface", iface)
	}
	return args
}

// newDeviceUSBSetupCmd builds `wendy device usb-setup`, the documented way to
// configure a Linux host's USB-C link to a Wendy device. It runs the same code
// as the automatic offer in `wendy discover` (runUSBSetup): a NetworkManager
// "shared" profile for the gadget interface plus a udev rule that keeps
// ModemManager off the device's serial console.
func newDeviceUSBSetupCmd() *cobra.Command {
	var iface string
	cmd := &cobra.Command{
		Use:   "usb-setup",
		Short: "Configure this Linux host's USB-C link to a Wendy device (uses sudo)",
		Long: "Configure this Linux host so a USB-C-tethered Wendy device is reachable:\n" +
			"a NetworkManager \"shared\" connection on the device's USB network interface\n" +
			"(the host serves DHCP on 10.42.0.1/24) and a udev rule that stops ModemManager\n" +
			"from grabbing the device's serial console.\n\n" +
			"Connect the device first. Runs under sudo and prompts for your password; it can\n" +
			"also be run as `sudo wendy device usb-setup`. Only needed on Linux.",
		// It may run as root: never load or write the CLI config, analytics or
		// first-run state as root (same as the hidden __usb-setup helper).
		PersistentPreRunE:  func(*cobra.Command, []string) error { return nil },
		PersistentPostRunE: func(*cobra.Command, []string) error { return nil },
		RunE: func(cmd *cobra.Command, _ []string) error {
			switch usbSetupModeFor(runtime.GOOS, os.Geteuid()) {
			case usbSetupUnsupported:
				return fmt.Errorf("`wendy device usb-setup` is only needed on Linux; on %s the USB-C link needs no host setup — run `wendy discover`", runtime.GOOS)
			case usbSetupDirect:
				return runUSBSetup(cmd.Context(), iface, cmd.OutOrStdout())
			}
			self, err := os.Executable()
			if err != nil {
				return fmt.Errorf("resolving the wendy executable for sudo: %w", err)
			}
			interactive := isInteractiveTerminal()
			if interactive {
				fmt.Fprintln(cmd.ErrOrStderr(), "You may be prompted for your password (sudo is required).")
			}
			sudo := exec.CommandContext(cmd.Context(), "sudo", usbSetupSudoArgs(self, iface, interactive)...)
			sudo.Stdin, sudo.Stdout, sudo.Stderr = os.Stdin, cmd.OutOrStdout(), cmd.ErrOrStderr()
			if err := sudo.Run(); err != nil {
				if !interactive {
					// sudo -n fails the same way whether it needed a password or
					// the helper itself failed; both messages are printed above.
					return fmt.Errorf("USB-C setup did not complete (%w); it needs root, and without a terminal sudo cannot ask for a password: run `wendy device usb-setup` in a terminal, or `sudo wendy device usb-setup`", err)
				}
				return fmt.Errorf("USB-C setup did not complete: %w", err)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&iface, "iface", "", "USB network interface to configure (auto-detected when exactly one is present)")
	return cmd
}
