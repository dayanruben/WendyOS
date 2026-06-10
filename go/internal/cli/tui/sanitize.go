package tui

import (
	"strings"
	"unicode"
)

// StripControl removes control characters from externally-sourced strings
// before they reach the terminal. This covers C0 controls (incl. ESC), DEL,
// and the C1 range (U+0080–U+009F) — U+009B is a single-rune CSI introducer
// on VTE-style terminals, so stripping only C0 would still allow escape
// injection. WiFi SSIDs arrive from beacon frames and can contain arbitrary
// bytes, so every scanner-derived SSID or security label must pass through
// here before being rendered.
func StripControl(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
}
