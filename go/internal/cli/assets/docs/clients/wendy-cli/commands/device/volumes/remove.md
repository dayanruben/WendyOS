Removes a volume from the remote Wendy device to clean up disk space.

```sh
wendy device volumes remove [name] [--force]
```

With a `[name]` argument, removes that volume. Without one, lists all volumes
(including their combined size) and lets you select one interactively.

## Flags

| Flag | Description |
|------|-------------|
| `--force` | Skip the confirmation prompt. |
