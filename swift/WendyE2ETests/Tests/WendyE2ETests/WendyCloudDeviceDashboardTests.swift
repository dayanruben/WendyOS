import Testing
import WendyE2ETesting

@Suite
struct `'wendy cloud device dashboard'` {
    let scenario = CLIAndAgentScenario()

    @Test
    func `prints command help`() async throws {
        try await self.scenario.run(authenticated: false) { cli, _ in
            try await cli.sh("wendy cloud device dashboard --help") { result in
                #expect(result.status.isSuccess)
                #expect(
                    result.stdout.contains("Live dashboard showing metrics and logs from a device")
                )
                #expect(result.stdout.contains("wendy cloud device dashboard [flags]"))
                #expect(result.stdout.contains("--app"))
                #expect(result.stdout.contains("--device"))
                #expect(result.stderr == "")
            }
        }
    }

    @Test(
        .disabled(
            "WDY-1949/WDY-1952: explicit cloud-target dashboard startup needs a seeded managed agent with telemetry state."
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
            try await cli.sh("wendy cloud device dashboard --device target --app Example") {
                result in
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
            "WDY-1952: live dashboard rendering needs seeded metrics/log streams and scripted terminal control."
        )
    )
    func `shows a live device dashboard`() async throws {}

    @Test(
        .disabled(
            "WDY-1952: terminal capability behavior needs a seeded target plus controlled TTY/non-TTY execution."
        )
    )
    func `reports non-interactive dashboard limitations`() async throws {}

    @Test(
        .disabled(
            "WDY-1952: dashboard cancellation cleanup needs seeded streams and harness process control."
        )
    )
    func `shuts down cleanly on cancellation`() async throws {}

    @Test
    func `rejects undocumented flags`() async throws {
        try await self.scenario.run(authenticated: false) { cli, _ in
            try await cli.sh("wendy cloud device dashboard --bogus") { result in
                #expect(result.status.isFailure)
                #expect(result.stdout == "")
                #expect(result.stderr.contains("unknown flag"))
            }
        }
    }

    @Test(
        .disabled(
            "WDY-1934: 'wendy cloud device dashboard' silently accepts positional arguments because the leaf command has no cobra.NoArgs validator."
        )
    )
    func `rejects undocumented positional arguments`() async throws {}
}
