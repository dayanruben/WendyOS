package commands

import (
	"os"
	"strings"

	"github.com/wendylabsinc/wendy/go/internal/shared/config"
)

// deviceEnvVar selects a target device for every command run in one shell — or
// one AI session, via the MCP server's env block — without touching the saved
// default that every session shares. Precedence: --device > WENDY_DEVICE >
// saved default.
const deviceEnvVar = "WENDY_DEVICE"

// noDeviceMessage is the error for a command that needs a device and has none.
//
// It must keep the exact prefix "no device specified; use --device flag or set
// a default" verbatim: two enabled Swift E2E tests match on that substring —
// swift/WendyE2ETests/Tests/WendyE2ETests/WendyDeviceVersionTests.swift:59 and
// swift/WendyE2ETests/Tests/WendyE2ETests/WendyDeviceInfoTests.swift:266-268 —
// reached through the JSON-mode branches at helpers.go's resolveDeviceAddress,
// connectToAgentInner and resolveTargetInner. Do not shorten or reword that
// prefix; extend the message after it instead.
const noDeviceMessage = "no device specified; use --device flag or set a default with 'wendy device set-default <device>', or set WENDY_DEVICE for this shell"

// deviceFlagFromEnv is the value applyDeviceEnv copied from WENDY_DEVICE into
// deviceFlag, or "" when --device was given or the variable is unset.
var deviceFlagFromEnv string

// envDevice returns WENDY_DEVICE with surrounding whitespace removed; a blank
// value counts as unset.
func envDevice() string {
	return strings.TrimSpace(os.Getenv(deviceEnvVar))
}

// applyDeviceEnv makes WENDY_DEVICE behave exactly like --device when the flag
// was not given. The root pre-run calls it once, before any command reads
// deviceFlag, so every resolution path — direct, cloud, run's cloud fallback,
// foxglove, build host — honours it without a lookup of its own.
func applyDeviceEnv() {
	deviceFlagFromEnv = ""
	if deviceFlag != "" {
		return
	}
	if v := envDevice(); v != "" {
		deviceFlag = v
		deviceFlagFromEnv = v
	}
}

// deviceChosenByEnv reports whether the current target came from WENDY_DEVICE.
// Code that later rewrites deviceFlag (ros2 exec's trailing --device, HIL's
// vm: selection, run's fleet split) ends that, because the target is then no
// longer the variable's.
func deviceChosenByEnv() bool {
	return deviceFlagFromEnv != "" && deviceFlag == deviceFlagFromEnv
}

// noteEnvDevice tells the user a command acted on the device named by
// WENDY_DEVICE, once a connection exists. explicit is a device the caller
// named in code (SelectDevice), which outranks the variable.
func noteEnvDevice(explicit string) {
	if explicit != "" || !deviceChosenByEnv() || os.Getenv("WENDY_AGENT_SOCKET") != "" {
		return
	}
	noteImplicitDevice(deviceFlag, implicitEnvDevice)
}

// resolveOptionsDevice is the device a resolveTarget caller named through
// SelectDevice, or "".
func resolveOptionsDevice(opts []resolveOption) string {
	cfg := resolveConfig{excludeProviderKeys: make(map[string]bool)}
	for _, o := range opts {
		o(&cfg)
	}
	return cfg.device
}

// withoutDeviceOverride runs fn with --device and WENDY_DEVICE suspended, so a
// connection made inside it resolves to the saved default. set-default uses it
// to confirm and pin the device it just saved.
func withoutDeviceOverride(fn func()) {
	prevFlag, prevEnv := deviceFlag, deviceFlagFromEnv
	deviceFlag, deviceFlagFromEnv = "", ""
	defer func() { deviceFlag, deviceFlagFromEnv = prevFlag, prevEnv }()
	fn()
}

// mcpStartupDevice picks the device `wendy mcp serve` connects to on startup,
// with the same precedence as every other command: its own -d/--device, then
// WENDY_DEVICE (set per AI session in the MCP client's env block), then the
// saved default.
func mcpStartupDevice(flag string, cfg *config.Config) string {
	if flag != "" {
		return flag
	}
	if env := envDevice(); env != "" {
		return env
	}
	if cfg != nil {
		return cfg.DefaultDevice
	}
	return ""
}
