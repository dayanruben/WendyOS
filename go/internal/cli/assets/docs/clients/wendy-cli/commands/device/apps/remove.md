Removes an app from the device. With an `[app-name]` argument, removes that app;
otherwise lists all apps and allows interactive removal.

```sh
wendy device apps remove [app-name] [flags]
```

## Flags

| Flag | Description |
|------|-------------|
| `--cleanup` | Also remove the container image, reclaiming disk space. |
| `--delete-volumes` | Also delete the app's persistent volumes. |
| `--force` | Skip confirmation prompts. |

When neither `--cleanup` nor `--delete-volumes` is given and `--force` is
absent, the command shows an interactive "Also clean up?" checklist offering to
delete the container image and the persistent volumes, so the cleanup choices
are still reachable without passing flags.
