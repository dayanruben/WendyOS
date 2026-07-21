import Testing
import WendyE2ETesting

@Suite
struct `'wendy device telemetry-stream'` {
    let scenario = CLIAndAgentScenario()

    @Test
    func `prints command help`() async throws {
        try await self.scenario.run(authenticated: false) { cli, _ in
            try await cli.sh("wendy device telemetry-stream --help") { result in
                #expect(result.status.isSuccess)
                #expect(
                    result.stdout.contains("Stream telemetry data (logs, metrics, traces) as JSONL")
                )
                #expect(result.stdout.contains("wendy device telemetry-stream [flags]"))
                #expect(result.stdout.contains("--app"))
                #expect(result.stdout.contains("--service"))
                #expect(result.stdout.contains("--logs"))
                #expect(result.stdout.contains("--metrics"))
                #expect(result.stdout.contains("--traces"))
                #expect(result.stdout.contains("--device"))
                #expect(result.stderr == "")
            }
        }
    }

    @Test(
        .disabled(
            "WDY-1952: explicit-target telemetry needs a seeded managed agent with logs, metrics, and traces."
        )
    )
    func `uses explicit device selection without prompting`() async throws {}

    @Test
    func `reports missing device selection in non-interactive mode`() async throws {
        try await self.scenario.run(authenticated: false) { cli, _ in
            try await cli.sh("wendy device telemetry-stream --logs --json") { result in
                #expect(result.status.isFailure)
                #expect(result.stdout == "")
                #expect(result.stderr.contains("no device specified"))
                #expect(!result.stderr.contains("Select a device"))
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
            "WDY-1952: JSONL framing needs seeded logs, metrics, and traces plus bounded stream control."
        )
    )
    func `streams telemetry as JSONL`() async throws {}

    @Test(
        .disabled(
            "WDY-1952: telemetry filtering needs seeded neighboring app/service records across all signal types."
        )
    )
    func `applies telemetry filters`() async throws {}

    @Test(
        .disabled(
            "WDY-1952: telemetry cancellation cleanup needs seeded streams and harness process control."
        )
    )
    func `shuts down cleanly on cancellation`() async throws {}

    @Test
    func `rejects undocumented flags`() async throws {
        try await self.scenario.run(authenticated: false) { cli, _ in
            try await cli.sh("wendy device telemetry-stream --bogus") { result in
                #expect(result.status.isFailure)
                #expect(result.stdout == "")
                #expect(result.stderr.contains("unknown flag"))
            }
        }
    }

    @Test(
        .disabled(
            "WDY-1934: 'wendy device telemetry-stream' silently accepts positional arguments because the leaf command has no cobra.NoArgs validator."
        )
    )
    func `rejects undocumented positional arguments`() async throws {}
}
