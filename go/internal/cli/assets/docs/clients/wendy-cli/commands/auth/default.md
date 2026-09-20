# `wendy auth default`

Shows or clears the current **auth context**.

## Usage

```sh
wendy auth default [--clear]
```

## Description

With no flags, prints the current context and the session it names
(`<context> (org N — endpoint)`). If the current context points at a session
that no longer exists (stale), it is automatically cleared and a warning is
printed.

Pass `--clear` to unset the current context. With no current context set,
resolution falls back to an interactive picker (or errors in a non-interactive
environment).

## Flags

| Flag | Description |
|------|-------------|
| `--clear` | Unset the current context. |

## Examples

```sh
# Show the current context
wendy auth default

# Clear the current context
wendy auth default --clear
```

## See also

- [`wendy auth use`](./use.md) — switch the current context
- [`wendy auth rename`](./rename.md) — rename a context
