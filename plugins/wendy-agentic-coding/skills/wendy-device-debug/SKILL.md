---
name: wendy-device-debug
description: Use when debugging live WendyOS or Wendy Lite devices, including ESP32 camera/SensorLink failures, Jetson, Raspberry Pi, USB-C host mode, containerd, GPU/audio/video entitlements, or device-specific `wendy run` issues.
---

# Wendy Device Debug Workflow

Use this for live-device and cross-layer runtime bugs. Static code reading is not enough when the failure depends on device state.

## Triage sequence

1. Capture the exact failing command and whether it was run with installed `wendy` or source `wendy-dev`.
2. Follow [wendy-device-ops](../wendy-device-ops/SKILL.md) for device access, selection, and inspection before tracing source code.
3. Use those observations to separate layers:
   - CLI behavior: command parsing, target selection, build provider, gRPC request.
   - Agent behavior: service implementation, containerd adapter, OCI spec, logs.
   - WendyOS behavior: image version, device type, mounts, CDI, system services.
   - Wendy Lite behavior: reported board/target, firmware version, WASM/native app support, SensorLink manifest, app state.
   - App behavior: Dockerfile/Containerfile, `wendy.json`, entitlements, environment, startup logs.
4. Correlate the failure with relevant app state and bounded logs before choosing a fix.

## Jetson GPU checks

Use device info, hardware capabilities, and app logs to check:

- The reported device type identifies the expected Jetson variant.
- The agent maps the device type to the expected Wendy platform.
- The application handles CUDA absence gracefully and exposes enough debug state.

If evidence points to GPU provisioning, inspect the image and agent code for the device's JetPack version. JetPack 6 uses `/etc/cdi/nvidia.yaml`; JetPack 5 uses the L4T CSV fallback at `/etc/nvidia-container-runtime/host-files-for-container.d/*.csv`. Keep source configuration distinct from confirmed device state.

## Repo files to inspect

Use the [wendy-codebase](../wendy-codebase/SKILL.md) map to locate the CLI, agent, runtime, and app configuration code for the failing layer. Inspect WendyOS image recipes and services when evidence points to image state.

## Output discipline

Give the root cause by layer. Distinguish what can be fixed in the repo now from what depends on device image state or host compatibility.
