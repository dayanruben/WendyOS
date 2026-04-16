# WendyAgent startup refactor plan

## Summary

Refactor `WendyAgent` startup to be deterministic, sequential, and easy to reason about.

Do **not** introduce a `WendyAgentComponent` abstraction for now.
Do **not** use timing-based readiness or startup probes.
Do **not** split orchestration into `WendyAgent` and `WendyAgentRuntime` yet.

Instead, keep the logic in `WendyAgent` and make startup explicitly:

1. set status to `.starting`
2. perform startup work step by step
3. wait for each required subsystem to report real readiness
4. switch to `.running` only after all required subsystems are actually started
5. monitor long-lived runtime tasks for unexpected exit

## Why this change is needed

The current startup behavior became hard to reason about because it mixed:

- sequential setup work
- long-lived runtime execution
- inferred readiness via timing/probe behavior

That led to a bug where the menu stayed on `Starting...` even though the process had effectively started.

The desired behavior is:

- deterministic and solid
- no appearance hacks
- no "sleep means started" logic
- explicit required-vs-optional startup behavior
- easy to extend later when startup gains more real work

## Current high-level startup responsibilities

`WendyAgent` currently owns startup for:

- optional Docker preflight / local registry setup
- main Wendy Agent gRPC server
- local OpenTelemetry gRPC server
- Bonjour advertisement
- status observation / menu updates

These should remain the top-level lifecycle responsibilities.

## Design principles

1. **Keep `WendyAgent` as the orchestrator**
   - No extra runtime type for now.
   - No component protocol for now.

2. **Keep `.starting` visible and meaningful**
   - `.starting` should represent real startup work in progress.
   - `.running` should only mean all required runtime pieces are ready.

3. **Use real readiness signals**
   - Required subsystems must report actual successful startup.
   - Do not use timing to infer readiness.

4. **Keep runtime execution separate from readiness**
   - A long-lived task is still needed for the forever-running server work.
   - But readiness must not be inferred from arbitrary delays.

5. **Treat Docker as optional**
   - Docker failure/unavailability should not block startup.
   - Required runtime pieces are the main server, OTel server, and Bonjour.

## Proposed shape of `WendyAgent`

Keep `WendyAgent` as a single type, but simplify it to explicit stored runtime state and a few private startup helpers.

### Suggested stored properties

The exact types depend on the final server-starting API, but conceptually `WendyAgent` should own:

- `status`
- required runtime handles/tasks for:
  - main gRPC server
  - OTel server
  - Bonjour registration
- a monitor task for unexpected runtime termination
- any shutdown coordination state needed

Example conceptual state:

```swift
@MainActor
final class WendyAgent {
    private(set) var status: WendyAgentStatus = .stopped

    private var mainServerTask: Task<Void, Error>?
    private var otelServerTask: Task<Void, Error>?
    private var bonjourRegistration: BonjourRegistration?
    private var monitorTask: Task<Void, Never>?
}
```

The concrete representation may differ, but the idea is:
- store the actual started things directly
- avoid generic lifecycle abstractions for now

## Proposed startup flow

`start()` should read top-to-bottom, sequentially.

### Desired behavior

1. return early if already started
2. set status to `.starting`
3. perform optional Docker setup
4. start the main gRPC server and wait until it is actually ready
5. start the OTel gRPC server and wait until it is actually ready
6. start Bonjour and wait until registration succeeds
7. store runtime state
8. start a monitor task for unexpected exit
9. set status to `.running`

### Conceptual shape

```swift
func start() async throws {
    guard notAlreadyRunning else { return }

    updateStatus(.starting)

    let dockerAvailable = await prepareDockerIfNeeded()

    let mainServer = try await startMainServer(dockerAvailable: dockerAvailable)
    do {
        let otelServer = try await startOTelServer()
        do {
            let bonjour = try await startBonjour()

            self.installStartedRuntime(
                mainServer: mainServer,
                otelServer: otelServer,
                bonjour: bonjour
            )
            self.startMonitorTask()
            self.updateStatus(.running)
        } catch {
            await rollback(mainServer: mainServer, otelServer: otelServer)
            throw error
        }
    } catch {
        await rollback(mainServer: mainServer)
        throw error
    }
}
```

The actual rollback mechanics depend on the server handle types, but startup should remain obviously sequential.

## Proposed shutdown flow

Shutdown should also be explicit and deterministic.

### Desired behavior

1. if not running, return
2. request shutdown of started subsystems
3. wait for all long-lived tasks to stop
4. clear stored runtime state
5. set status to `.stopped`

### Conceptual shape

```swift
func stop() async {
    guard hasRunningState else { return }

    let runtime = captureRuntimeStateAndClearStoredProperties()

    await runtime.bonjour.stop()
    await runtime.mainServer.stop()
    await runtime.otelServer.stop()

    await runtime.mainServer.waitUntilStopped()
    await runtime.otelServer.waitUntilStopped()
    await runtime.monitorTask?.value

    updateStatus(.stopped)
}
```

Again, the exact API depends on the final server/bind layer.

## Required helper methods

Keep the helpers concrete and private to `WendyAgent`.

Suggested helpers:

- `prepareDockerIfNeeded() async -> Bool`
- `startMainServer(dockerAvailable:) async throws -> <main server runtime state>`
- `startOTelServer() async throws -> <otel runtime state>`
- `startBonjour() async throws -> <bonjour runtime state>`
- `startMonitorTask()`
- `rollback(...) async`
- `clearRuntimeState()`

Each `start...` helper must follow this contract:

> It returns only when that subsystem is actually ready.

That is the key to making `.starting -> .running` deterministic.

## Readiness rules

### Optional component

#### Docker

Docker remains optional.

Its startup helper should:
- probe Docker availability
- try to ensure the local registry if available
- log failures/warnings
- return whether Docker-backed Linux container support is enabled

If Docker is unavailable or fails startup:
- agent startup continues
- Linux container support is disabled

### Required components

#### Main Wendy Agent gRPC server

Must report readiness only after it is actually bound and listening.

#### Local OpenTelemetry gRPC server

Must report readiness only after it is actually bound and listening.

#### Bonjour advertiser

Must report readiness only after `DNSServiceRegister` succeeds.

Only after all three are ready should `WendyAgent` transition to `.running`.

## Important implementation note: server APIs

This refactor depends on having a real readiness signal for the gRPC servers.

The current `ServiceGroup.run()` model is convenient for lifetime management, but not ideal for deterministic startup readiness because it is a long-lived run loop rather than a start-and-report-ready API.

### Likely implementation direction

Investigate moving server startup one level lower so that `WendyAgent` can:

- start each server explicitly
- know when the bind/listen step succeeded
- keep the resulting long-lived task/handle for later shutdown and monitoring

If the existing `GRPCServer`/transport APIs expose a clean readiness point, use that.
If not, introduce small private helpers around the lower-level server startup APIs instead of building a generic abstraction.

## Status model

For now, keep the existing status model unless there is a separate decision to simplify it later.

However, the intended transition for normal startup should be:

- `.stopped` / `.idle`
- `.starting`
- `.running`

And for normal shutdown:

- `.running`
- `.stopped`

The important behavioral rule is:
- `.running` must only mean required subsystems are actually ready

## Logging guidance

Keep logging practical and concise.

Recommended to keep:
- start of agent startup
- Docker unavailable / Docker startup warning
- successful transition to running
- unexpected runtime stop / failure
- shutdown completion if useful

Avoid very noisy startup-stage logs unless needed temporarily during implementation.

## Failure handling

### During startup

- If a required subsystem fails to start:
  - unwind already-started required subsystems
  - clear partial state
  - leave status in a non-running state
  - surface the error to the caller

### During runtime

- A monitor task should observe long-lived runtime tasks.
- If a required runtime task exits unexpectedly:
  - clean up stored runtime state
  - transition status away from `.running`
  - log the failure clearly

## Why not introduce `WendyAgentComponent` right now

A component abstraction may still be reasonable later, but right now it adds indirection before solving the real problem.

The real problem is lack of explicit deterministic startup sequencing and readiness.

Since there are only a few top-level lifecycle responsibilities, explicit code in `WendyAgent` is simpler and easier to reason about than introducing:

- component protocols
- component handles
- consuming lifecycle APIs
- extra abstraction boundaries

If startup later expands significantly, a component model can be revisited.

## Suggested implementation steps for the follow-up session

1. **Audit the current server startup APIs**
   - Determine how to get a real readiness signal for:
     - main gRPC server
     - OTel gRPC server
   - Prefer concrete private helpers over generic abstraction.

2. **Refactor `WendyAgent.start()`**
   - Keep it fully sequential.
   - Remove timing/probe-based readiness logic.
   - Transition to `.running` only after required startup helpers succeed.

3. **Refactor `WendyAgent.stop()`**
   - Make shutdown explicit and deterministic.
   - Shut down started pieces in a predictable order.
   - Wait for long-lived tasks to terminate cleanly.

4. **Make runtime state explicit**
   - Store only the concrete started things needed for shutdown and monitoring.
   - Remove unnecessary generic runtime bookkeeping if possible.

5. **Simplify logs**
   - Keep essential logs.
   - Remove debug-stage noise unless still actively needed.

6. **Add tests if practical**
   - status reaches `.running` only after startup completes
   - Docker unavailability does not block startup
   - required subsystem startup failure prevents `.running`
   - stopping transitions back to `.stopped`

## Open questions for the follow-up session

1. What is the cleanest lower-level API available for deterministic gRPC server readiness?
2. Should startup failure leave status as `.stopped` or `.failed(...)`?
3. Should `.idle` remain in the status model, or should the initial state become `.stopped`?
4. Is there a small concrete runtime-state struct worth introducing inside `WendyAgent.swift` purely for organization, without becoming a public abstraction?

## Success criteria

The refactor is successful if:

- `WendyAgent.start()` reads as simple sequential startup logic
- `.starting` remains visible during real startup work
- `.running` is set only after required subsystems are truly ready
- no timing-based readiness logic remains
- Docker remains optional
- shutdown is explicit and predictable
- the code is easier to understand than the current `ServiceGroup`-driven startup path
