package mcp

// serverInstructions is returned in the MCP initialize result. Clients that
// defer tool schemas (Claude Code) or treat instructions as standing guidance
// (Codex) load only this text and the tool names at session start, so it must
// stand on its own. Keep the first paragraph complete within 512 bytes so a
// client that truncates still learns where to start and how to deploy, keep
// the whole text ASCII and at most 2048 bytes, and name only registered tools;
// instructions_test.go enforces all of this.
const serverInstructions = "Wendy MCP manages WendyOS edge devices (Raspberry Pi, Jetson, x86 boards) and deploys apps to them. " +
	"Call `wendy_status` first: it reports the connection and a suggested next step. " +
	"Most tools need the one active device connection: find devices with `device_list` (scan=true adds LAN), " +
	"then connect with `device_connect` or `cloud_connect`. " +
	"`run` builds a project and deploys it to the connected device; then verify with `container_list` and `telemetry_logs`, " +
	"because a started container is not a working app.\n\n" +
	"Targets: `device_connect` takes an address from `device_list` (host:port), vm:NAME for a local simulator, " +
	"or a cloud selector such as cloud://HOST:PORT/org/ID/asset/ID. `cloud_connect` takes a cloud device name. " +
	"`run` reuses the connected target. If you pass device to deploy elsewhere, connect to that device before verifying. " +
	"With no connection and no device, `run` fails with NOT_CONNECTED instead of guessing.\n\n" +
	"Deploy, then verify: pass project_path as the absolute project directory. " +
	"`run` returns status, target and a build-log tail; readiness is not_checked. " +
	"Check `container_list` (running_state, termination_reason), read `telemetry_logs` for startup errors, " +
	"and test the app itself, e.g. its HTTP port. A first build can take minutes: raise timeout_seconds (default 300). " +
	"AUTH_REQUIRED means the user must run `wendy auth login` in a terminal.\n\n" +
	"MCP or CLI: prefer these tools for device state, containers, logs, WiFi and deploys; " +
	"results are structured and reuse this session's connection. " +
	"Use the wendy CLI in a shell only for work without a tool, such as `wendy auth login`, OS install or flashing, " +
	"`wendy device update` and `wendy run --watch`. " +
	"In a shell, keep each flag and its value as separate words (wendy run --device \"$DEVICE\"). " +
	"Never pack a flag and its value into one variable such as DEV=\"--device robot\": zsh passes it as a single argument.\n\n" +
	"More: read the wendy://guide resource for workflows, entitlements, error codes and links to wendy://docs/."
