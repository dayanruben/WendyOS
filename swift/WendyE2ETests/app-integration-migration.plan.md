# App Integration Tests in Swift E2E — Migration Plan

## Context

Wendy currently has two test surfaces that are easy to confuse:

1. **App integration tests** under `.github/ci-tests/`, driven by
   `go/scripts/test-ci.sh` and the integration-test workflows.
2. **Swift E2E tests** under `swift/WendyE2ETests/`, driven by the
   `WendyE2ETesting` library and the Swift E2E workflows.

These test different layers today, but the Swift E2E harness can eventually own
execution and assertions for the app integration coverage too.

## Current app integration tests

The registered app integration tests in `go/scripts/test-ci.sh` are:

```text
swift-hello
swift-network
swift-bluetooth
swift-resources

python-hello
python-network
python-gpu
python-onnx-gpu
python-bluetooth
python-no-network
python-no-bluetooth
python-no-ptrace
python-no-unshare
python-multiservice

compose-hello
compose-images

otel-localhost-only
```

Most entries have fixture directories under `.github/ci-tests/`. The exception
is `otel-localhost-only`, which is a script-level check that verifies OTEL ports
`4317` and `4318` are not reachable from the network.

The current shell driver has several behaviors that need to be preserved during
migration:

- GPU tests are skipped when the target device is not marked as GPU-capable.
- `python-multiservice` runs three checks:
  - full multi-service deploy
  - `--service api` deploy
  - unknown `--service ghost` fails with a diagnostic mentioning `ghost`
- `compose-*` tests run detached.
- standard single-container tests run `wendy run` against their fixture prefix.
- `otel-localhost-only` checks network reachability directly instead of deploying
  an app fixture.

## Current distinction

### App integration tests

App integration tests validate deployed application behavior on devices. They
prove that real fixture apps can be built, deployed, started, isolated, and run
with the expected runtime capabilities.

They cover:

- `wendy run` build/deploy paths
- Python and Swift app fixtures
- Docker Compose style deployments
- entitlements that grant or block capabilities
- GPU and ONNX GPU access
- Bluetooth access
- network access
- ptrace and unshare restrictions
- multi-service deploy behavior
- OTEL port exposure policy

### Swift E2E tests

Swift E2E tests validate the Wendy CLI and agent command surface. They use the
real `wendy` binary, isolated CLI/agent sandboxes, command recordings, and
aggregate reports.

They cover, or are intended to cover:

- CLI command behavior
- stdout/stderr/exit status contracts
- JSON output shape
- config and filesystem side effects
- CLI-to-agent interactions
- cross-machine command execution
- artifact generation, aggregation, AI review, and reporting

## Desired direction

Use `swift/WendyE2ETests` as the single E2E harness, while keeping app fixture
directories as deployable test inputs.

In the target model:

```text
Swift E2E Tests
├── CLI/agent behavior specs
└── app integration specs
    └── deploy fixture apps and assert runtime behavior

App fixtures
└── .github/ci-tests/<fixture>
```

The fixture apps remain valuable. The shell orchestration in `go/scripts/test-ci.sh`
can be replaced once the Swift E2E specs reach parity.

## Naming cleanup

Use names that describe the test layer:

- `.github/ci-tests/` → **App Integration Tests** in prose today.
- Future fixture terminology → **App Fixtures** or **Runtime Fixtures**.
- `swift/WendyE2ETests/` → **Swift E2E Tests**.
- App-related Swift E2E suites → **App Integration Specs**.

Avoid calling both systems simply “integration tests” or “E2E tests” without a
qualifier.

Recommended docs taxonomy:

```text
Testing
├── Unit Tests
├── App Integration Tests
│   └── .github/ci-tests/ fixture apps and runtime checks
└── Swift E2E Tests
    ├── CLI/agent behavior specs
    └── app integration specs
```

Do not rename `.github/ci-tests/` or existing workflows immediately. Start with
docs language and Swift E2E coverage. Rename directories or workflow display
names only after the migration proves useful.

## Migration plan

### Phase 1 — Document the split

- Describe `.github/ci-tests/` as app integration tests or app fixtures.
- Describe `swift/WendyE2ETests/` as the Swift E2E harness for CLI/agent
  behavior and, eventually, app integration specs.
- Link the Swift E2E README from the contributor testing docs.

### Phase 2 — Add fixture path helpers

Add Swift E2E helpers for resolving repository fixture paths on the CLI machine.
The helpers should work locally and over SSH, using `WENDY_E2E_CLI_REPO_DIR`
where available.

Desired shape:

```swift
let fixture = try cli.repositoryPath(".github/ci-tests/python-hello")
```

or a higher-level helper:

```swift
let fixture = AppIntegrationFixture.pythonHello
try await fixture.deploy(using: cli, device: agent.machine.address)
```

### Phase 3 — Port stable smoke coverage first

Start with low-risk fixtures that do not require special hardware:

1. `python-hello`
2. `swift-hello`
3. `python-network`
4. `swift-network`
5. `compose-hello`

Each Swift E2E spec should assert more than “command succeeded” where practical:

- exit status
- stderr behavior
- useful stdout anchors
- absence of prompts in non-interactive runs
- resulting app/container state when easy to observe
- command recordings as evidence

### Phase 4 — Port entitlement and hardware-sensitive fixtures

Port the entitlement and hardware-sensitive coverage after the basic deploy path
is stable:

- `python-no-network`
- `python-no-bluetooth`
- `python-no-ptrace`
- `python-no-unshare`
- `python-bluetooth`
- `swift-bluetooth`
- `python-gpu`
- `python-onnx-gpu`

Model hardware capabilities explicitly so tests skip or gate deterministically.
The existing GPU skip behavior should move from shell logic into Swift E2E test
traits or helper gates.

### Phase 5 — Port multi-service and compose coverage

Port the orchestration-specific cases:

- `python-multiservice` full deploy
- `python-multiservice --service api`
- `python-multiservice --service ghost` failure contract
- `compose-images`
- remaining compose behavior

These should become behavior specs for `wendy run`, not just smoke tests.

### Phase 6 — Port OTEL exposure policy

Represent `otel-localhost-only` as a Swift E2E test that checks the agent target
from the CLI side:

```swift
// Validate and quote every interpolated value; see the shell-safety helpers
// in LegacyIntegrationTests.swift (validatedHost / shellQuote).
let host = try Self.validatedHost(agent.machine.address)
try await cli.sh("nc -z -w 3 \(Self.shellQuote(host)) 4317") { result in
    #expect(result.status.isFailure)
}
```

Use `posix` and `power` command variants where needed.

### Phase 7 — Transition CI

Once Swift E2E has parity:

1. Keep `.github/ci-tests/` as fixtures.
2. Update CI to run app integration specs through `swift/WendyE2ETests`.
3. Upload the existing Swift E2E attempt and aggregate artifacts.
4. Keep `go/scripts/test-ci.sh` as a fallback for one or two cycles.
5. Remove or reduce `go/scripts/test-ci.sh` only after the Swift E2E workflow is
   stable on the same devices.

## Example target suite shape

```swift
import Testing
import WendyE2ETesting

@Suite
struct `'wendy run app integration fixtures'` {
    let scenario = CLIAndAgentScenario()

    @Test
    func `deploys the Python hello fixture`() async throws {
        try await self.scenario.run { cli, agent in
            // Validate and quote every interpolated value; see the
            // shell-safety helpers in LegacyIntegrationTests.swift.
            let agentAddress = try Self.validatedHost(agent.machine.address)
            let fixture = try AppIntegrationFixture.pythonHello.path(on: cli)

            try await cli.sh(
                "wendy run --device \(Self.shellQuote(agentAddress)) "
                    + "--prefix \(Self.shellQuote(fixture))"
            ) { result in
                #expect(result.status.isSuccess)
                #expect(result.stderr.isEmpty)
            }
        }
    }
}
```

The actual implementation should prefer helper APIs once repeated patterns are
clear.

## Acceptance criteria for replacing the shell driver

Before retiring `go/scripts/test-ci.sh`, Swift E2E should provide:

- parity for every entry in `ALL_TESTS`
- deterministic skip/gate behavior for hardware-specific tests
- equivalent or better diagnostics for failed deploys
- coverage for negative entitlement checks
- support for local and CI target matrices
- aggregate artifacts that are easy to inspect in CI
- a documented way to run one fixture/spec locally and on CI

## Risks and mitigations

- **Long-running deploy tests may slow Swift E2E feedback.** Use filters and CI
  matrix partitioning so developers can run small subsets.
- **Hardware-specific tests can become flaky.** Encode capability gates and keep
  target metadata explicit.
- **Fixture paths differ across local and SSH machines.** Resolve paths through
  machine/repository metadata instead of hardcoding local paths.
- **The migration can obscure what is being tested.** Keep the naming split:
  app fixtures validate runtime behavior; Swift E2E owns orchestration and
  assertions.

## Open questions

- Should fixture directories remain under `.github/ci-tests/`, or move to a
  source-tree location such as `Tests/AppFixtures/` once Swift E2E owns them?
- Should app integration specs live in one suite or be grouped by command area,
  language, or fixture capability?
- Should CI keep separate workflow names for app integration specs, or fold them
  into `swift-e2e-tests.yml` with filters?
- What is the canonical source of target capability metadata, especially GPU and
  Bluetooth availability?
