---
name: wendy-onboarding
description: Get started with WendyOS by installing and verifying a first physical device, or creating a local simulator when hardware has not arrived or is unavailable. Use for first-time Wendy setup and plugin onboarding.
---

# Get started with WendyOS

Help the user reach one verified Wendy target they can develop against. They can
start with physical hardware or a simulator, and keep the same app project when
their hardware arrives.

## Choose a starting point

Use what the conversation and available tools already establish. If the user has
not chosen, ask whether they have a device ready to install or want to start with
a simulator while waiting for hardware. Offer the simulator path when they have
no hardware. Do not turn an empty discovery result into a flashing decision.

Inspect the running server's tools before calling them:

- With the CLI MCP server, call `wendy_status`. Enable `setup` for installation
  or `simulator` for local VMs through `wendy_tools`.
- With the ChatGPT gateway, use `list_robots` to find existing authorized devices.
  Call `open_devices` when available to open the device and simulator workspace.
  It prefers fullscreen, but the host chooses the actual display mode. Continue
  in the conversation when no UI is available.
- With a local terminal, check `wendy --version`. Follow
  [wendy-install](../wendy-install/SKILL.md) only if CLI installation is needed.
  Use [wendy-mcp-setup](../wendy-mcp-setup/SKILL.md) when the user needs a local MCP
  connection. A running remote gateway does not imply access to their laptop.

Plugin installation alone does not authorize an OS installer or a disk erase.
The user's setup request authorizes ordinary setup work; get specific erase
authorization only after identifying the actual target and scope.

## First physical device

Identify the board and carrier, current OS, host OS, intended storage, and network.
Inspect available device information first and ask only for missing details. If
WendyOS is already installed, verify that device instead of reinstalling it.

Read [wendy-device-install](../wendy-device-install/SKILL.md) for board-specific
requirements. Jetson developer-kit images need a confirmed developer-kit carrier.
Unitree G1 PC2 keeps vendor Ubuntu and receives the Agent.

For a supported image install through MCP:

1. Call `os_install_plan`. For raw media, call `os_list_drives` and match the
   intended SD card or SSD by path, model, and capacity. Re-plan with that drive.
2. Call `os_install_start` and retain its `job_id`. Show its physical instructions
   for cables, recovery mode, media, and power before continuing.
3. After the user completes those steps, call `os_install_resume` to probe the
   target. Show the returned fingerprint and erase scope. Only after explicit
   authorization for that target, resume with that exact `target_id` and
   `confirm_erase=true`. Set `confirm_internal=true` only when separately approved.
4. Poll `os_install_status`. If elevation or an unsupported installation method
   requires a terminal, give the returned command and explain which host runs it.
   Never collect an administrator password in chat. Inspect an interrupted job
   before retrying; a timeout does not mean the write failed.
5. Follow the first-boot instructions. Discover the intended device and resume
   the job with its explicit address, or use `os_install_verify` with the planned
   device type and OS version. Record a verified identity before connecting.

With the CLI server, connect using `device_connect`. With the gateway, refresh
`list_robots` and use the authorized `robot_id` with `inspect_robot`. Do not invent
an ID or widen a grant when the installed device is not yet listed. Explain the
remaining connection or enrollment step.

Gateway installation tools require an enabled local stdio session with host
permissions. HTTP gateways cannot flash the user's laptop media. If the tools
are absent, follow the installation skill's terminal path on the appropriate
host. On Linux, a USB-C discovery warning means the user must run `wendy discover`
in a terminal and accept the host USB setup prompt before tethered access works.

## Start without hardware

List existing simulators with `simulator_list` before creating one. Reuse a VM
when it matches the user's purpose; preserve other VMs and their disks. Use
`generic` for ordinary WendyOS app development. Choose `go2` or `g1` only when
the user wants that robot simulation.

Create a named VM with `simulator_create`, using a name from the user's context
or an unused simple name such as `wendy-dev`. Omit `version` for the published
stable default unless the project requires a specific image. Explain that the
first download and boot can take several minutes.

Creation returns a stopped VM. Start it through the tools the server exposes:

- CLI MCP: `device_connect(device="vm:<name>")` boots and connects to it.
- Local ChatGPT gateway: `simulator_start(name="<name>")` boots it. Use task
  augmentation when supported and poll that task rather than starting it again.
- Terminal: inspect `wendy vm --help`, create with
  `wendy vm create <name> --profile generic`, then connect with a device inspection
  such as `wendy --json device info --device vm:<name>`.

Refresh the simulator state and inspect the agent to verify the connection. For
a robot profile, call `simulator_viewer` when available; only describe its viewer
as ready when the result reports both `ready` and `healthy`. If the VM's agent
lacks robot support, `simulator_update_agent` can finish setup when available;
then start and verify again. Do not replace or delete the VM to recover an
uncertain operation.

Gateway simulator operations require a local stdio connection and permission
through `allow_simulators` with `simulators:manage`, or approved host operations.
When unavailable, explain that local connection requirement or use the installed
CLI on the user's development host. Do not claim a simulator was created on the
user's laptop from a remote-only connection.

## Verify and continue

Report the chosen target, physical or simulated, its explicit selector or
authorized ID, and what verification succeeded. Separate a running VM, an agent
connection, and app readiness. A simulator cannot verify physical cameras, GPU
availability, or robot motion on hardware.

If the user also wants to build an app, continue with
[wendy-template-app](../wendy-template-app/SKILL.md) and
[wendy-app-lifecycle](../wendy-app-lifecycle/SKILL.md). Deploy to the verified
target explicitly. For VM HTTP apps, use the returned forwarded URL and test the
actual response; `vm:<name>` is a device selector, not a browser address.

When physical hardware arrives, install and verify it using the physical path,
check its capabilities and entitlements, then deploy the same project with an
explicit new target. Keep the simulator unless the user asks to remove it.
