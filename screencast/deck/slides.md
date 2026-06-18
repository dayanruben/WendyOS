---
theme: default
colorSchema: dark
title: Wendy Agent for Mac Beta
info: |
  Slidev source deck for the Wendy Agent for Mac Beta screencast.
class: text-left
drawings:
  persist: false
transition: fade-out
mdc: true
---

# Wendy Agent for Mac Beta

Native Swift app deployment to an Apple Silicon Mac with the familiar Wendy CLI workflow.

- Beta
- Apple Silicon
- SwiftPM
- Xcode

Konstantin<br>
Wendy Labs<br>
2026-06-17

<!--
Timeline id: intro
Voiceover: voiceover/text/00-intro.txt
-->

---

# The model is the same

The target is different: macOS instead of WendyOS.

- The CLI runs on your machine.
- Wendy Agent runs on the target Mac as a macOS app.
- Apps deploy as native SwiftPM or Xcode projects.

<!--
Timeline id: framing
Voiceover: voiceover/text/01-framing.txt
-->

---

# Install and launch

```sh
brew tap wendylabsinc/tap
brew install --cask wendy-agent
open /Applications/WendyAgentMac.app
```

There is no WendyOS image to flash. The Mac remains macOS; Wendy Agent is the service the CLI targets.

<video :src="'/videos/mac-beta/01-install-launch.mp4'" controls muted width="100%"></video>

<!--
Timeline id: install-launch
Voiceover: voiceover/text/02-install-launch.txt
VHS: tapes/01-install-launch.tape
-->

---

# One-time macOS setup

Wendy Agent is configured once on the Mac.

- Grant macOS permissions to Wendy Agent during setup.
- The menu item lists all apps managed by Wendy Agent.
- Later deploys run from the CLI without desktop interaction.
- Permissions prepare the broker; they do not imply every app-level hardware API is complete in beta.

<video :src="'/videos/mac-beta/ui-agent-menu-permissions.mp4'" controls muted width="100%"></video>

<!--
Timeline id: permissions
Voiceover: voiceover/text/03-permissions.txt
Screen recording: deck/public/videos/mac-beta/ui-agent-menu-permissions.mp4
-->

---

# Verify the Mac target

```sh
wendy --device mac-mini.local:50051 device info --json
```

The important fields are:

```json
{
  "os": "darwin",
  "cpuArchitecture": "arm64"
}
```

<video :src="'/videos/mac-beta/02-device-info.mp4'" controls muted width="100%"></video>

<!--
Timeline id: verify-target
Voiceover: voiceover/text/04-verify-target.txt
VHS: tapes/02-device-info.tape
-->

---

# Discovery and default target

```sh
wendy discover
wendy device set-default mac-mini.local:50051
```

Discovery and default device selection work like other Wendy targets. For a headless Mac mini, save the hostname or IP as the default target.

<video :src="'/videos/mac-beta/03-discovery-default.mp4'" controls muted width="100%"></video>

<!--
Timeline id: discovery-default
Voiceover: voiceover/text/05-discovery-default.txt
VHS: tapes/03-discovery-default.tape
-->

---

# Native Mac app shape

A Mac beta app is explicit about the Darwin target and uses a native project shape.

- `platform: "darwin"`
- `Package.swift` for SwiftPM projects
- `.xcodeproj` for Xcode projects
- No Dockerfile or Compose deploy path in this beta

<!--
Timeline id: native-app-section
Voiceover: voiceover/text/06-native-app-section.txt
-->

---

# Native app shape: files

<video :src="'/videos/mac-beta/04-native-app-shape.mp4'" controls muted width="100%"></video>

<!--
Timeline id: native-app-shape
Voiceover: voiceover/text/07-native-app-shape.txt
VHS: tapes/04-native-app-shape.tape
-->

---

# SwiftPM deployment

```sh
wendy run --device mac-mini.local:50051 --build-type swift
```

`wendy run` builds the Swift package, syncs it to Wendy Agent for Mac, and launches it as a native macOS process.

<video :src="'/videos/mac-beta/05-run-swiftpm.mp4'" controls muted width="100%"></video>

<!--
Timeline id: swiftpm-deploy
Voiceover: voiceover/text/08-swiftpm-deploy.txt
VHS: tapes/05-run-swiftpm.tape
-->

---

# Xcode and MLX-style apps

Xcode projects are also supported. That path matters for MLX and VLMMLX-style apps that need an app bundle and the Metal toolchain.

```sh
xcodebuild -find metal
wendy run --device mac-mini.local:50051
```

<video :src="'/videos/mac-beta/06-run-xcode.mp4'" controls muted width="100%"></video>

<!--
Timeline id: xcode-deploy
Voiceover: voiceover/text/09-xcode-deploy.txt
VHS: tapes/06-run-xcode.tape
-->

---

# App lifecycle

```sh
wendy device apps list
wendy device apps stop <app-id>
wendy device apps remove <app-id>
```

The familiar Wendy app lifecycle commands apply to the native Mac target.

<video :src="'/videos/mac-beta/07-app-lifecycle.mp4'" controls muted width="100%"></video>

<!--
Timeline id: app-lifecycle
Voiceover: voiceover/text/10-app-lifecycle.txt
VHS: tapes/07-app-lifecycle.tape
-->

---

# Unsupported paths fail clearly

Unsupported Mac beta surfaces should say so directly.

- Hardware commands that are not implemented yet
- Container project shapes such as Dockerfile or Compose
- Production-only flows that are outside the beta

<video :src="'/videos/mac-beta/08-unsupported.mp4'" controls muted width="100%"></video>

<!--
Timeline id: unsupported
Voiceover: voiceover/text/11-unsupported.txt
VHS: tapes/08-unsupported.tape
-->

---

# Beta boundaries

Supported today:

- Native SwiftPM and Xcode apps on Apple Silicon Macs

Not yet:

- Linux/WendyOS containers on Mac targets
- Production mTLS, provisioning, and Wendy Cloud support

Use it as a development preview for trusted networks and controlled environments.

<!--
Timeline id: beta-boundaries
Voiceover: voiceover/text/12-beta-boundaries.txt
-->

---

# Close: the mental model

Install and configure Wendy Agent once, then deploy native SwiftPM or Xcode apps headlessly with the same Wendy CLI workflow.

Next up:

- containers
- mTLS and provisioning
- Wendy Cloud integration
- production hardening

<!--
Timeline id: close-summary
Voiceover: voiceover/text/13-close-summary.txt
-->

---

# Thanks

wendy.dev

Contact: konstantin@wendy.sh

<!--
Timeline id: thanks
Voiceover: voiceover/text/14-thanks.txt
-->
