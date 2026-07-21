import Testing
import WendyE2ETesting

@Suite
struct `'wendy device audio list'` {
    let scenario = CLIAndAgentScenario()

    @Test
    func `prints command help`() async throws {
        try await self.scenario.run(authenticated: false) { cli, _ in
            try await cli.sh("wendy device audio list --help") { result in
                #expect(result.status.isSuccess)
                #expect(result.stdout.contains("List audio devices"))
                #expect(result.stdout.contains("wendy device audio list [flags]"))
                #expect(result.stdout.contains("--device"))
                #expect(result.stdout.contains("--json"))
                #expect(result.stderr == "")
            }
        }
    }

    @Test(
        .disabled(
            "WDY-1952: explicit-target inventory needs seeded managed-agent audio state without a physical device."
        )
    )
    func `uses explicit device selection without prompting`() async throws {}

    @Test
    func `reports missing device selection in non-interactive mode`() async throws {
        try await self.scenario.run(authenticated: false) { cli, _ in
            try await cli.sh("wendy device audio list --json") { result in
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
            "WDY-1952: human audio inventory needs seeded managed-agent audio hardware/capability state."
        )
    )
    func `lists audio devices`() async throws {}

    @Test(
        .disabled(
            "WDY-1952: JSON audio schema needs seeded managed-agent audio hardware/capability state."
        )
    )
    func `prints JSON audio device inventory`() async throws {}

    @Test
    func `rejects undocumented flags`() async throws {
        try await self.scenario.run(authenticated: false) { cli, _ in
            try await cli.sh("wendy device audio list --bogus") { result in
                #expect(result.status.isFailure)
                #expect(result.stdout == "")
                #expect(result.stderr.contains("unknown flag"))
            }
        }
    }

    @Test(
        .disabled(
            "WDY-1934: 'wendy device audio list' silently accepts extra positional arguments because the leaf command has no cobra.NoArgs validator."
        )
    )
    func `rejects undocumented positional arguments`() async throws {}
}
