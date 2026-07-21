import Testing
import WendyE2ETesting

@Suite
struct `'wendy device setup'` {
    let scenario = CLIAndAgentScenario()

    @Test
    func `prints command help`() async throws {
        try await self.scenario.run(authenticated: false) { cli, _ in
            try await cli.sh("wendy device setup --help") { result in
                #expect(result.status.isSuccess)
                #expect(result.stdout.contains("Walks through enrollment"))
                #expect(result.stdout.contains("wendy device setup [flags]"))
                #expect(result.stdout.contains("--device"))
                #expect(result.stderr == "")
            }
        }
    }

    @Test(
        .disabled(
            "WDY-1952: explicit-target setup needs seeded provisioning, auth, WiFi, and version state."
        )
    )
    func `uses explicit device selection without prompting`() async throws {}

    @Test
    func `reports missing device selection in non-interactive mode`() async throws {
        try await self.scenario.run(authenticated: false) { cli, _ in
            try await cli.sh("wendy device setup --json") { result in
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
            "WDY-1952: the setup wizard needs seeded provisioning/auth/WiFi state plus scripted PTY interaction."
        )
    )
    func `walks through new device setup`() async throws {}

    @Test(
        .disabled(
            "WDY-1952: setup resume behavior needs seeded partially configured device and auth state."
        )
    )
    func `uses existing state to skip completed setup steps`() async throws {}

    @Test(
        .disabled(
            "WDY-1952: setup cancellation needs seeded state and harness process control across wizard stages."
        )
    )
    func `cancels without continuing later setup`() async throws {}

    @Test
    func `rejects undocumented flags`() async throws {
        try await self.scenario.run(authenticated: false) { cli, _ in
            try await cli.sh("wendy device setup --bogus") { result in
                #expect(result.status.isFailure)
                #expect(result.stdout == "")
                #expect(result.stderr.contains("unknown flag"))
            }
        }
    }

    @Test(
        .disabled(
            "WDY-1934: 'wendy device setup' silently accepts positional arguments because the leaf command has no cobra.NoArgs validator."
        )
    )
    func `rejects undocumented positional arguments`() async throws {}
}
