import Testing
import WendyE2ETesting

@Suite
struct `'wendy cloud device update'` {
    let scenario = CLIAndAgentScenario()

    @Test
    func `prints command help`() async throws {
        try await self.scenario.run(authenticated: false) { cli, _ in
            try await cli.sh("wendy cloud device update --help") { result in
                #expect(result.status.isSuccess)
                #expect(result.stdout.contains("Updates the agent binary on the device"))
                #expect(result.stdout.contains("wendy cloud device update [flags]"))
                #expect(result.stdout.contains("--binary"))
                #expect(result.stdout.contains("--nightly"))
                #expect(result.stdout.contains("--artifact-url"))
                #expect(result.stdout.contains("--yes"))
                #expect(result.stdout.contains("--device"))
                #expect(result.stderr == "")
            }
        }
    }

    @Test(
        .disabled(
            "WDY-1949/WDY-1952: explicit cloud-target update needs a disposable seeded agent and isolated release/artifact fixtures."
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
            try await cli.sh("wendy cloud device update --device target --json") { result in
                #expect(result.status.isFailure)
                #expect(result.stdout == "")
                #expect(result.stderr.contains("not logged in"))
                #expect(result.stderr.contains("wendy auth login"))
            }
        }
    }

    @Test(
        .disabled(
            "WDY-1952: connection and incompatible-RPC failures need a disposable seeded agent with controllable responses."
        )
    )
    func `reports unreachable devices without partial success`() async throws {}

    @Test(
        .disabled(
            "WDY-1952: release update needs isolated downloads and a disposable agent restart/reconnect target."
        )
    )
    func `updates the device agent from the latest release`() async throws {}

    @Test(
        .disabled(
            "WDY-1952: local binary upload needs an architecture fixture and disposable agent restart/reconnect target."
        )
    )
    func `uploads a local binary when requested`() async throws {}

    @Test(.disabled("WDY-1952: nightly selection needs isolated release and OS manifest fixtures."))
    func `uses nightly releases when requested`() async throws {}

    @Test(
        .disabled(
            "WDY-1952: failure preservation needs controllable download/upload/restart stages on a disposable agent."
        )
    )
    func `preserves the running agent on failed update`() async throws {}

    @Test
    func `rejects undocumented flags`() async throws {
        try await self.scenario.run(authenticated: false) { cli, _ in
            try await cli.sh("wendy cloud device update --bogus") { result in
                #expect(result.status.isFailure)
                #expect(result.stdout == "")
                #expect(result.stderr.contains("unknown flag"))
            }
        }
    }

    @Test(
        .disabled(
            "WDY-1934: 'wendy cloud device update' silently accepts positional arguments because the leaf command has no cobra.NoArgs validator."
        )
    )
    func `rejects undocumented positional arguments`() async throws {}
}
