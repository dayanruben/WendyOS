# kb.graceful-unsupported-ux — Phase 0 implementation plan

## Objective

Execute **Phase 0: contract stabilization** for the Swift macOS agent, with a concrete focus on **graceful unsupported UX**.

At the end of this phase:

1. No unsupported Swift RPC crashes the agent with `fatalError`.
2. Unsupported Swift RPCs fail consistently with the user-facing message:
   - `Not supported on macOS yet.`
3. The Swift-generated gRPC surface is brought back in sync with `Proto/`.
4. `GetAgentVersion` returns a real version string instead of `0.0.0-dev`.
5. Tests cover the unsupported-path contract so future regressions are obvious.

---

## Scope

### In scope

- Regenerate Swift protobuf/gRPC code from `Proto/`
- Replace all unsupported RPC `fatalError("not implemented")` sites with graceful errors
- Replace current `.unimplemented` / `"... is not implemented"` container-service placeholders with the standardized unsupported behavior
- Add/centralize a reusable unsupported-RPC helper
- Update `GetAgentVersion` to return a real agent/app version
- Add tests for unsupported RPC behavior
- Add a short contributor note in the plan comments / commit context via code structure and naming

### Out of scope

- Implementing Wi‑Fi, Bluetooth, audio, hardware, update, or provisioning behavior
- Re-enabling Linux container execution on macOS
- Agent self-update design
- mTLS / provisioning
- Volume support, stats parity, dashboard polish

This phase is about **making unsupported features safe and predictable**, not implementing them.

---

## Current repo state

### Unsupported paths that currently crash

#### `swift/WendyAgentCore/Sources/WendyAgent/Services/AgentService.swift`
Current `fatalError("not implemented")` methods:

- `runContainer`
- `updateAgent`
- `listWiFiNetworks`
- `connectToWiFi`
- `getWiFiStatus`
- `disconnectWiFi`
- `listHardwareCapabilities`
- `scanBluetoothPeripherals`
- `connectBluetoothPeripheral`
- `disconnectBluetoothPeripheral`
- `forgetBluetoothPeripheral`
- `updateOS`

#### `swift/WendyAgentCore/Sources/WendyAgent/Services/AudioService.swift`
Current `fatalError("not implemented")` methods:

- `listAudioDevices`
- `setDefaultAudioDevice`
- `streamAudioLevels`
- `streamAudio`

### Unsupported paths that already return errors, but with the wrong contract

#### `swift/WendyAgentCore/Sources/WendyAgent/Services/ContainerService.swift`
Current `.unimplemented` placeholders:

- `attachContainer`
- `listVolumes`
- `removeVolume`
- `listLayers`
- `createContainerWithProgress`
- `runContainer`

Current messages are variants of `"... is not implemented"`; these should be normalized.

### Proto drift already visible

Current Swift generated service surface lags the canonical proto. For example, newer Wi‑Fi RPCs present in `Proto/wendy/agent/services/v1/wendy_agent_v1_service.proto` are not yet reflected in the checked-in Swift service implementation surface.

This means Phase 0 must start with codegen sync, not just hand-editing service files.

---

## Target contract

### Standard user-facing message
All unsupported Swift RPCs in this phase should present this exact text:

`Not supported on macOS yet.`

### Standard error code
Use `.unsupported` as the semantic contract for unsupported functionality.

Because the exact shape of GRPCCore's `RPCError` APIs may constrain this, implementation should **not** hard-code the constructor shape at every call site. Instead:

- introduce a small central helper/factory for unsupported RPC failures
- keep all call sites using that helper
- if GRPCCore does not expose `RPCError(code: .unsupported, ...)` directly, hide the compatibility detail inside the helper so the call-site contract still reads as unsupported

In other words, the codebase contract should become:

- call-site meaning = unsupported on macOS
- message = `Not supported on macOS yet.`
- transport-level representation = whatever minimal compatibility glue is required in one place

---

## Deliverables

1. **Codegen sync completed**
   - Swift stubs regenerated from current proto definitions
2. **Reusable unsupported helper added**
3. **All crashing unsupported RPC handlers converted**
4. **All placeholder `.unimplemented` container-service responses normalized**
5. **`GetAgentVersion` returns a real version string**
6. **Tests added for unsupported behavior and version contract**
7. **`swift test` passes for `WendyAgentCore`**

---

## Implementation plan

## Step 1 — Regenerate Swift gRPC/protobuf code

### Goal
Bring Swift generated code back in sync with `Proto/` before changing service implementations.

### Files / commands

- Script:
  - `swift/Scripts/GenerateProto.sh`
- Generated output roots:
  - `swift/WendyAgentCore/Sources/WendyAgentGRPC/Proto`
  - `swift/WendyAgentCore/Sources/OpenTelemetryGRPC/Proto`
  - `swift/WendyAgentCore/Sources/WendyCloudGRPC/Proto`

### Execution
From repo root or `swift/`:

```bash
cd swift
./Scripts/GenerateProto.sh
```

### Expected fallout
After regeneration, `AgentService` will likely fail to compile until newly-added RPC requirements are stubbed out. In particular, be prepared to add unsupported stubs for newer Wi‑Fi methods such as:

- `listKnownWiFiNetworks`
- `setWiFiNetworkPriority`
- `reorderKnownWiFiNetworks`
- `forgetWiFiNetwork`

### Acceptance criteria

- Generated sources are updated
- `Package.swift` still resolves and builds after service implementations are updated
- No manual edits are made inside generated files

---

## Step 2 — Introduce a centralized unsupported-RPC helper

### Goal
Stop scattering raw error construction across service code.

### Proposed addition
Add a small helper under `swift/WendyAgentCore/Sources/WendyAgent/Services/`, for example:

- `UnsupportedRPC.swift`

### Proposed API shape

```swift
import GRPCCore

enum UnsupportedRPC {
    static let message = "Not supported on macOS yet."

    static func error() -> RPCError {
        // central compatibility point
    }
}
```

Optional enhancement if helpful for debugging/logging while preserving UX:

```swift
static func error(feature: String) -> RPCError
```

But the returned `message` shown to callers should remain the standard text unless there is a strong reason otherwise.

### Design rules

- Every unsupported service path should use this helper
- No remaining `"not implemented"` strings in Swift service code
- If a compatibility shim is needed because `.unsupported` is not directly available, hide it here

### Acceptance criteria

- Exactly one canonical place defines the unsupported RPC message
- Service call sites become one-liners
- Future unsupported RPCs can reuse the helper without inventing new wording

---

## Step 3 — Convert `AgentService` unsupported methods

### Goal
Replace all `fatalError` sites in `AgentService` with graceful failures.

### File
- `swift/WendyAgentCore/Sources/WendyAgent/Services/AgentService.swift`

### Required edits
Convert each currently unsupported method from:

```swift
fatalError("not implemented")
```

to the centralized unsupported error path.

### Methods to update

- `runContainer`
- `updateAgent`
- `listWiFiNetworks`
- `connectToWiFi`
- `getWiFiStatus`
- `disconnectWiFi`
- `listHardwareCapabilities`
- `scanBluetoothPeripherals`
- `connectBluetoothPeripheral`
- `disconnectBluetoothPeripheral`
- `forgetBluetoothPeripheral`
- `updateOS`

### Also required after codegen sync
Add unsupported implementations for any newly-required protocol methods introduced by regenerated stubs.

### Acceptance criteria

- `AgentService.swift` contains no `fatalError("not implemented")`
- All unsupported methods return the standardized unsupported error behavior
- File compiles against regenerated service protocols

---

## Step 4 — Convert `AudioService` unsupported methods

### Goal
Replace all `fatalError` sites in `AudioService` with graceful failures.

### File
- `swift/WendyAgentCore/Sources/WendyAgent/Services/AudioService.swift`

### Methods to update

- `listAudioDevices`
- `setDefaultAudioDevice`
- `streamAudioLevels`
- `streamAudio`

### Acceptance criteria

- `AudioService.swift` contains no `fatalError("not implemented")`
- All methods use the centralized unsupported helper

---

## Step 5 — Normalize `ContainerService` unsupported placeholders

### Goal
Make existing unsupported container-service methods use the same Phase 0 contract.

### File
- `swift/WendyAgentCore/Sources/WendyAgent/Services/ContainerService.swift`

### Methods to update

- `attachContainer`
- `listVolumes`
- `removeVolume`
- `listLayers`
- `createContainerWithProgress`
- `runContainer`

### Required changes
Replace per-method raw `.unimplemented`/`"... is not implemented"` logic with the centralized unsupported helper.

### Important note
Do **not** change supported native/container lifecycle methods in this phase unless required by compiler fallout from codegen. This step is strictly normalization of currently unsupported paths.

### Acceptance criteria

- No `"is not implemented"` strings remain in unsupported container-service methods
- Unsupported container-service methods present the same UX contract as the rest of the service layer

---

## Step 6 — Return a real version string from `GetAgentVersion`

### Goal
Stop reporting `0.0.0-dev` for the shipped macOS agent by default.

### File
- `swift/WendyAgentCore/Sources/WendyAgent/Services/AgentService.swift`

### Current state
`getAgentVersion` currently hard-codes:

```swift
response.version = "0.0.0-dev"
```

### Implementation direction
Use a real version source from the app/package build metadata. Practical options, in preferred order:

1. **Bundle version from the macOS app** when running inside `WendyAgentMac`
2. **Injected build setting / generated constant** shared into `WendyAgentCore`
3. **Fallback to `0.0.0-dev` only in local/dev contexts** where no build metadata is available

### Recommended approach for Phase 0
Keep it simple and deterministic:

- introduce a small internal version provider in `WendyAgentCore`
- read from `Bundle.main` when available
- support a controlled fallback for tests/dev builds

Example shape:

- new file, e.g. `AgentVersion.swift`
- `static let current: String`

### Acceptance criteria

- Release/app builds no longer report `0.0.0-dev`
- Tests can still run without brittle bundle assumptions
- `GetAgentVersion` remains fast and side-effect free

---

## Step 7 — Add tests

### Goal
Lock in the Phase 0 behavior so regressions are caught immediately.

### Test target
- `swift/WendyAgentCore/Tests/WendyAgentTests`

### Recommended new test file
- `UnsupportedRPCTests.swift`

### Test strategy
Prefer **direct service invocation tests** for this phase. They are sufficient to verify:

- the method no longer crashes
- the method throws the expected unsupported error contract
- the message is exactly `Not supported on macOS yet.`

This is cheaper and more stable than spinning up a full gRPC server just to validate the Phase 0 contract.

### Tests to add

#### 1. Agent service unsupported methods
One test per representative category is sufficient if table-driven; otherwise a handful of direct tests is fine.

Minimum coverage:
- unary unsupported RPC from `AgentService`
- streaming unsupported RPC from `AgentService`

#### 2. Audio service unsupported methods
Minimum coverage:
- unary unsupported RPC
- streaming unsupported RPC

#### 3. Container service unsupported placeholders
Minimum coverage:
- one existing unsupported unary method
- one existing unsupported streaming method

#### 4. Version contract
- `GetAgentVersion` returns non-empty OS / architecture
- version is not hard-coded `0.0.0-dev` in the normal tested path, or the test explicitly validates the chosen fallback semantics if the runtime test environment cannot supply bundle metadata

### Assertion rules
Assert:

- error type is `RPCError`
- code/semantic contract is unsupported
- message text is exactly `Not supported on macOS yet.`

If the transport library forces a different lower-level code, keep the helper-based semantic check in one place and assert against that abstraction.

### Acceptance criteria

- New tests fail on current `fatalError` behavior
- New tests pass after conversion
- `swift test` passes in `swift/WendyAgentCore`

---

## File-level change list

### Expected to change

- `swift/Scripts/GenerateProto.sh` output only, not script logic
- `swift/WendyAgentCore/Sources/WendyAgent/Services/AgentService.swift`
- `swift/WendyAgentCore/Sources/WendyAgent/Services/AudioService.swift`
- `swift/WendyAgentCore/Sources/WendyAgent/Services/ContainerService.swift`
- `swift/WendyAgentCore/Sources/WendyAgent/` or `.../Services/`
  - new helper file for unsupported RPC errors
  - optional small version-provider helper file
- `swift/WendyAgentCore/Tests/WendyAgentTests/`
  - new unsupported-RPC tests
  - possibly small shared test helpers
- generated files under:
  - `swift/WendyAgentCore/Sources/WendyAgentGRPC/Proto`
  - `swift/WendyAgentCore/Sources/OpenTelemetryGRPC/Proto`
  - `swift/WendyAgentCore/Sources/WendyCloudGRPC/Proto`

### Should not change in Phase 0

- `WendyAgentMac` app UX
- provisioning implementation
- Docker/Linux-container runtime behavior
- telemetry behavior
- file sync implementation
- supported container/native lifecycle logic

---

## Recommended implementation order

1. Regenerate proto/grpc code
2. Add unsupported helper
3. Update `AgentService`
4. Update `AudioService`
5. Normalize unsupported `ContainerService` methods
6. Add real version provider and wire `GetAgentVersion`
7. Add tests
8. Run format/tests and verify no `fatalError("not implemented")` remains

This order minimizes churn and prevents rework from protocol drift.

---

## Verification checklist

### Static checks

```bash
cd swift/WendyAgentCore
swift test
```

### Search checks
From repo root:

```bash
rg -n 'fatalError\("not implemented"\)' swift/WendyAgentCore -g '!**/*.pb.swift' -g '!**/*.grpc.swift'
rg -n 'is not implemented' swift/WendyAgentCore -g '!**/*.pb.swift' -g '!**/*.grpc.swift'
```

Expected result: no hits in service implementation files for unsupported RPCs.

### Optional compile-only sanity

```bash
cd swift/WendyAgentCore
swift build
```

---

## Acceptance criteria

Phase 0 is complete when all of the following are true:

- Unsupported Swift RPCs no longer crash the agent
- Unsupported Swift RPCs consistently surface `Not supported on macOS yet.`
- Unsupported behavior is centralized behind one helper
- Swift generated code is synced with current proto definitions
- `GetAgentVersion` reports a real version source instead of unconditional `0.0.0-dev`
- Tests cover unsupported unary + streaming paths and version behavior
- `swift test` passes

---

## Open questions to resolve during implementation

1. **Does GRPCCore expose `.unsupported` directly on `RPCError` in the currently pinned version?**
   - If yes, use it inside the helper.
   - If not, implement the closest compatibility mapping inside the helper and keep the call-site API/semantics as unsupported.

2. **What is the cleanest stable version source for `WendyAgentCore`?**
   - `Bundle.main`
   - generated build constant
   - both, with fallback order

3. **Do we want the unsupported helper to log internal feature names?**
   - Nice-to-have, not required for Phase 0.

These should be settled during implementation, but they do not block writing the code structure described here.

---

## Summary

This branch should deliver a narrow but important quality bar:

- the Swift agent stops crashing on unsupported CLI paths
- unsupported behavior becomes standardized and reviewable
- proto drift is corrected
- version reporting stops misleading the CLI

That makes the next implementation phases much safer and easier to review.
