import Testing
import WendyE2ETesting

@Suite
struct `'wendy device camera list'` {
    let scenario = CLIAndAgentScenario()

    @Test
    func `prints command help`() async throws {
        try await self.scenario.run(authenticated: false) { cli, _ in
            try await cli.sh("wendy device camera list --help") { result in
                #expect(result.status.isSuccess)
                #expect(result.stdout.contains("List cameras"))
                #expect(result.stdout.contains("wendy device camera list [flags]"))
                #expect(result.stdout.contains("--device"))
                #expect(result.stdout.contains("--json"))
                #expect(result.stderr == "")
            }
        }
    }

    @Test(
        .disabled(
            "WDY-1952: explicit-target inventory needs seeded managed-agent camera state without a physical device."
        )
    )
    func `uses explicit device selection without prompting`() async throws {}

    @Test
    func `reports missing device selection in non-interactive mode`() async throws {
        try await self.scenario.run(authenticated: false) { cli, _ in
            try await cli.sh("wendy device camera list --json") { result in
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
            "WDY-1952: human camera inventory needs seeded managed-agent camera hardware/capability state."
        )
    )
    func `lists cameras`() async throws {}

    @Test(
        .disabled(
            "WDY-1952: JSON camera schema needs seeded managed-agent camera hardware/capability state."
        )
    )
    func `prints JSON camera inventory`() async throws {}

    @Test
    func `rejects undocumented flags`() async throws {
        try await self.scenario.run(authenticated: false) { cli, _ in
            try await cli.sh("wendy device camera list --bogus") { result in
                #expect(result.status.isFailure)
                #expect(result.stdout == "")
                #expect(result.stderr.contains("unknown flag"))
            }
        }
    }

    @Test(
        .disabled(
            "WDY-1934: 'wendy device camera list' silently accepts extra positional arguments because the leaf command has no cobra.NoArgs validator."
        )
    )
    func `rejects undocumented positional arguments`() async throws {}
}
