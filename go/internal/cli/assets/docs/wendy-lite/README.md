# Wendy Lite

Wendy Lite is Wendy's runtime and deployment layer for ESP32 microcontrollers. It supports native ESP-IDF applications as the recommended project model and can also run portable WASM guest applications.

## Role in the Wendy Platform

The broader Wendy platform targets Linux/macOS edge devices (Raspberry Pi, Jetson, Mac) via WendyOS and wendy-agent. Wendy Lite covers bare-metal MCUs where containers and a full OS are not viable. Native apps use the complete ESP-IDF API and normal ESP-IDF project layout; `wendy run` detects, builds, and deploys them. WASM remains available when a smaller portable application boundary is preferable.

> **Recommendation:** Start new applications as regular native ESP-IDF projects. Use the optional WASM runtime when portability or sandboxing matters more than full ESP-IDF access. Camera and display/framebuffer peripherals are not exposed to WASM guests and should be driven by native ESP-IDF drivers.

## Supported Targets

| Target | Status |
|--------|--------|
| ESP32-C5 | CI-built, nightly releases |
| ESP32-C6 | CI-built, nightly releases |
| ESP32-C61 | CI-built, nightly releases |
| ESP32-P4 | Supported boards are listed by `wendy install` |
| ESP32-S3 | CI-built, nightly releases |

Boards without a published Wendy firmware variant are not shown by `wendy install`. Native app capability is firmware-specific; where the installer offers multiple choices, select a board variant labeled **native app support**.

Native applications are built with ESP-IDF 5.5.4 through the ESP-IDF Installation Manager (`eim`).

See [`repository.md`](repository.md) for the repository layout and CI/release process.
