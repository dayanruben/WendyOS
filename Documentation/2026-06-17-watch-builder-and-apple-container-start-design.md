# `--builder` on `wendy watch` + Apple Container auto-start

Date: 2026-06-17
Branch: `jo/fast-build`
Status: Design — pending review

## Problem

Two related gaps in the local image-builder support:

1. `wendy run`, `wendy build`, and `wendy cloud run` accept `--builder docker|apple-container`,
   but `wendy watch` does not, even though it reuses `runOptions` and calls `runCommand`.
   You cannot watch-redeploy through Apple Container.

2. When `--builder apple-container` is selected explicitly but the Apple Container system
   (apiserver) is not running, Wendy fails hard:

   ```
   ✗ building service api: Apple Container system is not running: apiserver is not running.
     Run 'container system start' and try again: exit status 1
   ```

   By contrast, the Docker path (`ensureDockerDaemon`) actively starts the runtime — prompting
   the user interactively, or auto-starting when non-interactive, and waiting up to 60s. Apple
   Container has no equivalent, so the user is left to start it by hand. The behavior should be
   consistent: offer to start the system.

## Goals

- `wendy watch --builder docker|apple-container` works, identical semantics to `wendy run`.
- When the Apple Container builder is selected **explicitly** and the system is not running,
  Wendy offers to start it (interactive prompt) or auto-starts it (`--yes` / non-interactive /
  `wendy watch`), waits for readiness, and surfaces a clear error if it cannot start.
- The silent **auto-attempt** path (darwin/arm64, no explicit `--builder`) is unchanged: it must
  keep doing a fast non-mutating check and falling back to Docker without prompting or starting.

## Non-goals

- Repairing a broken/mismatched local `container` install. On the reporter's machine
  `container system start` itself fails with `failed to decode apiServerBuild in health check`
  (apiserver/CLI version mismatch). Wendy cannot fix that; it will surface the start output in a
  clear "could not start" error instead of a bare status-check failure.
- Changing Docker daemon handling.

## Design

### Part 1 — `--builder` on `wendy watch`

Register the flag in `newWatchCmd` (`go/internal/cli/commands/watch.go`), matching `run.go`:

```go
cmd.Flags().StringVar(&opts.builder, "builder", "",
    "Image builder to force for Dockerfile/Containerfile builds: docker or apple-container")
```

`opts.builder` already flows through `runCommand` → `normalizeImageBuilder` → the build paths, so
no further wiring is needed. Across watch cycles the system stays up after the first start, so
later redeploys do not re-prompt.

### Part 2 — `ensureAppleContainerSystem`

New helper in `go/internal/cli/commands/docker.go`, modeled on `ensureDockerDaemon`:

```go
// ensureAppleContainerSystem verifies the Apple Container system is running,
// offering to start it when it is not. assumeYes skips the interactive prompt
// (set from --yes and by `wendy watch`).
func ensureAppleContainerSystem(ctx context.Context, assumeYes bool) error
```

Steps:

1. Verify host is darwin/arm64 and the `container` CLI is present and usable — the existing front
   half of `checkAppleContainerBuilder` (`imageBuilderHostGOOS`/`GOARCH`, `imageBuilderLookPath`,
   `container --version`). Factor this into a small shared helper so the check is not duplicated.
2. Run `container system status`. If it succeeds, return nil. This is cheap and idempotent.
3. If not running:
   - If an interactive terminal **and** not `assumeYes`: prompt
     `Apple Container system is not running. Start it now? [Y/n] `. A "no" answer returns the
     current error (unchanged guidance to run `container system start`).
   - Otherwise (non-interactive, or `assumeYes`): auto-start, mirroring `ensureDockerDaemon`.
   - Run `container system start --timeout 60`, then poll `container system status` every 2s until
     it succeeds or a ~60s deadline elapses (respecting `ctx`).
   - On failure to become ready, return an error including a sanitized summary of the `start`
     output (via `safeCommandOutputSummary`) so install problems like
     `failed to decode apiServerBuild in health check` are visible.

`checkAppleContainerBuilder` remains a pure, side-effect-free status check used by the
auto-fallback paths.

### Wiring (explicit-builder paths only)

`ensureAppleContainerSystem` is called only where the Apple Container builder is selected
explicitly, before any build:

- **`wendy run` (single service):** in `buildAndPushImageForAgent`, explicit branch
  (`imageBuilderWasExplicit(builder)`), when the normalized builder is apple-container.
- **Compose / multi-service:** once at the top of `buildServicesParallel` (before the parallel
  goroutines) when the builder is explicit apple-container, so the prompt appears once rather than
  once per service. The per-service `buildAndPushImageForAgent` call is then a no-op (system
  already running).
- **`wendy build` (local):** the explicit branch of `buildDockerProjectWithBuilder`.

`assumeYes` is sourced from `opts.yes` at each call site (and is implicitly true under
`wendy watch`, which sets `opts.yes = true`).

The auto-attempt paths (`buildDockerProjectWithBuilder` auto branch, `buildAndPushImageForAgent`
`shouldAutoAttemptAppleContainerBuilder` branch) do **not** call `ensureAppleContainerSystem`;
they keep calling `checkAppleContainerBuilder` and silently fall back to Docker.

## Error handling

- Declining the prompt → today's "system is not running" error, unchanged.
- Start attempted but never ready → "could not start Apple Container system" error wrapping the
  sanitized `container system start` output.
- Non-darwin/arm64 or missing CLI → same errors `checkAppleContainerBuilder` returns today.

## Testing

Unit tests (table-driven, in `docker_test.go`), using the existing seams
`imageBuilderCommandContext`, `imageBuilderLookPath`, `imageBuilderHostGOOS`/`GOARCH`, plus an
injectable interactive-terminal seam for the prompt:

- system already running → no start invoked, returns nil.
- not running + `assumeYes` → `container system start` invoked, then status polled, returns nil
  once status passes.
- not running + interactive + declined → returns the unchanged error, no start invoked.
- start runs but status never passes → returns the "could not start" error containing the start
  output summary.
- non-darwin/arm64 / missing CLI → returns the existing guard errors.

Plus a CLI-surface test that `wendy watch --builder apple-container` parses and routes through
`normalizeImageBuilder` (alongside the existing builder flag tests).

## Files touched

- `go/internal/cli/commands/watch.go` — register `--builder`.
- `go/internal/cli/commands/docker.go` — `ensureAppleContainerSystem`, shared CLI-presence helper,
  call sites in `buildAndPushImageForAgent` and `buildDockerProjectWithBuilder`.
- `go/internal/cli/commands/multibuild.go` — single ensure call in `buildServicesParallel`.
- `go/internal/cli/commands/docker_test.go` — unit tests.
