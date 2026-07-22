import Testing
import WendyE2ETesting

@Suite
struct `'wendy cloud device wifi rank'` {
    let scenario = CLIAndAgentScenario()

    @Test
    func `prints command help`() async throws {
        try await self.scenario.run(authenticated: false) { cli, _ in
            try await cli.sh("wendy cloud device wifi rank --help") { result in
                #expect(result.status.isSuccess)
                #expect(result.stdout.contains("Set the priority of a single known network"))
                #expect(result.stdout.contains("wendy cloud device wifi rank [flags]"))
                #expect(result.stdout.contains("--ssid"))
                #expect(result.stdout.contains("--priority"))
                #expect(result.stdout.contains("--order"))
                #expect(result.stdout.contains("--device"))
                #expect(result.stderr == "")
            }
        }
    }

    @Test(
        .disabled(
            "WDY-1949/WDY-1952: explicit cloud-target ranking needs seeded managed-agent saved-network state without physical radios."
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
            try await cli.sh(
                "wendy cloud device wifi rank --ssid Example --priority 10 --device target --json"
            ) { result in
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
            "WDY-1952: single-network ranking needs seeded neighboring managed-agent priority state."
        )
    )
    func `sets priority for a single known network`() async throws {}

    @Test(
        .disabled(
            "WDY-1952: bulk ranking needs seeded listed/unlisted managed-agent priority state."
        )
    )
    func `bulk reorders known networks`() async throws {}

    @Test
    func `validates mutually exclusive ranking modes`() async throws {
        try await self.scenario.run(authenticated: false) { cli, _ in
            try await cli.sh("wendy cloud device wifi rank") { result in
                #expect(result.status.isFailure)
                #expect(result.stdout == "")
                #expect(result.stderr.contains("must pass either --order or --ssid"))
            }
            try await cli.sh("wendy cloud device wifi rank --ssid Example") { result in
                #expect(result.status.isFailure)
                #expect(result.stdout == "")
                #expect(result.stderr.contains("--priority is required when --ssid is set"))
            }
            try await cli.sh(
                "wendy cloud device wifi rank --ssid Example --priority 1 --order Other"
            ) {
                result in
                #expect(result.status.isFailure)
                #expect(result.stdout == "")
                #expect(result.stderr.contains("--order and --ssid are mutually exclusive"))
            }
        }
    }

    @Test
    func `rejects undocumented flags`() async throws {
        try await self.scenario.run(authenticated: false) { cli, _ in
            try await cli.sh("wendy cloud device wifi rank --bogus") { result in
                #expect(result.status.isFailure)
                #expect(result.stdout == "")
                #expect(result.stderr.contains("unknown flag"))
            }
        }
    }

    @Test(
        .disabled(
            "WDY-1934: 'wendy cloud device wifi rank' silently accepts positional arguments because the leaf command has no cobra.NoArgs validator."
        )
    )
    func `rejects undocumented positional arguments`() async throws {}
}
