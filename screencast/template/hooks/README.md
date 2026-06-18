# Screencast hooks

`../scripts/render-tapes.sh` can run project hooks around VHS rendering.

- `preflight.sh` — non-destructive checks; runs by default when executable.
- `setup.sh` — destructive/setup work; runs with `render-tapes.sh --with-hooks`.
- `teardown.sh` — cleanup work; runs with `render-tapes.sh --with-hooks`.

Hooks receive:

- `SCREENCAST_ROOT` — absolute path to `screencast/`.
- `SCREENCAST_YES` — `1` when confirmations are skipped.
- `SCREENCAST_DRY_RUN` — `1` for dry-run mode.
