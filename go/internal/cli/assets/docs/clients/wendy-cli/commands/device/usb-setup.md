Configures a Linux host so a USB-C-tethered WendyOS device is reachable over its USB network link.

```sh
wendy device usb-setup
```

The changes need root. Run the command as your normal user and it re-runs itself under `sudo`, prompting for your password; `sudo wendy device usb-setup` works too. Without an interactive terminal (CI, an AI agent's shell) it cannot prompt, so it exits with instructions instead of waiting.

It makes two changes:

- A NetworkManager connection named `wendy-usb` on the device's USB network interface, in `shared` mode: the host serves DHCP on `10.42.0.1/24` to the device.
- A udev rule, `/etc/udev/rules.d/99-wendy-usb.rules`, that stops ModemManager from probing the device's serial console (`/dev/ttyACM*`) and network port (USB ID `1d6b:0104`).

Connect the device with a data-capable USB-C cable first. When more than one USB network interface is present, name the one to configure:

```sh
wendy device usb-setup --iface <interface>
```

It needs `nmcli` (NetworkManager) and `udevadm`. On Linux, [`wendy discover`](../discover.md) detects an unconfigured USB-C link and offers to run this setup for you. macOS and Windows need no host setup, and the command exits with an error there.

It does not install the Jetson recovery-mode rule (`70-wendy-jetson.rules`) used for flashing; the wendy deb/rpm packages include that rule, and [`wendy install`](../os/install.md) prints the commands to add it by hand.
