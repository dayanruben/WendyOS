import Testing
import WendyE2ETesting

@Suite
struct `'wendy cloud device logs'` {
    let scenario = CLIAndAgentScenario()

    @Test
    func `prints command help`() async throws {
        try await self.scenario.run(authenticated: false) { cli, _ in
            try await cli.sh("wendy cloud device logs --help") { result in
                #expect(result.status.isSuccess)
                #expect(result.stdout.contains("Stream logs from containers on the device"))
                #expect(result.stdout.contains("wendy cloud device logs [app] [flags]"))
                #expect(result.stdout.contains("--app"))
                #expect(result.stdout.contains("--service"))
                #expect(result.stdout.contains("--level"))
                #expect(result.stdout.contains("--tail"))
                #expect(result.stdout.contains("--device"))
                #expect(result.stderr == "")
            }
        }
    }

    @Test(
        .disabled(
            "WDY-1949/WDY-1952: explicit cloud-target log streaming needs a seeded managed agent with telemetry state."
        )
    )
    func `uses explicit device selection without prompting`() async throws {}

    @Test(
        .disabled(
            "WDY-1949: missing cloud-device selection can only be observed after injecting valid isolated auth."
        )
    )
    func `reports missing device selection in non-interactive mode`() async throws {}

    @Test
    func `requires cloud authentication before opening a tunnel`() async throws {
        try await self.scenario.run(authenticated: false) { cli, _ in
            try await cli.sh("wendy cloud device logs Example --device target --json") { result in
                #expect(result.status.isFailure)
                #expect(result.stdout == "")
                #expect(result.stderr.contains("not logged in"))
                #expect(result.stderr.contains("wendy auth login"))
            }
        }
    }

    @Test(
        .disabled(
            "WDY-1952: connection and incompatible-RPC failures need controllable seeded managed-agent responses."
        )
    )
    func `reports unreachable devices without partial success`() async throws {}

    @Test(
        .disabled(
            "WDY-1952: filtered log streaming needs seeded managed-agent container and telemetry records."
        )
    )
    func `streams device logs`() async throws {}

    @Test(
        .disabled(
            "WDY-1952: JSON log framing needs seeded managed-agent telemetry records and bounded stream control."
        )
    )
    func `prints structured log entries in JSON mode`() async throws {}

    @Test(
        .disabled(
            "WDY-1952: log cancellation cleanup needs a seeded stream and harness process control."
        )
    )
    func `shuts down cleanly on cancellation`() async throws {}

    @Test
    func `rejects undocumented flags`() async throws {
        try await self.scenario.run(authenticated: false) { cli, _ in
            try await cli.sh("wendy cloud device logs --bogus") { result in
                #expect(result.status.isFailure)
                #expect(result.stdout == "")
                #expect(result.stderr.contains("unknown flag"))
            }
        }
    }

    @Test
    func `rejects extra positional arguments`() async throws {
        try await self.scenario.run(authenticated: false) { cli, _ in
            try await cli.sh("wendy cloud device logs one two") { result in
                #expect(result.status.isFailure)
                #expect(result.stdout == "")
                #expect(result.stderr.contains("accepts at most 1 arg(s), received 2"))
            }
        }
    }
}
