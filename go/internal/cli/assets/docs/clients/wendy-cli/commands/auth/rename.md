# `wendy auth rename`

Renames an auth **context**. Context names are how [`wendy auth use`](./use.md)
selects a session.

## Usage

```sh
wendy auth rename [old] <new>
```

## Description

With **two** arguments, renames the context `<old>` to `<new>`. With **one**
argument, renames the current context to `<new>` (see
[`wendy auth default`](./default.md) for the current context).

The first login is always named `default`; rename it once you add a second
organization so both contexts have meaningful names. If the renamed context is
the current one, it stays current under its new name.

Renaming onto an existing context name, or to an empty name, is refused.

## Flags

_None._

## Examples

```sh
# Rename the default context after adding a second org
wendy auth rename default acme

# Rename a specific context
wendy auth rename org-42 staging

# Rename the current context
wendy auth rename prod
```

## See also

- [`wendy auth use`](./use.md) — switch the current context
- [`wendy auth default`](./default.md) — show or clear the current context
- [`wendy auth login`](./login.md)
