# `wendy os update`

Updates the OS on a WendyOS device using the Mender OTA mechanism.

```sh
# Auto-detect the latest stable release for the connected device
wendy os update

# Use the latest nightly build
wendy os update --nightly

# Provide a specific Mender artifact URL
wendy os update --artifact-url https://example.com/update.mender

# Provide a local Mender artifact file
wendy os update ./update.mender
```

---

## Supported targets

`wendy os update` only works with WendyOS devices that have OTA support. The command validates the target before doing anything else, including before updating the agent binary.

A target is treated as a WendyOS OTA target when the agent reports `os = "linux"` and either:
- `os_version` begins with `WendyOS-`, or
- `device_type` is set (non-empty).

Note that WendyOS devices report `os = "linux"` (they do not have a standard `/etc/os-release` file). Other Linux hosts report their distro ID (e.g., "ubuntu", "debian", "arch") when detectable.

Hosts that are not WendyOS OTA targets — including macOS, Windows, unknown platforms, Wendy Lite / BLE-only targets, external/local-provider targets, and generic Linux hosts with `wendy-agent` installed but no WendyOS identity — are rejected immediately with an actionable error message.

| Circumstance | Error |
|---|---|
| macOS, Windows, unknown non-WendyOS platform, Wendy Lite, external/local provider | `This setup cannot be updated with wendy os update. Use this machine's normal OS update tools instead. To use WendyOS OTA updates, install WendyOS on supported hardware with wendy os install.` |
| Generic Linux host with `wendy-agent` but no WendyOS identity (including hosts that also have `mender-update` installed) | `This Linux host has wendy-agent installed, but it cannot be updated with WendyOS OTA artifacts. Use the Linux distribution's package manager, such as apt, dnf, or pacman, to update this machine.` |
| WendyOS identity present but `mender-update` not found | `This WendyOS image does not support OTA updates because mender-update was not found. Reinstall or upgrade to a WendyOS image with OTA support.` |
| No explicit artifact and the device type is missing or unrecognized in the update catalog | Shows a warning and prompts the user to select the correct device type from a picker. The latest version (stable or nightly) is then chosen automatically. |

> **Note:** macOS agents report a host OS version, but this does not qualify the host as a WendyOS OTA target. Only a `WendyOS-`-prefixed `os_version` or a non-empty `device_type` on a Linux host qualifies.

---

## Update sequence

1. **Validate target identity** — query `GetAgentVersion` and confirm the target is a WendyOS OTA target. Exits immediately with an error if not.
2. **Update the agent** — ensure the agent binary is at the latest release before proceeding with the OS image update. GitHub release lookups use the `GITHUB_TOKEN` environment variable when present, and fall back to unauthenticated requests otherwise.
3. **Re-query version** — query `GetAgentVersion` again after the agent update.
4. **Validate OTA support** — confirm `mender` is present in the featureset.
5. **Resolve artifact** — if no artifact or URL was provided, look up the latest OTA artifact for the device's reported `device_type`. If the device type is missing or not recognized, shows a warning and prompts the user to select the correct device type.
6. **Check current version** — if the device is already at the latest version, exits without updating.
7. **Stream update** — call `UpdateOS` on the agent, which runs `mender-update install` and streams progress to the terminal. The agent then reboots into the updated OS.
8. **Wait for reboot** — poll the device until it is reachable again (up to 10 minutes, enough for a rollback's second reboot).
9. **Report the outcome** — query the device for the post-update healthcheck verdict and print it. The command exits non-zero when the update was rolled back.

---

## Post-update healthchecks and automatic rollback

WendyOS uses Mender A/B rootfs slots, so an update boots into the new slot while keeping the previous OS intact. On the first boot after an update, the agent healthchecks critical system services **before** committing the update:

| Service | Why it matters |
|---------|----------------|
| `avahi-daemon.service` | mDNS/device discovery — an unreachable device cannot be managed |
| `containerd.service` | Container runtime — apps cannot run without it |
| `NetworkManager.service` | WiFi/network connectivity |

Each service gets a bounded time to become active (services that do not exist on a device, or are intentionally disabled, are skipped). If every check passes, the agent runs `mender-update commit` to make the update permanent. If any check fails, the agent rolls back (`mender-update rollback`) and reboots into the previous OS.

The verdict — including which services failed and why — is persisted on the device's data partition, so it survives the rollback. `wendy os update` reports it once the device is back online:

```
Update failed post-reboot healthchecks and was rolled back to WendyOS-0.10.4.
Failed services:
  - avahi-daemon.service: timed out after 30s waiting for active; last state: ActiveState=failed SubState=exited Result=exit-code
```

> **Note:** the healthchecks run inside the agent bundled with the *new* OS image, so they only protect updates to images that ship an agent with healthcheck support. For older target images the CLI falls back to comparing the OS version before and after the reboot, and warns when the device appears to have rolled back.

---

## Artifact auto-selection

When no artifact path or `--artifact-url` is given, the CLI uses the device's `device_type` field to look up the latest OTA artifact from the WendyOS release manifest. If the device type is missing or not recognized in the update catalog, the CLI shows a warning and falls back to an interactive device-type picker. The latest version (stable or nightly) is then chosen automatically.

Use `--nightly` to select nightly (pre-release) artifacts instead of stable ones.

---

## Flags reference

| Flag | Default | Description |
|------|---------|-------------|
| `--artifact-url` | — | URL of a Mender artifact to install directly |
| `--nightly` | false | Use nightly/pre-release builds for auto-selection |

A positional argument (local file path) can be used instead of `--artifact-url`.
