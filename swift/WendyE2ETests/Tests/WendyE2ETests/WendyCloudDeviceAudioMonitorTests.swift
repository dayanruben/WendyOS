import Testing
import WendyE2ETesting

@Suite
struct `'wendy cloud device audio monitor'` {
    let scenario = CLIAndAgentScenario()

    @Test
    func `prints command help`() async throws {
        try await self.scenario.run(authenticated: false) { cli, _ in
            try await cli.sh("wendy cloud device audio monitor --help") { result in
                #expect(result.status.isSuccess)
                #expect(result.stdout.contains("Real-time VU meter for audio levels"))
                #expect(result.stdout.contains("wendy cloud device audio monitor [flags]"))
                #expect(result.stdout.contains("--id"))
                #expect(result.stdout.contains("--rate"))
                #expect(result.stdout.contains("--device"))
                #expect(result.stderr == "")
            }
        }
    }

    @Test(
        .disabled(
            "WDY-1949/WDY-1952: explicit cloud-target monitoring needs a seeded managed agent and simulated audio capability without physical hardware."
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
            try await cli.sh("wendy cloud device audio monitor --device target --json") { result in
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
            "WDY-1952: VU rendering needs seeded managed-agent level frames plus controlled terminal output."
        )
    )
    func `renders live audio levels`() async throws {}

    @Test(
        .disabled(
            "WDY-1956: semantic audio parameter ranges are not validated locally before target connection/RPC."
        )
    )
    func `validates monitor parameters before streaming`() async throws {}

    @Test(
        .disabled(
            "WDY-1952: cancellation cleanup needs seeded streaming RPC state and harness process control."
        )
    )
    func `shuts down cleanly on cancellation`() async throws {}

    @Test
    func `rejects undocumented flags`() async throws {
        try await self.scenario.run(authenticated: false) { cli, _ in
            try await cli.sh("wendy cloud device audio monitor --bogus") { result in
                #expect(result.status.isFailure)
                #expect(result.stdout == "")
                #expect(result.stderr.contains("unknown flag"))
            }
        }
    }

    @Test(
        .disabled(
            "WDY-1934: 'wendy cloud device audio monitor' silently accepts positional arguments because the leaf command has no cobra.NoArgs validator."
        )
    )
    func `rejects undocumented positional arguments`() async throws {}
}
