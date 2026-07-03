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

    /**
     Builds and runs a Python fixture that calls the kernel-module and kexec
     syscalls. The app passes only when the default seccomp profile denies
     them with `EPERM` — these are host-escape primitives a normal application
     container never needs.
     */
    @Test
    func `python-no-kexec-module`() async throws {
        try await self.runFixture("python-no-kexec-module")
    }

    /**
     Builds and runs a Python fixture with the `host-admin` network mode. The
     app verifies that `CAP_NET_ADMIN` is present in its effective capability
     set, since `host-admin` is the explicit opt-in for reconfiguring host
     networking.
     */
    @Test
    func `python-network-host-admin`() async throws {
        try await self.runFixture("python-network-host-admin")
    }

    /**
     Builds and runs a Python fixture with plain `host` networking. The app
     passes only when `CAP_NET_ADMIN` is absent, guarding against a regression
     that re-couples the capability to plain host networking.
     */
    @Test
    func `python-no-net-admin`() async throws {
        try await self.runFixture("python-no-net-admin")
    }

    /**
     Builds and runs a Python fixture that declares app-level resource limits
     in `wendy.json`. The app reads its own cgroup and verifies the memory,
     CPU, and pids ceilings were applied end-to-end.
     */
    @Test
    func `python-resources`() async throws {
        try await self.runFixture("python-resources")
    }

    /**
     Builds and runs a Python fixture with a top-level `serviceName` in
     `wendy.json`. The app verifies the `WENDY_HOSTNAME`, `WENDY_APP_GROUP`,
     and `WENDY_APP_ID` environment variables follow the container-naming
     convention.
     */
    @Test
    func `python-servicename`() async throws {
        try await self.runFixture("python-servicename")
    }

    // MARK: - Multi-Service Fixtures

    /**
     Deploys the legacy Python multi-service fixture. The full deployment and
     `--service api` deployment must succeed, while `--service ghost` must fail
     with a diagnostic that mentions the unknown service name.
     */
    @Test
    func `python-multiservice`() async throws {
        // The legacy fixtures pre-date CLI auth and `go/scripts/test-ci.sh`
        // runs them unauthenticated; authenticated variants are tracked as a
        // migration follow-up.
        try await self.scenario.run(authenticated: false) { cli, agent in
            let device = agent.machine.address
            let fixture = try Self.fixturePath("python-multiservice")

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

            let command = try Self.wendyRunCommand(
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

    /**
     Deploys the legacy multi-service resource-limit fixture. The `db` service
     inherits the app-level memory limit while `api` overrides it; each service
     asserts its own cgroup limits and logs `<svc>: PASS`. The test reads the
     device logs for both PASS lines, mirroring `go/scripts/test-ci.sh`, and
     removes the app afterwards.
     */
    @Test
    func `python-multiservice-resources`() async throws {
        // The legacy fixtures pre-date CLI auth and `go/scripts/test-ci.sh`
        // runs them unauthenticated; authenticated variants are tracked as a
        // migration follow-up.
        try await self.scenario.run(authenticated: false) { cli, agent in
            let device = agent.machine.address
            let fixture = try Self.fixturePath("python-multiservice-resources")
            let appID = "sh.wendy.ci.python-multiservice-resources"

            try await Self.assertWendyRunSucceeds(
                on: cli,
                device: device,
                fixture: fixture,
                extraArguments: ["--deploy"]
            )

            let logs = try Self.deviceLogsCommand(device: device, appID: appID)
            try await cli.sh(posix: logs.posix, power: logs.power) { result in
                let output = result.normalizedStdout

                #expect(!output.localizedCaseInsensitiveContains("FAIL"))
                #expect(output.contains("db: PASS"))
                #expect(output.contains("api: PASS"))
            }

            try await Self.removeApp(on: cli, device: device, appID: appID)
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

    /**
     Deploys the legacy Compose fixture that pairs a locally built service
     with a public-image companion. The command runs detached, matching
     `go/scripts/test-ci.sh`.
     */
    @Test
    func `compose-companion`() async throws {
        try await self.runFixture("compose-companion", extraArguments: ["--detach"])
    }

    // MARK: - Device Monitoring

    /**
     Deploys a long-running fixture, then verifies `wendy device top --json`
     reports a host snapshot with non-zero CPU and memory totals and lists the
     deployed container. The app is removed afterwards.
     */
    @Test
    func `python-device-top`() async throws {
        // The legacy fixtures pre-date CLI auth and `go/scripts/test-ci.sh`
        // runs them unauthenticated; authenticated variants are tracked as a
        // migration follow-up.
        try await self.scenario.run(authenticated: false) { cli, agent in
            let device = agent.machine.address
            let fixture = try Self.fixturePath("python-device-top")
            let appID = "sh.wendy.ci.python-device-top"

            try await Self.assertWendyRunSucceeds(
                on: cli,
                device: device,
                fixture: fixture,
                extraArguments: ["--detach"]
            )

            let top = try Self.wendyCommand([
                "device", "top",
                "--device", Self.validatedHost(device),
                "--json",
            ])
            try await cli.sh(posix: top.posix, power: top.power) { result in
                #expect(result.status.isSuccess)

                let stdout = result.stdout.trimmingCharacters(in: .whitespacesAndNewlines)
                guard let data = stdout.data(using: .utf8),
                    let json = try? JSONSerialization.jsonObject(with: data) as? [String: Any]
                else {
                    Issue.record("'wendy device top --json' did not print a JSON object")
                    return
                }

                let host = json["host"] as? [String: Any]
                #expect((host?["cpuCount"] as? Int ?? 0) > 0)
                #expect((host?["memTotalBytes"] as? Int ?? 0) > 0)

                let containers = json["containers"] as? [[String: Any]] ?? []
                #expect(containers.contains { ($0["name"] as? String) == appID })
            }

            try await Self.removeApp(on: cli, device: device, appID: appID)
        }
    }

    // MARK: - Agent Exposure Policy

    /**
     Verifies that the agent OTEL receivers are bound to localhost only. Ports
     4317 and 4318 must not be reachable from the CLI side over the network.
     */
    @Test
    func `otel-localhost-only`() async throws {
        // The legacy fixtures pre-date CLI auth and `go/scripts/test-ci.sh`
        // runs them unauthenticated; authenticated variants are tracked as a
        // migration follow-up.
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
        // The legacy fixtures pre-date CLI auth and `go/scripts/test-ci.sh`
        // runs them unauthenticated; authenticated variants are tracked as a
        // migration follow-up.
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
        let command = try Self.wendyRunCommand(
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
        let host = try Self.validatedHost(host)
        let port = try Self.validatedPort(port)

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
        let command = try Self.wendyCommand([
            "--device", Self.validatedHost(device),
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

    /**
     Reads recent device logs for an app with a bounded wall-clock wait,
     because `wendy device logs` streams and never exits on its own. This
     mirrors the background-reader pattern in `go/scripts/test-ci.sh`.
     */
    private static func deviceLogsCommand(device: String, appID: String) throws -> ShellCommand {
        let logs = try Self.wendyCommand([
            "device", "logs",
            "--device", Self.validatedHost(device),
            "--app", Self.validatedAppID(appID),
            "--tail", "50",
        ])
        return ShellCommand(
            posix: """
                logfile=$(mktemp)
                \(logs.posix) >"$logfile" 2>&1 &
                logs_pid=$!
                sleep 8
                kill "$logs_pid" 2>/dev/null || true
                wait "$logs_pid" 2>/dev/null || true
                cat "$logfile"
                rm -f "$logfile"
                """,
            power: """
                $job = Start-Job -ScriptBlock { \(logs.power) 2>&1 | Out-String }
                $null = Wait-Job -Job $job -Timeout 8
                Stop-Job -Job $job
                Receive-Job -Job $job
                Remove-Job -Job $job -Force
                """
        )
    }

    private static func removeApp(
        on cli: WendyE2ESession,
        device: String,
        appID: String
    ) async throws {
        let command = try Self.wendyCommand([
            "device", "apps", "remove", Self.validatedAppID(appID),
            "--device", Self.validatedHost(device),
            "--force", "--cleanup",
        ])
        // Cleanup is best-effort, mirroring `|| true` in the shell driver.
        try await cli.sh(posix: command.posix, power: command.power) { _ in }
    }

    private static func recordSkip(on cli: WendyE2ESession, _ reason: String) async throws {
        try Self.validateShellArgument(reason)
        try await cli.sh(
            posix: "printf '%s\\n' \(Self.shellQuote("SKIP: \(reason)"))",
            power: "Write-Output \(Self.powerShellQuote("SKIP: \(reason)"))"
        )
    }

    private static func wendyRunCommand(
        device: String,
        fixture: String,
        extraArguments: [String] = []
    ) throws -> ShellCommand {
        try Self.wendyCommand(
            [
                "run",
                "--device", Self.validatedHost(device),
                "--prefix", fixture,
            ] + extraArguments.map(Self.validatedRunArgument))
    }

    private static func wendyCommand(_ arguments: [String]) throws -> ShellCommand {
        for argument in arguments {
            try Self.validateShellArgument(argument)
        }
        return ShellCommand(
            posix: (["wendy"] + arguments.map(Self.shellQuote)).joined(separator: " "),
            power: (["wendy"] + arguments.map(Self.powerShellQuote)).joined(separator: " ")
        )
    }

    private static func fixturePath(_ name: String) throws -> String {
        try Self.validatedFixturePath(
            Self.path(
                Self.validatedRepositoryRoot(Self.repositoryRootPathOnCLIMachine),
                ".github", "ci-tests",
                Self.validatedFixtureName(name)
            ))
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

    /// Joins path components onto a validated root. Components must be single
    /// directory names — separators and traversal components are rejected so
    /// a refactor that sources them from variables cannot escape the root.
    private static func path(_ first: String, _ rest: String...) throws -> String {
        let separator = first.contains("\\") && !first.contains("/") ? "\\" : "/"
        return try rest.reduce(first) { path, component in
            guard component != "..", component != ".",
                !component.contains("/"), !component.contains("\\")
            else {
                throw ShellSafetyError("path component contains unsupported characters")
            }
            return path.hasSuffix("/") || path.hasSuffix("\\")
                ? "\(path)\(component)" : "\(path)\(separator)\(component)"
        }
    }

    // MARK: - Shell Safety

    /**
     Values interpolated into remote shell commands are validated against
     strict allowlists before they are quoted. Validation is the primary
     defense; the single-quote wrappers below are only a second layer.
     */

    /// Accepts hostnames, IPv4, and IPv6 addresses (including bracketed forms
    /// and zone indices). Anything else — in particular whitespace, quotes,
    /// and shell metacharacters — is rejected before command construction.
    /// The original scalars are validated, never a case-folded copy, so the
    /// returned value is exactly what was checked.
    private static func validatedHost(_ value: String) throws -> String {
        let allowed = CharacterSet(
            charactersIn: "abcdefghijklmnopqrstuvwxyz"
                + "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
                + "0123456789.:%-[]"
        )
        guard !value.isEmpty, value.unicodeScalars.allSatisfy(allowed.contains) else {
            throw ShellSafetyError("device address contains unsupported characters")
        }
        return value
    }

    private static func validatedPort(_ port: Int) throws -> Int {
        guard (1...65535).contains(port) else {
            throw ShellSafetyError("port \(port) is out of range")
        }
        return port
    }

    /// Extra `wendy run` arguments are flags and service names. Everything in
    /// this suite is a string literal, but the allowlist keeps that true for
    /// future callers: only lowercase alphanumerics and hyphens survive.
    private static func validatedRunArgument(_ value: String) throws -> String {
        let allowed = CharacterSet(charactersIn: "abcdefghijklmnopqrstuvwxyz0123456789-")
        guard !value.isEmpty, value.unicodeScalars.allSatisfy(allowed.contains) else {
            throw ShellSafetyError("run argument contains unsupported characters")
        }
        return value
    }

    /// App identifiers follow the reverse-DNS `sh.wendy.ci.<fixture>` shape:
    /// lowercase alphanumerics, dots, and hyphens only.
    private static func validatedAppID(_ value: String) throws -> String {
        let allowed = CharacterSet(charactersIn: "abcdefghijklmnopqrstuvwxyz0123456789.-")
        guard !value.isEmpty, value.unicodeScalars.allSatisfy(allowed.contains) else {
            throw ShellSafetyError("app identifier contains unsupported characters")
        }
        return value
    }

    /// Every component of a fixture path is validated individually, but the
    /// joined result is held to the same character allowlist as the root so
    /// the full string that reaches the shell is allowlisted end to end.
    private static func validatedFixturePath(_ path: String) throws -> String {
        let allowed = CharacterSet(
            charactersIn: "abcdefghijklmnopqrstuvwxyz"
                + "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
                + "0123456789._:/\\-"
        )
        guard path.unicodeScalars.allSatisfy(allowed.contains) else {
            throw ShellSafetyError("fixture path contains unsupported characters")
        }
        return path
    }

    /// Fixture names are directory names under `.github/ci-tests` and must
    /// never introduce separators or traversal components.
    private static func validatedFixtureName(_ name: String) throws -> String {
        let allowed = CharacterSet(charactersIn: "abcdefghijklmnopqrstuvwxyz0123456789-")
        guard !name.isEmpty, name.unicodeScalars.allSatisfy(allowed.contains) else {
            throw ShellSafetyError("fixture name contains unsupported characters")
        }
        return name
    }

    /// The repository root comes from `WENDY_E2E_CLI_REPO_DIR` or the
    /// compile-time source location. It must be an absolute path without
    /// traversal components so fixture paths cannot escape the repository,
    /// and it is restricted to characters expected in filesystem paths —
    /// quotes and shell metacharacters are rejected outright rather than
    /// left for the quoting layer to neutralize.
    private static func validatedRepositoryRoot(_ path: String) throws -> String {
        let isPosixAbsolute = path.hasPrefix("/")
        let isWindowsAbsolute = path.count >= 3
            && path[path.index(path.startIndex, offsetBy: 1)] == ":"
            && (path.hasPrefix("\(path.first!):\\") || path.hasPrefix("\(path.first!):/"))
        guard isPosixAbsolute || isWindowsAbsolute else {
            throw ShellSafetyError("repository root must be an absolute path")
        }

        let components = path.split(whereSeparator: { $0 == "/" || $0 == "\\" })
        guard !components.contains("..") else {
            throw ShellSafetyError("repository root must not contain traversal components")
        }

        let allowed = CharacterSet(
            charactersIn: "abcdefghijklmnopqrstuvwxyz"
                + "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
                + "0123456789._:/\\-"
        )
        guard path.unicodeScalars.allSatisfy(allowed.contains) else {
            throw ShellSafetyError(
                "repository root contains unsupported characters (spaces are not"
                    + " supported; set WENDY_E2E_CLI_REPO_DIR to a space-free checkout)"
            )
        }
        return path
    }

    /// Every argument is single-quoted for both shells, which neutralizes
    /// spaces, quotes, and expansion. Control and format characters (newlines
    /// and bidirectional overrides above all) are the known bypass vectors
    /// for quoted strings, so they are rejected outright.
    private static func validateShellArgument(_ value: String) throws {
        let hasUnsafeCharacters = value.unicodeScalars.contains { scalar in
            scalar.properties.generalCategory == .control
                || scalar.properties.generalCategory == .format
        }
        guard !hasUnsafeCharacters else {
            throw ShellSafetyError("shell argument contains control or format characters")
        }
    }

    private static func shellQuote(_ value: String) -> String {
        "'" + value.replacingOccurrences(of: "'", with: "'\\''") + "'"
    }

    private static func powerShellQuote(_ value: String) -> String {
        "'" + value.replacingOccurrences(of: "'", with: "''") + "'"
    }

    private struct ShellSafetyError: Error, CustomStringConvertible {
        let description: String

        init(_ description: String) {
            self.description = description
        }
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
