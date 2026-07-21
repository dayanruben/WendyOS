import Testing
import WendyE2ETesting

@Suite
struct `'wendy device hardware list'` {
    let scenario = CLIAndAgentScenario()

    @Test
    func `prints command help`() async throws {
        try await self.scenario.run(authenticated: false) { cli, _ in
            try await cli.sh("wendy device hardware list --help") { result in
                #expect(result.status.isSuccess)
                #expect(result.stdout.contains("List hardware capabilities"))
                #expect(result.stdout.contains("wendy device hardware list [flags]"))
                #expect(result.stdout.contains("--category"))
                #expect(result.stdout.contains("--device"))
                #expect(result.stdout.contains("--json"))
                #expect(result.stderr == "")
            }
        }
    }

    @Test(
        .disabled(
            "WDY-1952: explicit-target inventory needs seeded managed-agent hardware state without a physical device."
        )
    )
    func `uses explicit device selection without prompting`() async throws {}

    @Test
    func `reports missing device selection in non-interactive mode`() async throws {
        try await self.scenario.run(authenticated: false) { cli, _ in
            try await cli.sh("wendy device hardware list --json") { result in
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
        .disabled("WDY-1952: human hardware inventory needs seeded managed-agent capability state.")
    )
    func `lists hardware capabilities`() async throws {}

    @Test(
        .disabled(
            "WDY-1952: category filtering needs seeded multi-category managed-agent capability state."
        )
    )
    func `filters by category`() async throws {}

    @Test(.disabled("WDY-1952: JSON hardware schema needs seeded managed-agent capability state."))
    func `prints JSON hardware inventory`() async throws {}

    @Test
    func `rejects undocumented flags`() async throws {
        try await self.scenario.run(authenticated: false) { cli, _ in
            try await cli.sh("wendy device hardware list --bogus") { result in
                #expect(result.status.isFailure)
                #expect(result.stdout == "")
                #expect(result.stderr.contains("unknown flag"))
            }
        }
    }

    @Test(
        .disabled(
            "WDY-1934: 'wendy device hardware list' silently accepts extra positional arguments because the leaf command has no cobra.NoArgs validator."
        )
    )
    func `rejects undocumented positional arguments`() async throws {}
}
