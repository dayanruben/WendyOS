`wendy project optimize` statically analyzes your project's build configuration — its Dockerfile(s), `requirements.txt`, and `wendy.json` — and reports missed build-speed and runtime optimizations.

It runs locally and is read-only by default. It works on a single Dockerfile, a multi-service / Compose project (findings are grouped by service), and native Swift (`Package.swift`) or `Brewfile` projects.

## What it checks

- **Build caches** — compiled-language build/install steps (`cargo`, `go`, `swift`, `npm`/`yarn`/`pnpm`, `pip`) that run without a BuildKit `--mount=type=cache`, and so re-download or re-compile dependencies on every build.
- **Release vs. debug** — debug builds shipped to production (`swift build` without `-c release`, `cargo build` without `--release`), and whether `WENDY_DEBUG` is wired to toggle the optimization level.
- **CUDA / ML** — a CPU-only ML wheel (e.g. `torch==…+cpu`) paired with the `gpu` entitlement (or a CUDA wheel without it), and x86 `nvidia/cuda` base images on an arm64 (Jetson) target.
- **Architecture & image** — an `amd64` base image on an arm64 device (which runs under slow QEMU emulation or fails), a missing `.dockerignore`, and single-stage builds that ship their full build toolchain.

## Usage

```bash
wendy project optimize            # report findings (colorized in a terminal, JSON in CI)
wendy project optimize --json     # machine-readable findings
wendy project optimize --fix      # apply the safe, deterministic fixes
wendy project optimize --agentic  # emit a context bundle for an AI agent
```

## Flags

- `--fix` — apply the safe fixes only: add a build-cache mount, add the release flag (`swift`/`cargo`), and create a default `.dockerignore`. Fixes are idempotent; contextual changes (multi-stage refactors, choosing the right CUDA wheel) are left to you or the `--agentic` flow.
- `--agentic` — instead of a report, emit a JSON bundle (static findings plus the verbatim project files and a prompt) designed to be piped into Claude Code or the Wendy MCP server.
- `--severity <info|warning|error>` — the minimum severity that causes a non-zero exit. Defaults to `warning`.
- `--arch <arch>` — override the target architecture (defaults to `arm64`).
- `--json` — emit findings as JSON (also the default when output is not a terminal, e.g. in CI).

## Exit codes

- `0` — no findings at or above the severity threshold.
- `1` — findings at or above the threshold (use this to gate CI).
- `2` — execution error (no project found, parse failure).

## At build time

After a slow incremental build (one that reused cached layers and still took more than ~50s), `wendy run` / `wendy build` will run this scan automatically in an interactive terminal, show the findings, and offer to apply the safe fixes for your next build. This never runs in CI or non-interactive shells.

## A note on `--agentic` and secrets

The `--agentic` bundle includes the **verbatim contents** of your Dockerfile(s), `requirements.txt`, and `wendy.json` so the agent has full context. These files can contain secrets (`ARG`/`ENV` tokens, private registry URLs). The command prints a reminder to stderr; review the bundle before sending it to an external agent.
