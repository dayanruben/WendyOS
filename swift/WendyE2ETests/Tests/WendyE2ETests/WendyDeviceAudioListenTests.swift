import Testing
import WendyE2ETesting

@Suite
struct `'wendy device audio listen'` {
    let scenario = CLIAndAgentScenario()

    @Test
    func `prints command help`() async throws {
        try await self.scenario.run(authenticated: false) { cli, _ in
            try await cli.sh("wendy device audio listen --help") { result in
                #expect(result.status.isSuccess)
                #expect(result.stdout.contains("Stream raw audio from a device microphone"))
                #expect(result.stdout.contains("wendy device audio listen [flags]"))
                #expect(result.stdout.contains("--sample-rate"))
                #expect(result.stdout.contains("--channels"))
                #expect(result.stdout.contains("--stdout"))
                #expect(result.stdout.contains("--buffer-ms"))
                #expect(result.stdout.contains("--device"))
                #expect(result.stderr == "")
            }
        }
    }

    @Test(
        .disabled(
            "WDY-1952: explicit-target listening needs a seeded managed agent and simulated audio capability without physical hardware."
        )
    )
    func `uses explicit device selection without prompting`() async throws {}

    @Test
    func `reports missing device selection in non-interactive mode`() async throws {
        try await self.scenario.run(authenticated: false) { cli, _ in
            try await cli.sh("wendy device audio listen --stdout --json") { result in
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
            "WDY-1952: microphone streaming needs seeded managed-agent audio frames without physical hardware."
        )
    )
    func `streams microphone audio`() async throws {}

    @Test(
        .disabled(
            "WDY-1952: raw PCM routing needs seeded managed-agent audio frames and stream process control."
        )
    )
    func `writes raw PCM to stdout when requested`() async throws {}

    @Test(
        .disabled(
            "WDY-1956: semantic audio parameter ranges are not validated locally before target connection/RPC."
        )
    )
    func `validates audio parameters before streaming`() async throws {}

    @Test
    func `rejects undocumented flags`() async throws {
        try await self.scenario.run(authenticated: false) { cli, _ in
            try await cli.sh("wendy device audio listen --bogus") { result in
                #expect(result.status.isFailure)
                #expect(result.stdout == "")
                #expect(result.stderr.contains("unknown flag"))
            }
        }
    }

    @Test(
        .disabled(
            "WDY-1934: 'wendy device audio listen' silently accepts positional arguments because the leaf command has no cobra.NoArgs validator."
        )
    )
    func `rejects undocumented positional arguments`() async throws {}
}
