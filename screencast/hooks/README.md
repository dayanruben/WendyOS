# Screencast hooks

`render-tapes.sh` can run project hooks around VHS rendering.

- `preflight.sh` — non-destructive checks; runs by default when executable.
- `setup.sh` — destructive/setup work; runs with `render-tapes.sh --with-hooks`.
- `teardown.sh` — cleanup work; runs with `render-tapes.sh --with-hooks`.

Hooks receive these environment variables:

- `SCREENCAST_ROOT` — absolute path to `screencast/`.
- `SCREENCAST_YES` — `1` when confirmations are skipped.
- `SCREENCAST_DRY_RUN` — `1` for dry-run mode.

For this Mac beta screencast, `setup.sh` resets the local recording machine and
removes managed apps from the Mac mini target. `teardown.sh` removes the local
container demo directory created by the unsupported-paths tape.
