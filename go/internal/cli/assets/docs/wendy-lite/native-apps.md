# Native Apps for Wendy Lite

## Overview

Wendy Lite runs on Espressif ESP32 microcontrollers. On top of the operating
system, you can write your own **native applications**, build them, and deploy
them to any device running Wendy Lite.

Native apps are built with Espressif's tooling, in particular
[ESP-IDF](https://docs.espressif.com/projects/esp-idf/), version **5.5**.
ESP-IDF lets you write your application in C or C++. Because it is driven by
CMake, you can also integrate other compiled languages, such as Rust or Swift.

The **Wendy CLI** is your entry point to the device. It lets you:

- install Wendy Lite on a device,
- configure Wi-Fi and cloud access,
- build and deploy your native application.

Deployment can happen over three transports:

| Transport | Availability |
| --- | --- |
| USB | available |
| Wi-Fi (local network) | available |
| Cloud | coming soon |

Configuring a device can additionally go over **Bluetooth Low Energy (BLE)**.
BLE is there mainly to reach a device in the situations where neither Wi-Fi nor
USB is available.

<!-- TODO: list the exact ESP32 targets supported (esp32s3, esp32c5, esp32c6,
     esp32c61, esp32p4, ...). -->

## Prerequisites

Before you start, you need:

- **Wendy CLI** — installs Wendy Lite on a device, configures it, and builds
  and deploys your application.
- **EIM**, the ESP-IDF Installation Manager — Espressif's tool for managing the
  ESP-IDF development environment (toolchains, Python environment, SDK
  versions). Wendy Lite supports **ESP-IDF 5.5**.
- a device already flashed with Wendy Lite.

[Getting Started with Wendy Lite](native-apps-getting-started.md) walks through
installing both tools and flashing a device.

You do not have to pin an ESP-IDF version yourself: `wendy run` picks the right
toolchain for you from the environments EIM manages.

## Anatomy of a Wendy Lite native app

A Wendy Lite native app is an ordinary ESP-IDF project with one addition: it
includes the **`wendy_core`** component.

`wendy_core` is what makes your application a Wendy app rather than a bare
ESP-IDF firmware. It runs your code inside the Wendy environment, so that the
device can be managed by the Wendy CLI and reached through Wendy Cloud.

Creating an app therefore comes down to three steps:

1. Create an ESP-IDF project.
2. Declare `wendy_core` in the project's dependencies.
3. Call `wendy_core_init()` at the very beginning of `app_main()`.

### Step 1 — Create an ESP-IDF project

Use the standard ESP-IDF project layout: a top-level `CMakeLists.txt`, a `main`
component, and a `sdkconfig.defaults` holding your build configuration.

<!-- TODO: document `wendy.json` (appId, version, entitlements) — it is present
     in every example project but its role is not described yet. -->

### Step 2 — Declare the `wendy_core` dependency

Add `wendy_core` to your component manifest, `main/idf_component.yml`:

```yaml
dependencies:
  idf:
    version: ">=5.5.0"
  wendy_core:
    git: "git@github.com:wendylabsinc/wendy-lite.git"
    path: "components/wendy_core"
```

Then require it from your component in `main/CMakeLists.txt`:

```cmake
idf_component_register(SRCS "main.c"
                    INCLUDE_DIRS "."
                    REQUIRES wendy_core)
```

### Step 3 — Initialize Wendy Core

`wendy_core_init()` must be the **first** function your application calls:

```c
#include "wendy_core.h"

void app_main(void)
{
    ESP_ERROR_CHECK(wendy_core_init());

    /* Your application starts here. */
}
```

This call brings up the agent that handles all communication with the Wendy
environment. Nothing else in your application may run before it.

## Starting from an example

The quickest way to start a project is to copy an existing one. The
[`blink-rgb`](https://github.com/wendylabsinc/wendy-lite-native-apps/tree/main/blink-rgb)
project is a deliberately minimal example: it shows
how a project for Wendy Lite is laid out and how `wendy_core` is integrated.

Its `main/main.c` is essentially the three-step recipe above plus a blink loop:

```c
#include <stdio.h>

#include "freertos/FreeRTOS.h"
#include "freertos/task.h"
#include "rgb_led.h"

#include "wendy_core.h"

#define RGB_LED_GPIO 8

void app_main(void)
{
    ESP_ERROR_CHECK(wendy_core_init());

    ESP_ERROR_CHECK(rgb_led_init(RGB_LED_GPIO, 1));

    bool on = false;
    while (true) {
        on = !on;
        if (on) {
            ESP_ERROR_CHECK(rgb_led_set(0, 24, 24, 0));
        } else {
            ESP_ERROR_CHECK(rgb_led_clear());
        }
        vTaskDelay(pdMS_TO_TICKS(500));
    }
}
```

<!-- TODO: add the other example projects (blink, blink-arduino, m5-stamp-fly,
     xiao-esp32s3-camera, xiao-esp32s3-camera-stream) and say what each one
     demonstrates. -->

## What Wendy Lite owns, and what your app must not touch

Wendy Lite handles communication for you. It owns and initializes the
communication controllers and peripherals:

- **USB**
- **Wi-Fi**
- **Bluetooth Low Energy (BLE)** — the system uses it above all to configure the
  device when Wi-Fi and USB are not available.
- **Cloud connectivity**

Your application is free to *use* these channels, but it must
never configure them itself. Do not initialize the Wi-Fi or BLE controller, and
do not set up the network stack: use what Wendy Lite has already configured.

> **Do not use the USB Serial/JTAG controller as a console.**
> Wendy Lite claims that peripheral and drives the console itself. Configuring
> it from your application, or using it for your own I/O, will conflict with
> the system.

<!-- TODO: spell out the concrete "do use" side for each channel: which API an
     app should call to open a socket, to advertise/serve over BLE, to send
     data to the cloud. -->

## Building and deploying

One command covers the whole cycle. Run it from your project directory:

```sh
wendy run
```

`wendy run` compiles the project, pushes the resulting application to the
device, and starts it. It selects the right ESP-IDF toolchain version for you,
so you do not have to pin one yourself. The application is pushed over USB, over
the local network, or (soon) through the cloud.

<!-- TODO: how to choose the transport / target device, and how to read logs
     back from the running application. -->
