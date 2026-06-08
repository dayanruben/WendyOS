import Foundation
import Testing
import WendyE2ETesting

private let legacyIntegrationTestsEnabled = LegacyIntegrationTestEnvironment.flag(
    "WENDY_E2E_LEGACY_INTEGRATION"
)

/**
 Legacy integration tests preserve parity with `go/scripts/test-ci.sh`.

 These tests deploy the existing `.github/ci-tests` fixtures against a real
 WendyOS device. They are intentionally separated from the CLI spec suites so
 the shell-driven app integration coverage can move into the Swift E2E harness
 without redefining the formal command contract.

 Set `WENDY_E2E_LEGACY_INTEGRATION=true` and filter this suite to run it during
 the migration period.
 */
@Suite(.serialized, .enabled(if: legacyIntegrationTestsEnabled))
struct `legacy integration tests` {
    let scenario = CLIAndAgentScenario()

    // MARK: - Swift Fixtures

    /**
     Builds, deploys, starts, and streams the minimal Swift fixture. This
     preserves the `swift-hello` check from `go/scripts/test-ci.sh`.
     */
    @Test
    func `swift-hello`() async throws {
        try await self.runFixture("swift-hello")
    }

    /**
     Builds and runs the Swift fixture with the host-network entitlement. The
     app verifies DNS resolution and TCP connectivity from inside the container.
     */
    @Test
    func `swift-network`() async throws {
        try await self.runFixture("swift-network")
    }

    /**
     Builds and runs the Swift fixture with the Bluetooth entitlement. The app
     verifies that Bluetooth controller state or raw HCI socket access is
     visible inside the container.
     */
    @Test
    func `swift-bluetooth`() async throws {
        try await self.runFixture("swift-bluetooth")
    }

    /**
     Builds and runs the SwiftPM resource fixture. The app verifies that a
     bundled resource file is synced into the image and loaded with
     `Bundle.module` at runtime.
     */
    @Test
    func `swift-resources`() async throws {
        try await self.runFixture("swift-resources")
    }

    // MARK: - Python Fixtures

    /**
     Builds, deploys, starts, and streams the minimal Python fixture. This
     preserves the `python-hello` smoke check from `go/scripts/test-ci.sh`.
     */
    @Test
    func `python-hello`() async throws {
        try await self.runFixture("python-hello")
    }

    /**
     Builds and runs the Python fixture with the host-network entitlement. The
     app verifies outbound HTTP connectivity from inside the container.
     */
    @Test
    func `python-network`() async throws {
        try await self.runFixture("python-network")
    }

    /**
     Builds and runs the Python GPU fixture on GPU-capable devices. The app
     verifies CUDA availability through PyTorch and runs a CUDA matrix multiply.
     Devices without GPU support are recorded as skipped for shell parity.
     */
    @Test
    func `python-gpu`() async throws {
        try await self.runFixture("python-gpu", requiresGPU: true)
    }

    /**
     Builds and runs the Python ONNX Runtime GPU fixture on GPU-capable devices.
     The app verifies that `CUDAExecutionProvider` is available and performs a
     tiny GPU-backed inference.
     */
    @Test
    func `python-onnx-gpu`() async throws {
        try await self.runFixture("python-onnx-gpu", requiresGPU: true)
    }

    /**
     Builds and runs the Python fixture with the Bluetooth entitlement. The app
     verifies that Bluetooth controller state or raw HCI socket access is
     visible inside the container.
     */
    @Test
    func `python-bluetooth`() async throws {
        try await self.runFixture("python-bluetooth")
    }

    /**
     Builds and runs a Python fixture without the network entitlement. The app
     passes only when outbound network access is denied.
     */
    @Test
    func `python-no-network`() async throws {
        try await self.runFixture("python-no-network")
    }

    /**
     Builds and runs a Python fixture without the Bluetooth entitlement. The app
     passes only when Bluetooth devices and raw HCI socket access are denied.
     */
    @Test
    func `python-no-bluetooth`() async throws {
        try await self.runFixture("python-no-bluetooth")
    }

    /**
     Builds and runs a Python fixture that calls `ptrace(PTRACE_TRACEME)`. The
     app passes only when the default seccomp profile denies the syscall.
     */
    @Test
    func `python-no-ptrace`() async throws {
        try await self.runFixture("python-no-ptrace")
    }

    /**
     Builds and runs a Python fixture that calls `unshare(CLONE_NEWUSER)`. The
     app passes only when the default seccomp profile denies the syscall.
     */
    @Test
    func `python-no-unshare`() async throws {
        try await self.runFixture("python-no-unshare")
    }

    // MARK: - Multi-Service Fixtures

    /**
     Deploys the legacy Python multi-service fixture. The full deployment and
     `--service api` deployment must succeed, while `--service ghost` must fail
     with a diagnostic that mentions the unknown service name.
     */
    @Test
    func `python-multiservice`() async throws {
        try await self.scenario.run(authenticated: false) { cli, agent in
            let device = agent.machine.address
            let fixture = Self.fixturePath("python-multiservice")

            try await Self.assertWendyRunSucceeds(
                on: cli,
                device: device,
                fixture: fixture,
                extraArguments: ["--deploy"]
            )
            try await Self.assertWendyRunSucceeds(
                on: cli,
                device: device,
                fixture: fixture,
                extraArguments: ["--deploy", "--service", "api"]
            )

            let command = Self.wendyRunCommand(
                device: device,
                fixture: fixture,
                extraArguments: ["--deploy", "--service", "ghost"]
            )
            try await cli.sh(posix: command.posix, power: command.power) { result in
                let output = result.normalizedStdout + result.normalizedStderr

                #expect(result.status.isFailure)
                #expect(output.localizedCaseInsensitiveContains("ghost"))
            }
        }
    }

    // MARK: - Compose Fixtures

    /**
     Deploys the legacy Compose fixture whose services are built from local
     Dockerfiles. The command runs detached, matching `go/scripts/test-ci.sh`.
     */
    @Test
    func `compose-hello`() async throws {
        try await self.runFixture("compose-hello", extraArguments: ["--detach"])
    }

    /**
     Deploys the legacy Compose fixture whose services use public images. The
     command runs detached, matching `go/scripts/test-ci.sh`.
     */
    @Test
    func `compose-images`() async throws {
        try await self.runFixture("compose-images", extraArguments: ["--detach"])
    }

    // MARK: - Agent Exposure Policy

    /**
     Verifies that the agent OTEL receivers are bound to localhost only. Ports
     4317 and 4318 must not be reachable from the CLI side over the network.
     */
    @Test
    func `otel-localhost-only`() async throws {
        try await self.scenario.run(authenticated: false) { cli, agent in
            let device = agent.machine.address

            try await Self.assertPortIsNotReachable(on: cli, host: device, port: 4317)
            try await Self.assertPortIsNotReachable(on: cli, host: device, port: 4318)
        }
    }

    // MARK: - Private

    private func runFixture(
        _ name: String,
        extraArguments: [String] = [],
        requiresGPU: Bool = false
    ) async throws {
        try await self.scenario.run(authenticated: false) { cli, agent in
            let device = agent.machine.address
            if requiresGPU {
                let hasGPU = try await Self.deviceHasGPU(on: cli, device: device)
                guard hasGPU else {
                    try await Self.recordSkip(on: cli, "\(name): no GPU")
                    return
                }
            }

            try await Self.assertWendyRunSucceeds(
                on: cli,
                device: device,
                fixture: Self.fixturePath(name),
                extraArguments: extraArguments
            )
        }
    }

    private static func assertWendyRunSucceeds(
        on cli: WendyE2ESession,
        device: String,
        fixture: String,
        extraArguments: [String] = []
    ) async throws {
        let command = Self.wendyRunCommand(
            device: device,
            fixture: fixture,
            extraArguments: extraArguments
        )
        try await cli.sh(posix: command.posix, power: command.power) { result in
            #expect(result.status.isSuccess)
        }
    }

    private static func assertPortIsNotReachable(
        on cli: WendyE2ESession,
        host: String,
        port: Int
    ) async throws {
        try await cli.sh(
            posix: "! nc -z -w 3 \(Self.shellQuote(host)) \(port) 2>/dev/null",
            power: """
                $reachable = Test-NetConnection -ComputerName \(Self.powerShellQuote(host)) -Port \(port) -InformationLevel Quiet
                if ($reachable) { exit 1 } else { exit 0 }
                """
        ) { result in
            #expect(result.status.isSuccess)
        }
    }

    private static func deviceHasGPU(
        on cli: WendyE2ESession,
        device: String
    ) async throws -> Bool {
        let command = Self.wendyCommand([
            "--device", device,
            "device", "info",
            "--json",
        ])
        return try await cli.sh(posix: command.posix, power: command.power) { result in
            guard result.status.isSuccess,
                let data = result.stdout.data(using: .utf8),
                let json = try? JSONSerialization.jsonObject(with: data) as? [String: Any]
            else {
                return false
            }

            return json["hasGpu"] as? Bool ?? false
        }
    }

    private static func recordSkip(on cli: WendyE2ESession, _ reason: String) async throws {
        try await cli.sh(
            posix: "printf '%s\\n' \(Self.shellQuote("SKIP: \(reason)"))",
            power: "Write-Output \(Self.powerShellQuote("SKIP: \(reason)"))"
        )
    }

    private static func wendyRunCommand(
        device: String,
        fixture: String,
        extraArguments: [String] = []
    ) -> ShellCommand {
        Self.wendyCommand([
            "run",
            "--device", device,
            "--prefix", fixture,
        ] + extraArguments)
    }

    private static func wendyCommand(_ arguments: [String]) -> ShellCommand {
        ShellCommand(
            posix: (["wendy"] + arguments.map(Self.shellQuote)).joined(separator: " "),
            power: (["wendy"] + arguments.map(Self.powerShellQuote)).joined(separator: " ")
        )
    }

    private static func fixturePath(_ name: String) -> String {
        Self.path(Self.repositoryRootPathOnCLIMachine, ".github", "ci-tests", name)
    }

    private static var repositoryRootPathOnCLIMachine: String {
        WendyE2EEnvironment.cliRepoDirectory
            ?? URL(fileURLWithPath: #filePath, isDirectory: false)
                .deletingLastPathComponent()  // Tests/WendyE2ETests
                .deletingLastPathComponent()  // Tests
                .deletingLastPathComponent()  // swift/WendyE2ETests
                .deletingLastPathComponent()  // swift
                .deletingLastPathComponent()  // repository root
                .path
    }

    private static func path(_ first: String, _ rest: String...) -> String {
        let separator = first.contains("\\") && !first.contains("/") ? "\\" : "/"
        return rest.reduce(first) { path, component in
            let suffix = component.trimmingCharacters(in: CharacterSet(charactersIn: "/\\"))
            return path.hasSuffix("/") || path.hasSuffix("\\")
                ? "\(path)\(suffix)" : "\(path)\(separator)\(suffix)"
        }
    }

    private static func shellQuote(_ value: String) -> String {
        "'" + value.replacingOccurrences(of: "'", with: "'\\''") + "'"
    }

    private static func powerShellQuote(_ value: String) -> String {
        "'" + value.replacingOccurrences(of: "'", with: "''") + "'"
    }

    private struct ShellCommand: Sendable {
        let posix: String
        let power: String
    }
}

private enum LegacyIntegrationTestEnvironment {
    static func flag(_ name: String) -> Bool {
        guard let value = ProcessInfo.processInfo.environment[name]?.lowercased() else {
            return false
        }
        return ["1", "true", "yes", "on", "enabled"].contains(value)
    }
}
