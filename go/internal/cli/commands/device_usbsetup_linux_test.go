//go:build linux

package commands

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestUSBSetupSudoHint(t *testing.T) {
	cases := []struct {
		opts usbSetupOptions
		want string
	}{
		{usbSetupOptions{}, ""},
		{usbSetupOptions{shared: true}, " --shared"},
		{usbSetupOptions{iface: "usb0"}, " --iface usb0"},
		{usbSetupOptions{undo: true, shared: true, iface: "enx1"}, " --undo --shared --iface enx1"},
	}
	for _, c := range cases {
		if got := usbSetupSudoHint(c.opts); got != c.want {
			t.Errorf("usbSetupSudoHint(%+v) = %q, want %q", c.opts, got, c.want)
		}
	}
}

func TestResolveUSBSetupInterface_Override(t *testing.T) {
	got, err := resolveUSBSetupInterface("usb7")
	if err != nil || got != "usb7" {
		t.Fatalf("resolveUSBSetupInterface(override) = %q, %v; want usb7, nil", got, err)
	}
}

// Dry-run must print the plan, require no root, and change nothing.
func TestUSBSetupRun_DryRun(t *testing.T) {
	newCmd := func(buf *bytes.Buffer) *cobra.Command {
		cmd := &cobra.Command{}
		cmd.SetOut(buf)
		cmd.SetContext(context.Background())
		return cmd
	}

	var buf bytes.Buffer
	if err := usbSetupRun(newCmd(&buf), usbSetupOptions{iface: "usb0", dryRun: true}); err != nil {
		t.Fatalf("dry-run (link-local) returned error: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"[dry-run]", "usb0", "ipv4.method link-local", usbSetupUdevPath} {
		if !strings.Contains(out, want) {
			t.Errorf("link-local dry-run output missing %q\n%s", want, out)
		}
	}

	buf.Reset()
	if err := usbSetupRun(newCmd(&buf), usbSetupOptions{iface: "usb0", shared: true, dryRun: true}); err != nil {
		t.Fatalf("dry-run (shared) returned error: %v", err)
	}
	if out := buf.String(); !strings.Contains(out, "ipv4.method shared") {
		t.Errorf("shared dry-run output missing 'ipv4.method shared'\n%s", out)
	}

	buf.Reset()
	if err := usbSetupRun(newCmd(&buf), usbSetupOptions{undo: true, dryRun: true}); err != nil {
		t.Fatalf("dry-run (undo) returned error: %v", err)
	}
	if out := buf.String(); !strings.Contains(out, usbSetupNMConnName) || !strings.Contains(out, "[dry-run]") {
		t.Errorf("undo dry-run output unexpected\n%s", out)
	}
}
