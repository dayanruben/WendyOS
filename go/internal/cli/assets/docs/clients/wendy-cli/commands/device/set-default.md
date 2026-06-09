Selects a device as the [default device](../../device-selection.md), so that other commands default to this device if available.

The default can be a hostname, IP address, provider key, or explicit `host:port` value. For the WendyAgentMac beta on the same Mac as the CLI, use:

```sh
wendy device set-default localhost:50051
wendy device info --json
wendy run
```

Use `wendy device unset-default` to clear the saved target.