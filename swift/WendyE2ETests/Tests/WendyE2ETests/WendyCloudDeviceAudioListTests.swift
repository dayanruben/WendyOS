import Testing
import WendyE2ETesting

@Suite
struct `'wendy cloud device audio list'` {
    let scenario = CLIAndAgentScenario()

    @Test
    func `prints command help`() async throws {
        try await self.scenario.run(authenticated: false) { cli, _ in
            try await cli.sh("wendy cloud device audio list --help") { result in
                #expect(result.status.isSuccess)
                #expect(result.stdout.contains("List audio devices"))
                #expect(result.stdout.contains("wendy cloud device audio list [flags]"))
                #expect(result.stdout.contains("--cloud-grpc"))
                #expect(result.stdout.contains("--broker-url"))
                #expect(result.stdout.contains("--device"))
                #expect(result.stdout.contains("--json"))
                #expect(result.stderr == "")
            }
        }
    }

    @Test(
        .disabled(
            "WDY-1949: explicit cloud-device selection needs isolated auth and tunnel fixtures."
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
            try await cli.sh("wendy cloud device audio list --device example --json") { result in
                #expect(result.status.isFailure)
                #expect(result.stdout == "")
                #expect(result.stderr.contains("not logged in"))
                #expect(result.stderr.contains("wendy auth login"))
            }
        }
    }

    @Test(
        .disabled(
            "WDY-1952: tunnel and incompatible-RPC failures need seeded cloud and managed-agent responses."
        )
    )
    func `reports unreachable devices without partial success`() async throws {}

    @Test(
        .disabled(
            "WDY-1952: human audio inventory needs seeded cloud tunnel and managed-agent audio state."
        )
    )
    func `lists audio devices`() async throws {}

    @Test(
        .disabled(
            "WDY-1952: JSON audio schema needs seeded cloud tunnel and managed-agent audio state."
        )
    )
    func `prints JSON audio device inventory`() async throws {}

    @Test
    func `rejects undocumented flags`() async throws {
        try await self.scenario.run(authenticated: false) { cli, _ in
            try await cli.sh("wendy cloud device audio list --bogus") { result in
                #expect(result.status.isFailure)
                #expect(result.stdout == "")
                #expect(result.stderr.contains("unknown flag"))
            }
        }
    }

    @Test(
        .disabled(
            "WDY-1934: 'wendy cloud device audio list' silently accepts positional arguments because the mirrored leaf command has no cobra.NoArgs validator."
        )
    )
    func `rejects undocumented positional arguments`() async throws {}
}
