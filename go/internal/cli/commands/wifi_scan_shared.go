package commands

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
)

// errNoWifiAdapter signals that the host has no usable WiFi hardware, as
// opposed to a transient scan failure. Platform scanners return it (possibly
// wrapped) so the install flow can show a specific message (WDY-1474).
var errNoWifiAdapter = errors.New("no WiFi adapter detected on this machine")

// exitErrWithStderr enriches an error from exec's Output() with the captured
// stderr, which otherwise surfaces only as "exit status N". Non-ExitError
// values and empty stderr pass through unchanged.
func exitErrWithStderr(err error) error {
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		if stderr := bytes.TrimSpace(ee.Stderr); len(stderr) > 0 {
			return fmt.Errorf("%w: %s", err, stderr)
		}
	}
	return err
}

// wifiScanFailureNotice renders the user-facing message shown when a host
// WiFi scan produced no usable networks. scanErr == nil means the scan
// succeeded but found nothing.
func wifiScanFailureNotice(scanErr error) string {
	switch {
	case scanErr == nil:
		msg := "No WiFi networks found."
		if wifiScanCacheHint != "" {
			msg += " " + wifiScanCacheHint + "."
		}
		return msg
	case errors.Is(scanErr, errNoWifiAdapter):
		return "No WiFi adapter detected on this machine."
	default:
		return fmt.Sprintf("WiFi scan failed: %v", scanErr)
	}
}
