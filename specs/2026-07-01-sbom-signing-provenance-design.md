# SBOMs + signing + SLSA provenance for WendyOS releases

**Date:** 2026-07-01
**Status:** Design — approved approach, pending spec review
**Related:** WDY-1001 (Reproducible builds / SPDX SBOM / cosign+SLSA provenance), `specs/2026-06-28-security-page-and-threat-model-refresh-design.md`

## Goal

Produce SPDX Software Bills of Materials for every WendyOS release artifact, sign
them, and emit SLSA build provenance — all attached to (or verifiable against) the
GitHub release. This satisfies the SBOM + signing portion of WDY-1001 and lets the
security page claim SBOM + signed releases with an honest, verifiable status.

## Scope

**In scope — SBOMs for:**
- The shipped Go binaries: `wendy` (CLI) and `wendy-agent`, across all release targets
  (linux amd64/arm64, windows amd64/arm64, darwin amd64/arm64).
- Swift components: `WendyAgentCore` and `WendyAgentMac` (via their `Package.resolved`).
- Whole-repo source tree (one aggregate SBOM).

**In scope — signing + provenance:**
- SBOM attestation binding each shipped binary's digest to its SBOM.
- SLSA v1 build provenance for each shipped binary.
- Keyless signing via the GitHub OIDC identity already used in `build.yml`.

**Out of scope (YAGNI):**
- CycloneDX format (SPDX-only for now; add later if a consumer requires it).
- SBOMs for the `Examples/` sample apps (not shipped).
- Container-image SBOMs.
- Key-based (non-keyless) cosign signing / self-managed keys.

## Decisions

| Decision | Choice | Why |
|---|---|---|
| Generator | [Syft](https://github.com/anchore/syft) | De-facto standard; catalogs Go binaries, Swift `Package.resolved`, and source dirs. |
| Format | SPDX-JSON | Matches WDY-1001 ("SPDX SBOM"); widely consumed. |
| Signing / provenance | GitHub-native artifact attestations (`actions/attest-build-provenance`, `actions/attest-sbom`) | Keyless via existing OIDC; Sigstore/Fulcio/Rekor under the hood; verifiable with both `gh attestation verify` and `cosign verify-blob-attestation`. No key management. |
| Go SBOM granularity | Per-binary (`syft scan file:<binary>`) | Reads Go build info embedded in the actual shipped binary → exact dependency set, and gives a digest to bind attestations to. |
| Placement | One dedicated `sbom` job + one shared script | Centralizes logic vs. sprinkling across 8 build matrix jobs; the script is reusable locally, supporting the reproducible-builds goal. |

## Architecture

### 1. Shared script — `scripts/generate-sbom.sh`

Single source of truth, runnable locally and in CI. Contract:

- **Inputs:** `--binaries-dir <dir>` (directory of downloaded built binaries),
  `--out-dir <dir>` (where SBOMs are written), `--version <version>`.
- **Behavior:**
  1. Per-binary: for each shipped binary found under `--binaries-dir`, run
     `syft scan file:<binary> -o spdx-json` → `<artifact-name>-<version>.spdx.json`.
  2. Swift: `syft scan dir:swift -o spdx-json` → `wendy-swift-<version>.spdx.json`
     (reads both `Package.resolved` files).
  3. Whole-repo source: `syft scan dir:. -o spdx-json` with excludes for
     `./node_modules/**`, `**/.build/**`, `./Examples/**`, `./.git/**` →
     `wendy-source-<version>.spdx.json`.
- **Output:** all `*.spdx.json` files in `--out-dir`; exits non-zero on any Syft failure
  (no silent SBOM gaps).
- **Syft version:** pinned (env `SYFT_VERSION`) so local and CI output match.

### 2. New `sbom` job in `.github/workflows/build.yml`

- `needs: [determine-version, build, build-go-macos, build-agent-macos-app]`.
- `permissions: { id-token: write, attestations: write, contents: read }`.
- Steps:
  1. `actions/checkout` (source needed for Swift + repo SBOMs).
  2. Download all built binary artifacts (`actions/download-artifact`, `merge-multiple`).
  3. Install Syft at the pinned version (pinned action or pinned release binary).
  4. Run `scripts/generate-sbom.sh` → SBOM files.
  5. For each shipped binary: `actions/attest-sbom` (subject = binary, predicate = its SBOM)
     and `actions/attest-build-provenance` (subject = binary).
  6. `actions/upload-artifact` the `*.spdx.json` files so the `release` job collects them.

Runs on every build (nightly prereleases + stable publish). Attestation predicates are
always produced and bind to whatever release is created; release gating is unchanged.

### 3. `release` job change

- Extend the artifact glob (currently `build.yml` ~line 685:
  `*.tar.gz -o *.zip -o *.msi -o *.deb -o *.rpm`) to also match `*.spdx.json`, so SBOMs
  attach as release assets alongside the archives.
- No change to release triggering or version logic.

### 4. Docs

- Add a verification section (how to run `gh attestation verify <artifact>` and
  `cosign verify-blob-attestation`) — as release-notes boilerplate and/or a
  `security/VERIFICATION.md`.
- Update `security/THREAT_MODEL.md` and the WDY-1001 status in the security specs to
  reflect that the SBOM + provenance portion ships.

## Data flow

```
build / build-go-macos / build-agent-macos-app
        │  (binaries as workflow artifacts)
        ▼
   sbom job ── generate-sbom.sh ──► *.spdx.json
        │                              │
        ├─ attest-sbom ────────────────┤ (binary digest ↔ SBOM, signed, Rekor)
        ├─ attest-build-provenance ─────┘ (SLSA v1, signed, Rekor)
        └─ upload-artifact (*.spdx.json)
        ▼
   release job ── attaches *.spdx.json as release assets
```

## Error handling

- Any Syft scan failure → script exits non-zero → `sbom` job fails (no partial/missing
  SBOMs slip through silently).
- The `sbom` job is a hard dependency of `release`, so a release never publishes without
  its SBOMs and attestations.

## Testing / verification

- Local: run `scripts/generate-sbom.sh` against a local `go build` output; assert one
  `.spdx.json` per binary plus Swift + source SBOMs, and that each validates as SPDX
  (e.g. `syft convert` round-trip or an SPDX validator).
- CI: a `workflow_dispatch` prerelease run produces attestations verifiable via
  `gh attestation verify` against the published binaries.

## Rollout

1. Land `scripts/generate-sbom.sh` + local test.
2. Add the `sbom` job (attestations only) and confirm on a prerelease run.
3. Wire `*.spdx.json` into the `release` asset glob.
4. Add verification docs and update WDY-1001 / threat-model status.
