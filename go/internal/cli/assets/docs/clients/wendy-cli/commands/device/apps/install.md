Installs an app from the [Wendy AppStore](https://appstore.wendy.dev) onto the target device.

This is the canonical spelling. (There is no top-level `wendy app install`; `wendy install` is the OS-image installer.)

## Flags

| Flag | Description |
|------|-------------|
| `--api` | Override the Wendy AppStore resolution API base URL (default: `$WENDY_APPSTORE_API` or the built-in default). |
| `--no-start` | Create the container but do not start it. |

## Examples

```sh
wendy device apps install jellyfin
wendy device apps install jellyfin --no-start
```
