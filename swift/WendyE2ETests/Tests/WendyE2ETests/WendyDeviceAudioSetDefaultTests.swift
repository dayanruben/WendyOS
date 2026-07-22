import Testing
import WendyE2ETesting

@Suite
struct `'wendy device audio set-default'` {
    let scenario = CLIAndAgentScenario()

    @Test
    func `prints command help`() async throws {
        try await self.scenario.run(authenticated: false) { cli, _ in
            try await cli.sh("wendy device audio set-default --help") { result in
                #expect(result.status.isSuccess)
                #expect(result.stdout.contains("Set the default audio device"))
                #expect(result.stdout.contains("wendy device audio set-default [flags]"))
                #expect(result.stdout.contains("--id"))
                #expect(result.stdout.contains("--device"))
                #expect(result.stderr == "")
            }
        }
    }

    @Test(
        .disabled(
            "WDY-1952: explicit-target mutation needs seeded managed-agent audio state without a physical device."
        )
    )
    func `uses explicit device selection without prompting`() async throws {}

    @Test
    func `reports missing device selection in non-interactive mode`() async throws {
        try await self.scenario.run(authenticated: false) { cli, _ in
            try await cli.sh("wendy device audio set-default --id 4 --json") { result in
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
            "WDY-1952: successful default selection needs seeded managed-agent audio device state."
        )
    )
    func `sets the default audio device`() async throws {}

    @Test
    func `requires an audio device id`() async throws {
        try await self.scenario.run(authenticated: false) { cli, _ in
            try await cli.sh("wendy device audio set-default") { result in
                #expect(result.status.isFailure)
                #expect(result.stdout == "")
                #expect(result.stderr.contains("required flag(s) \"id\" not set"))
            }
        }
    }

    @Test(
        .disabled(
            "WDY-1952: rejection and previous-default preservation need seeded managed-agent audio state."
        )
    )
    func `reports unsupported devices without changing defaults`() async throws {}

    @Test
    func `rejects undocumented flags`() async throws {
        try await self.scenario.run(authenticated: false) { cli, _ in
            try await cli.sh("wendy device audio set-default --bogus") { result in
                #expect(result.status.isFailure)
                #expect(result.stdout == "")
                #expect(result.stderr.contains("unknown flag"))
            }
        }
    }

    @Test(
        .disabled(
            "WDY-1934: 'wendy device audio set-default' silently accepts positional arguments because the leaf command has no cobra.NoArgs validator."
        )
    )
    func `rejects undocumented positional arguments`() async throws {}
}
