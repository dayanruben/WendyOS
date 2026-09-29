Selects a device as the [default device](../../device-selection.md), so that other commands default to this device if available.

The default can be a hostname, IP address, provider key, or explicit `host:port` value:

```sh
wendy device set-default my-mac.local:50051
wendy device info --json
wendy run
```

Use `wendy device get-default` to see the current default, or `wendy device unset-default` to clear it.

## Certificate pinning

When you set a default device (and again on the first successful connection if the device was offline at set-default time), the CLI **pins** the device's identity — the organisation and cloud host its TLS certificate belongs to. On every later connection to the default device, the CLI checks that the device still presents that same organisation and cloud host.

A routine certificate **renewal or re-enrollment** keeps the same organisation and cloud, so it is accepted silently. A change of organisation or cloud host — which can indicate a man-in-the-middle or a swapped device — **refuses the connection**. So does a device that was pinned while enrolled but now answers with no identity at all, which means it was reflashed, factory reset, or replaced by something squatting its name.

Both refusals are **unconditional**: they read identically in interactive, `--json`, and non-interactive runs, and there is deliberately **no "trust this anyway?" prompt** — a man-in-the-middle warning that can be dismissed gets dismissed. Re-running `wendy device set-default` does not re-pin either. Clearing the pin is a separate, deliberate act:

```sh
wendy device unpin <host>
```
