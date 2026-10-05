# `wendy cloud login`

Authenticates the CLI with Wendy Cloud. This is the primary login entry point.

## Usage

```sh
wendy cloud login --email you@example.com
wendy cloud login --production --email you@example.com
wendy cloud login --development --email you@example.com
wendy cloud login --development --issuer https://auth.dev.wendy.sh/realms/<realm>
wendy cloud login --service-account ./wendy-service-account.json
wendy cloud login --legacy
```

## Description

`wendy cloud login` is identical to [`wendy auth login`](../auth/login.md) — it
reuses the same implementation. It targets the production Cloud by default;
`--production` makes that choice explicit, `--development` selects development,
and `--legacy` selects the previous Cloud. Production and development use OIDC:
the CLI discovers your realm from `--email` (or takes `--issuer` directly),
completes authorization code + PKCE through a loopback callback, obtains an
operator certificate from pki-core, and stores it with a refreshable Cloud API
session. Subsequent commands use the certificate automatically.

The current Cloud flow temporarily requires `--email` unless `--issuer` is
provided, so a bare `wendy cloud login` selects production but exits with that
instruction instead of falling back to legacy. `--api-key` continues to select
local authentication. `--service-account` signs in headlessly as a wendy-auth
service account (see [`wendy auth login`](../auth/login.md#service-account-login)).

Pass `--legacy` to use the old Wendy Cloud dashboard enrollment callback
(`cloud.wendy.sh`). The three target flags are mutually exclusive, and legacy is
never selected implicitly.

`wendy auth login` remains functional for backward compatibility but is no
longer listed in the top-level help. See [`wendy auth login`](../auth/login.md)
for the full flag reference and multi-session behaviour.

## Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--email` | `""` | Email address temporarily required to discover your realm and sign in (OIDC flow). |
| `--issuer` | `""` | Complete realm issuer URL; skips email-based realm discovery. |
| `--service-account` | `""` | Service-account key file for headless login; `WENDY_SERVICE_ACCOUNT_KEY` may hold its contents instead. |
| `--production` | `false` | Explicitly use production (also the default when no target flag is passed). |
| `--development` | `false` | Use the development Cloud. |
| `--legacy` | `false` | Use the old cloud-dashboard enrollment flow (`cloud.wendy.sh`). |
| `--cloud` | `""` | Dashboard URL of a non-default cloud instance. |
| `--cloud-grpc` | `""` | gRPC endpoint of a non-default cloud instance. |

## See also

- [`wendy cloud logout`](./logout.md)
- [`wendy cloud status`](./status.md)
- [`wendy auth use`](../auth/use.md) — advanced multi-session management
