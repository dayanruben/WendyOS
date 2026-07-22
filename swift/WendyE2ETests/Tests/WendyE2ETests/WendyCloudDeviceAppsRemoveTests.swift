import Testing
import WendyE2ETesting

@Suite
struct `'wendy cloud device apps remove'` {
    let scenario = CLIAndAgentScenario()

    @Test
    func `prints command help`() async throws {
        try await self.scenario.run(authenticated: false) { cli, _ in
            try await cli.sh("wendy cloud device apps remove --help") { result in
                #expect(result.status.isSuccess)
                #expect(result.stdout.contains("Remove an application"))
                #expect(
                    result.stdout.contains(
                        "wendy cloud device apps remove [app-name] [flags]"
                    )
                )
                #expect(result.stdout.contains("--cleanup"))
                #expect(result.stdout.contains("--delete-volumes"))
                #expect(result.stdout.contains("--force"))
                #expect(result.stdout.contains("--cloud-grpc"))
                #expect(result.stdout.contains("--device"))
                #expect(result.stderr == "")
            }
        }
    }

    @Test(
        .disabled(
            "WDY-1949/WDY-1952: explicit cloud-target removal needs isolated auth, tunnel, and seeded managed-agent application state."
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
                "wendy cloud device apps remove example --device target --force --json"
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
            "WDY-1952: tunnel, connection, and incompatible-RPC failures need controllable seeded cloud and managed-agent responses."
        )
    )
    func `reports unreachable devices without partial success`() async throws {}

    @Test(
        .disabled(
            "WDY-1952: confirmation and successful removal need seeded cloud tunnel and managed-agent application state."
        )
    )
    func `removes an application after confirmation`() async throws {}

    @Test(
        .disabled(
            "WDY-1952: cleanup isolation needs seeded application, image, and persistent-volume state behind a cloud tunnel."
        )
    )
    func `honors cleanup and volume deletion flags`() async throws {}

    @Test(
        .disabled(
            "WDY-1952: unknown-application behavior needs seeded neighboring cloud-managed application and resource state."
        )
    )
    func `reports unknown applications without deleting resources`() async throws {}

    @Test
    func `rejects undocumented flags`() async throws {
        try await self.scenario.run(authenticated: false) { cli, _ in
            try await cli.sh("wendy cloud device apps remove example --bogus") { result in
                #expect(result.status.isFailure)
                #expect(result.stdout == "")
                #expect(result.stderr.contains("unknown flag"))
            }
        }
    }

    @Test
    func `rejects extra positional arguments`() async throws {
        try await self.scenario.run(authenticated: false) { cli, _ in
            try await cli.sh("wendy cloud device apps remove one two") { result in
                #expect(result.status.isFailure)
                #expect(result.stdout == "")
                #expect(result.stderr.contains("accepts at most 1 arg(s), received 2"))
            }
        }
    }
}
