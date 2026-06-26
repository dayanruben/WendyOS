//go:build linux

package commands

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"

	"github.com/wendylabsinc/wendy/go/internal/shared/discovery"
	"github.com/wendylabsinc/wendy/go/internal/shared/nmcli"
)

const (
	// usbSetupNMConnName is the NetworkManager profile name this command manages.
	usbSetupNMConnName = "wendy-usb"
	// usbSetupUdevPath is the host udev rule that keeps ModemManager off the gadget.
	usbSetupUdevPath = "/etc/udev/rules.d/99-wendy-usb.rules"
	// usbGadgetVID/PID identify the WendyOS USB-CDC composite gadget.
	usbGadgetVID = "1d6b"
	usbGadgetPID = "0104"
)

// usbSetupUdevRule tags the Wendy USB gadget so ModemManager leaves its CDC-ACM
// serial console (/dev/ttyACM*) and CDC-NCM network port alone.
const usbSetupUdevRule = `# Installed by 'wendy device usb-setup'.
# Stop ModemManager from probing the Wendy USB gadget's serial console
# (/dev/ttyACM*) and network port (VID:PID 1d6b:0104).
ACTION=="add|change", SUBSYSTEM=="tty", SUBSYSTEMS=="usb", ATTRS{idVendor}=="1d6b", ATTRS{idProduct}=="0104", ENV{ID_MM_DEVICE_IGNORE}="1"
ACTION=="add|change", SUBSYSTEM=="net", SUBSYSTEMS=="usb", ATTRS{idVendor}=="1d6b", ATTRS{idProduct}=="0104", ENV{ID_MM_DEVICE_IGNORE}="1"
`

// usbSetupSudoHint reproduces the relevant flags so a re-run-as-root suggestion
// preserves the user's intent.
func usbSetupSudoHint(opts usbSetupOptions) string {
	var b strings.Builder
	if opts.undo {
		b.WriteString(" --undo")
	}
	if opts.shared {
		b.WriteString(" --shared")
	}
	if opts.iface != "" {
		fmt.Fprintf(&b, " --iface %s", opts.iface)
	}
	return b.String()
}

func usbSetupRun(cmd *cobra.Command, opts usbSetupOptions) error {
	w := cmd.OutOrStdout()
	ctx := cmd.Context()

	if opts.undo {
		return usbSetupUndo(ctx, w, opts)
	}

	iface, err := resolveUSBSetupInterface(opts.iface)
	if err != nil {
		return err
	}

	mode := "link-local"
	modeNote := "device should use its on-device DHCP/link-local address"
	if opts.shared {
		mode = "shared"
		modeNote = "host serves DHCP 10.42.0.1/24 to the device"
	}

	fmt.Fprintln(w, "Wendy USB-C host setup")
	fmt.Fprintf(w, "  interface: %s\n", iface)
	fmt.Fprintf(w, "  ipv4 mode: %s (%s)\n", mode, modeNote)
	fmt.Fprintf(w, "  udev rule: %s (ModemManager-ignore for %s:%s)\n", usbSetupUdevPath, usbGadgetVID, usbGadgetPID)

	addArgs := []string{"connection", "add", "type", "ethernet", "ifname", iface,
		"con-name", usbSetupNMConnName, "connection.autoconnect", "yes"}
	if opts.shared {
		addArgs = append(addArgs, "ipv4.method", "shared")
	} else {
		addArgs = append(addArgs, "ipv4.method", "link-local", "ipv6.method", "link-local")
	}

	if opts.dryRun {
		fmt.Fprintf(w, "\n[dry-run] would replace any existing '%s' profile, then run:\n", usbSetupNMConnName)
		fmt.Fprintf(w, "  nmcli connection delete %s   # if present\n", usbSetupNMConnName)
		fmt.Fprintf(w, "  nmcli %s\n", strings.Join(addArgs, " "))
		fmt.Fprintf(w, "  nmcli connection up %s\n", usbSetupNMConnName)
		fmt.Fprintf(w, "[dry-run] would write %s and run 'udevadm control --reload-rules && udevadm trigger'\n", usbSetupUdevPath)
		return nil
	}

	if os.Geteuid() != 0 {
		return fmt.Errorf("this command modifies NetworkManager and udev and must run as root.\n"+
			"  Re-run with: sudo wendy device usb-setup%s\n"+
			"  Or preview the changes first with: wendy device usb-setup --check%s",
			usbSetupSudoHint(opts), usbSetupSudoHint(opts))
	}

	nmcliPath, err := exec.LookPath("nmcli")
	if err != nil {
		return fmt.Errorf("nmcli not found; install NetworkManager or configure %s manually: %w", iface, err)
	}

	// Replace any stale profile with our name first (Scenario 5 in the
	// networking troubleshooting docs). Ignore errors — it may not exist.
	_ = nmcli.Command(ctx, nmcliPath, "connection", "delete", usbSetupNMConnName).Run()

	if b, err := nmcli.Command(ctx, nmcliPath, addArgs...).CombinedOutput(); err != nil {
		return fmt.Errorf("nmcli connection add failed: %v: %s", err, strings.TrimSpace(string(b)))
	}
	if b, err := nmcli.Command(ctx, nmcliPath, "connection", "up", usbSetupNMConnName).CombinedOutput(); err != nil {
		return fmt.Errorf("nmcli connection up failed: %v: %s", err, strings.TrimSpace(string(b)))
	}
	fmt.Fprintf(w, "✓ configured %s (%s)\n", iface, mode)

	if err := os.WriteFile(usbSetupUdevPath, []byte(usbSetupUdevRule), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", usbSetupUdevPath, err)
	}
	if err := reloadUdev(ctx); err != nil {
		return fmt.Errorf("reloading udev: %w", err)
	}
	fmt.Fprintf(w, "✓ installed ModemManager-ignore rule: %s\n", usbSetupUdevPath)
	fmt.Fprintln(w, "\nDone. Re-plug the device if it was already connected, then run 'wendy discover'.")
	return nil
}

// resolveUSBSetupInterface returns the gadget interface to configure, using the
// override when given or auto-detecting otherwise.
func resolveUSBSetupInterface(override string) (string, error) {
	if override != "" {
		return override, nil
	}
	found, err := discovery.USBNetworkInterfaceNames()
	if err != nil {
		return "", fmt.Errorf("detecting USB interfaces: %w", err)
	}
	switch len(found) {
	case 0:
		return "", fmt.Errorf("no USB gadget interface found.\n" +
			"  Connect the device with a data (not charge-only) USB-C cable, wait a few seconds, then retry.\n" +
			"  If it is connected, pass it explicitly: wendy device usb-setup --iface <name> (see 'ip link').")
	case 1:
		return found[0], nil
	default:
		return "", fmt.Errorf("multiple USB interfaces found (%s); choose one with --iface <name>", strings.Join(found, ", "))
	}
}

func usbSetupUndo(ctx context.Context, w io.Writer, opts usbSetupOptions) error {
	if opts.dryRun {
		fmt.Fprintf(w, "[dry-run] would delete NM profile '%s', remove %s, and reload udev\n", usbSetupNMConnName, usbSetupUdevPath)
		return nil
	}
	if os.Geteuid() != 0 {
		return fmt.Errorf("--undo modifies NetworkManager and udev and must run as root.\n"+
			"  Re-run with: sudo wendy device usb-setup%s", usbSetupSudoHint(opts))
	}
	if nmcliPath, err := exec.LookPath("nmcli"); err == nil {
		_ = nmcli.Command(ctx, nmcliPath, "connection", "delete", usbSetupNMConnName).Run()
	}
	if err := os.Remove(usbSetupUdevPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing %s: %w", usbSetupUdevPath, err)
	}
	_ = reloadUdev(ctx)
	fmt.Fprintf(w, "✓ removed '%s' NM profile and %s\n", usbSetupNMConnName, usbSetupUdevPath)
	return nil
}

// reloadUdev reloads udev rules and re-triggers matching so the ModemManager
// ignore tag applies to an already-connected device.
func reloadUdev(ctx context.Context) error {
	udevadm, err := exec.LookPath("udevadm")
	if err != nil {
		return fmt.Errorf("udevadm not found: %w", err)
	}
	if b, err := exec.CommandContext(ctx, udevadm, "control", "--reload-rules").CombinedOutput(); err != nil {
		return fmt.Errorf("udevadm control: %v: %s", err, strings.TrimSpace(string(b)))
	}
	if b, err := exec.CommandContext(ctx, udevadm, "trigger").CombinedOutput(); err != nil {
		return fmt.Errorf("udevadm trigger: %v: %s", err, strings.TrimSpace(string(b)))
	}
	return nil
}
