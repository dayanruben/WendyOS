import Testing
import WendyE2ETesting

@Suite
struct `'wendy cloud device apps stop'` {
    let scenario = CLIAndAgentScenario()

    @Test
    func `prints command help`() async throws {
        try await self.scenario.run(authenticated: false) { cli, _ in
            try await cli.sh("wendy cloud device apps stop --help") { result in
                #expect(result.status.isSuccess)
                #expect(result.stdout.contains("Stop an application"))
                #expect(
                    result.stdout.contains(
                        "wendy cloud device apps stop [app-name] [flags]"
                    )
                )
                #expect(result.stdout.contains("--cloud-grpc"))
                #expect(result.stdout.contains("--device"))
                #expect(result.stdout.contains("--json"))
                #expect(result.stderr == "")
            }
        }
    }

    @Test(
        .disabled(
            "WDY-1949/WDY-1952: explicit cloud-target stop needs isolated auth, tunnel, and seeded managed-agent application state."
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
                "wendy cloud device apps stop example --device target --json"
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
            "WDY-1952: successful stop needs seeded running cloud-managed application/container state."
        )
    )
    func `stops a running application`() async throws {}

    @Test(
        .disabled(
            "WDY-1952: stopped-app idempotence needs seeded stopped cloud-managed application/container state."
        )
    )
    func `handles already stopped applications predictably`() async throws {}

    @Test(
        .disabled(
            "WDY-1952: unknown-app isolation needs seeded neighboring cloud-managed application/container state."
        )
    )
    func `reports unknown applications without side effects`() async throws {}

    @Test
    func `rejects undocumented flags`() async throws {
        try await self.scenario.run(authenticated: false) { cli, _ in
            try await cli.sh("wendy cloud device apps stop example --bogus") { result in
                #expect(result.status.isFailure)
                #expect(result.stdout == "")
                #expect(result.stderr.contains("unknown flag"))
            }
        }
    }

    @Test
    func `rejects extra positional arguments`() async throws {
        try await self.scenario.run(authenticated: false) { cli, _ in
            try await cli.sh("wendy cloud device apps stop one two") { result in
                #expect(result.status.isFailure)
                #expect(result.stdout == "")
                #expect(result.stderr.contains("accepts at most 1 arg(s), received 2"))
            }
        }
    }
}
