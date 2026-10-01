# Device enrollment with an OIDC account

Direct ACME enrollment is experimental and disabled by default on the agent.
Notifications, mesh routing, and legacy Avahi identity advertisements still
require numeric organization and asset IDs. Use legacy enrollment for those
services. For development, explicitly set `WENDY_EXPERIMENTAL_ACME_ENROLLMENT=1`
in the agent environment before attempting OIDC enrollment. The agent rejects
disabled enrollment before generating keys or spending EAB credentials. The CLI
and MCP enrollment tool also check the agent's read-only readiness before
requesting an EAB or reserving a Cloud name. Older agents that do not advertise
readiness must be updated before using these clients; older clients remain
compatible, but do not perform this preflight.

```sh
wendy device enroll --name sim
```

Sessions created by OIDC login (`oauthIssuer` is set) automatically obtain
class B enrollment credentials through Cloud. Legacy sessions retain the
Cloud enrollment-token RPC and `--org` override. `wendy cloud enroll-device`
is an alias with the same behavior.

The CLI uses the selected session's tenant; it does not list organizations.
`--org` is rejected for OIDC enrollment. The CLI generates a UUID for the
permanent SPIFFE device identity. The discovery name is a separate, renameable
DNS label; renaming it does not change the certificate identity.

## Automatic credential handoff

1. Check that the agent is not already provisioned and explicitly advertises
   ACME enrollment enabled in v2 `IsProvisioned.not_provisioned`. Validate the
   device name and ACME directory before requesting credentials. Missing or
   disabled readiness stops before contacting Cloud; it does not alter the
   agent environment. Readiness only reports the experimental gate, not network
   health or a guarantee of successful issuance.
2. Call `wendycloud.v2.DeviceEnrollmentService/EnrollDevice` with the OIDC
   bearer token and two separate operator signatures:
   - `x-wendy-request-signature`, scoped to the Cloud method and
     `org/<tenant>/device/<device-id>` resource.
   - `enrollment_request_jws`, with `tenant`, `device_id`, `device_class: B`,
     `iat`, `exp` and a fresh `jti`. This authorization is valid for five minutes;
     that is not an expiration time for the resulting EAB credential.
3. Cloud checks `device:enroll` permission, reserves the asset name and relays
   the enrollment JWS unchanged to PKI's private `RelayEnrollment` RPC.
4. Cloud returns an asset UUID and the once-only EAB credentials. The CLI
   passes these directly to the agent's v2 `StartACMEProvisioning` RPC.

No credentials file is needed. The login certificate signs the request; PKI
mints the EAB secret. The CLI neither prints nor persists that secret.

The Cloud signature header carries only the operator leaf certificate: Cloud
validates it against PKI's own CA material. Including the ML-DSA intermediates
would exceed the broker's 16 KiB HTTP/2 header limit alongside the bearer token.
The enrollment JWS retains the full chain in the protobuf body.

Cloud does not return an ACME directory URL. The CLI derives one from the
pki-core identity endpoint the session already holds, by replacing its leading
`identity.` label:

```text
https://identity.<rest>/v1/identity/certificate
  -> https://acme.<rest>/<session-tenant>/acme/directory
```

That keeps the derivation inside one PKI deployment and names no environment,
so a self-hosted pki-core derives its own directory exactly as the hosted one
does. A session whose identity endpoint is not an `https://identity.<rest>`
URL derives nothing and must supply `--acme-directory-url <url>`. The directory
must use HTTPS (HTTP is allowed on loopback development servers), and its
tenant must match the selected login.

## Device-side behavior and failures

The agent generates and retains its own private key and ACME account key. It
uses the EAB to register an account and orders a `permanent-identifier`
certificate. Before persisting it, the agent checks the certificate's key and
`spiffe://wendy.sh/tenant/<tenant>/device/<device-id>` identity. Successful
provisioning persists the certificate, chain, principal and directory URL;
it does not persist the EAB secret.

Both Cloud's enrollment service and an agent with `StartACMEProvisioning` are
required. Unsupported deployments produce an explicit error, with no fallback
to legacy enrollment. This command supports class B; class A attestation and
class C EST enrollment are not implemented.

Cloud and device enrollment are separate operations. If Cloud succeeds but the
device step fails, Cloud keeps the asset and name reservation. The CLI reports
the asset UUID; restarting the command does not retrieve the original secret
and may encounter that reservation. Automatic recovery across those two
operations requires additional Cloud support. The agent retains its ACME
account key for retries, but Cloud has no credential retrieval RPC.

## Imaging and first boot

`os install --pre-enroll` reserves a Cloud name and writes the device UUID,
ACME directory, EAB and Cloud host to `/config/acme-enrollment.json`. This is
credential staging, not verified device enrollment, and does not enable the
experimental flag. Agents predating this consumer do not read that file.

The baked handoff is trusted provisioning input, not a signed artifact. Anyone
who can replace it before consumption can choose the device's PKI deployment,
tenant and identity. Use only trusted imaging hosts/media and restrict access
to the config partition through first boot; FAT media do not enforce Unix file
permissions. The experimental flag does not authenticate the handoff. Do not
use this experimental path where the imaging/config-partition write boundary
is untrusted; handoff signing/sealing is not implemented here.

With explicit experimental opt-in, the agent reads the baked handoff after
provisioning callbacks are installed and uses the same ACME provisioning path
as manual enrollment. The attempt is non-blocking, bounded to two minutes,
and does not fall back to legacy enrollment. Conflicting legacy and ACME
handoffs are retained without choosing an identity. Disabled, invalid or
already-provisioned devices retain the baked credential without redeeming it.

Before redemption, a private, nonsecret `acme-first-boot-attempt.json` marker
records the expected PKI principal. A failure or interrupted attempt retains
the baked credential and device/account keys, but blocks automatic attempts
on later agent restarts. Do not delete the marker or rotate keys blindly:
issuance may have completed even when local provisioning failed. Recovery
requires authoritative identity and certificate evidence and explicit operator
authorization. The baked file is removed only after successful provisioning;
a file-removal failure does not trigger another issuance.

Successful certificate provisioning is not proof of broker heartbeat or live
metadata. Verify those independently before reporting Cloud device readiness.
Notifications and mesh remain subject to the legacy limitations above.

## Contract sources

`Proto/wendycloud/v2/device_enrollment.proto` is copied unchanged from
Cloud's `cloud-proto/device_enrollment.proto` (commit `c194cf91`). Cloud owns this
public API outside its shared service-protos submodule, but it is vendored
beside the shared v2 contracts and generated from the same include root, so a
re-copy stays a plain `cp`.

PKI's `internal/fabric/enrollment_request.go`,
`docs/reference/api/acme.md` and `docs/reference/fabric-relay-for-issuance.md`
define the enrollment artifact and downstream enrollment protocol.
