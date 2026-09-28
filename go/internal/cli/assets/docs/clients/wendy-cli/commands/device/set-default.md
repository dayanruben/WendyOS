Selects a device as the [default device](../../device-selection.md), so that other commands default to this device if available.

The default can be a hostname, IP address, provider key, or explicit `host:port` value:

```sh
wendy device set-default my-mac.local:50051
wendy device info --json
wendy run
```

Use `wendy device get-default` to see the current default, or `wendy device unset-default` to clear it.

A device name made only of digits is rejected. To make a cloud asset the default, pass its cloud selector, `cloud://<cloud-grpc-host:port>/org/<org-id>/asset/<asset-id>`; the error for a numeric name prints the exact selector when you are logged in.

## Scripts and other non-interactive shells

With no argument, `set-default` opens the device picker, which needs an interactive terminal. In a script, CI, an AI agent's shell, or with `--json`, it instead exits with an error that shows the command to run and the devices this CLI has seen recently:

```sh
wendy device set-default wendyos-calm-zinnia.local
wendy device set-default vm:dev
```

To target a different device in one shell or AI session without changing the default that every session shares, set `WENDY_DEVICE` instead (see [device selection](../../device-selection.md)).

## Certificate pinning

When you set a default device (and again on the first successful connection if the device was offline at set-default time), the CLI **pins** the device's identity — the organisation and cloud host its TLS certificate belongs to. On every later connection to the default device, the CLI checks that the device still presents that same organisation and cloud host.

A routine certificate **renewal or re-enrollment** keeps the same organisation and cloud, so it is accepted silently. A change of organisation or cloud host — which can indicate a man-in-the-middle or a swapped device — **refuses the connection**. So does a device that was pinned while enrolled but now answers with no identity at all, which means it was reflashed, factory reset, or replaced by something squatting its name.

Both refusals are **unconditional**: they read identically in interactive, `--json`, and non-interactive runs, and there is deliberately **no "trust this anyway?" prompt** — a man-in-the-middle warning that can be dismissed gets dismissed. Re-running `wendy device set-default` does not re-pin either. Clearing the pin is a separate, deliberate act:

```sh
wendy device unpin <host>
```

Loopback targets are pinned per endpoint: `127.0.0.1:50051`, `127.0.0.1:50151` and `localhost:50051` are different devices, and a connection also pins the port that answered it (usually the next one, which carries mTLS). `127.0.0.1` on a running VM's forwarded agent port, or on the mTLS port after it, is that VM, pinned as `vm:<name>`; the VM's identity is also pinned at both of those ports (unless an older pin there names a different device), so the pin still applies while the VM is stopped. `wendy device set-default 127.0.0.1` refers to `127.0.0.1:50051`.
