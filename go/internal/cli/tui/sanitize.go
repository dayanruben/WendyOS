package tui

import "strings"

// StripControl removes terminal control characters (C0 controls and DEL)
// from externally-sourced strings before they reach the terminal. WiFi SSIDs
// arrive from beacon frames and can contain arbitrary bytes — including ANSI
// escape sequences — so every scanner-derived SSID or security label must
// pass through here before being rendered.
func StripControl(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s)
}
