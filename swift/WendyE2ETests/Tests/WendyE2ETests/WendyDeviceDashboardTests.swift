import Testing
import WendyE2ETesting

@Suite
struct `'wendy device dashboard'` {
    let scenario = CLIAndAgentScenario()

    @Test
    func `prints command help`() async throws {
        try await self.scenario.run(authenticated: false) { cli, _ in
            try await cli.sh("wendy device dashboard --help") { result in
                #expect(result.status.isSuccess)
                #expect(
                    result.stdout.contains("Live dashboard showing metrics and logs from a device")
                )
                #expect(result.stdout.contains("wendy device dashboard [flags]"))
                #expect(result.stdout.contains("--app"))
                #expect(result.stdout.contains("--device"))
                #expect(result.stderr == "")
            }
        }
    }

    @Test(
        .disabled(
            "WDY-1952: explicit-target dashboard startup needs a seeded managed agent with telemetry state."
        )
    )
    func `uses explicit device selection without prompting`() async throws {}

    @Test
    func `reports missing device selection in non-interactive mode`() async throws {
        try await self.scenario.run(authenticated: false) { cli, _ in
            try await cli.sh("wendy device dashboard --app Example") { result in
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
            try await cli.sh("wendy device dashboard --bogus") { result in
                #expect(result.status.isFailure)
                #expect(result.stdout == "")
                #expect(result.stderr.contains("unknown flag"))
            }
        }
    }

    @Test(
        .disabled(
            "WDY-1934: 'wendy device dashboard' silently accepts positional arguments because the leaf command has no cobra.NoArgs validator."
        )
    )
    func `rejects undocumented positional arguments`() async throws {}
}
