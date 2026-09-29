//go:build linux

package commands

import (
	"context"
	"net"
	"strings"
	"testing"
)

func TestIPv4Configured(t *testing.T) {
	cases := []struct {
		name  string
		addrs []net.Addr
		want  bool
	}{
		{"empty", nil, false},
		{"only ipv6 link-local", []net.Addr{&net.IPNet{IP: net.ParseIP("fe80::1")}}, false},
		{"ipv4 link-local", []net.Addr{&net.IPNet{IP: net.ParseIP("169.254.3.4")}}, true},
		{"ipv4 routable", []net.Addr{&net.IPNet{IP: net.ParseIP("10.42.0.5")}}, true},
		{"ipaddr form", []net.Addr{&net.IPAddr{IP: net.ParseIP("10.42.0.5")}}, true},
	}
	for _, c := range cases {
		if got := ipv4Configured(c.addrs); got != c.want {
			t.Errorf("%s: ipv4Configured = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestDetectUnconfiguredUSBGadget(t *testing.T) {
	origNames := usbGadgetIfaceNames
	origAddrs := usbIfaceAddrs
	origProfile := usbSetupProfileExists
	t.Cleanup(func() {
		usbGadgetIfaceNames = origNames
		usbIfaceAddrs = origAddrs
		usbSetupProfileExists = origProfile
	})
	usbSetupProfileExists = func() bool { return false }

	ipv6Only := []net.Addr{&net.IPNet{IP: net.ParseIP("fe80::1")}}
	withIPv4 := []net.Addr{&net.IPNet{IP: net.ParseIP("10.42.0.5")}}

	cases := []struct {
		name  string
		names []string
		addrs []net.Addr
		want  string
	}{
		{"no gadget", nil, nil, ""},
		{"ambiguous (two gadgets)", []string{"usb0", "usb1"}, ipv6Only, ""},
		{"unconfigured", []string{"enxaa"}, ipv6Only, "enxaa"},
		{"already configured", []string{"enxaa"}, withIPv4, ""},
	}
	for _, c := range cases {
		names, addrs := c.names, c.addrs
		usbGadgetIfaceNames = func() ([]string, error) { return names, nil }
		usbIfaceAddrs = func(string) ([]net.Addr, error) { return addrs, nil }
		if got := detectUnconfiguredUSBGadget(); got != c.want {
			t.Errorf("%s: detectUnconfiguredUSBGadget = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestResolveUSBSetupInterface_Override(t *testing.T) {
	got, err := resolveUSBSetupInterface("usb7")
	if err != nil || got != "usb7" {
		t.Fatalf("resolveUSBSetupInterface(override) = %q, %v; want usb7, nil", got, err)
	}
}

// Without a terminal (an agent, a script, --json) nobody can answer the setup
// prompt, so discovery must say why the tethered device is missing instead of
// staying silent, on stderr so JSON on stdout stays intact.
func TestMaybeOfferUSBSetup_WithoutPromptPrintsNotice(t *testing.T) {
	origNames, origAddrs, origProfile := usbGadgetIfaceNames, usbIfaceAddrs, usbSetupProfileExists
	origJSON, origInteractive := jsonOutput, isInteractiveTerminalFn
	t.Cleanup(func() {
		usbGadgetIfaceNames, usbIfaceAddrs, usbSetupProfileExists = origNames, origAddrs, origProfile
		jsonOutput, isInteractiveTerminalFn = origJSON, origInteractive
	})
	usbIfaceAddrs = func(string) ([]net.Addr, error) {
		return []net.Addr{&net.IPNet{IP: net.ParseIP("fe80::1")}}, nil
	}
	usbSetupProfileExists = func() bool { return false }

	cases := []struct {
		name        string
		json        bool
		interactive bool
	}{
		{"--json in a terminal", true, true},
		{"no terminal", false, false},
	}
	for _, c := range cases {
		jsonOutput = c.json
		interactive := c.interactive
		isInteractiveTerminalFn = func() bool { return interactive }

		usbGadgetIfaceNames = func() ([]string, error) { return []string{"enxaa"}, nil }
		got := captureStderr(t, func() { _ = maybeOfferUSBSetup(context.Background()) })
		if !strings.Contains(got, "enxaa") || !strings.Contains(got, "wendy discover") {
			t.Errorf("%s: stderr = %q, want the USB-C setup notice for enxaa", c.name, got)
		}

		usbGadgetIfaceNames = func() ([]string, error) { return nil, nil }
		if got := captureStderr(t, func() { _ = maybeOfferUSBSetup(context.Background()) }); got != "" {
			t.Errorf("%s, no gadget: stderr = %q, want nothing", c.name, got)
		}
	}
}
