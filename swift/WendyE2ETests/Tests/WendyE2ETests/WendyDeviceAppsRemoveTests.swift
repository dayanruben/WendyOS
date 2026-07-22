import Testing
import WendyE2ETesting

@Suite
struct `'wendy device apps remove'` {
    let scenario = CLIAndAgentScenario()

    @Test
    func `prints command help`() async throws {
        try await self.scenario.run(authenticated: false) { cli, _ in
            try await cli.sh("wendy device apps remove --help") { result in
                #expect(result.status.isSuccess)
                #expect(result.stdout.contains("Remove an application"))
                #expect(result.stdout.contains("wendy device apps remove [app-name] [flags]"))
                #expect(result.stdout.contains("--cleanup"))
                #expect(result.stdout.contains("--delete-volumes"))
                #expect(result.stdout.contains("--force"))
                #expect(result.stdout.contains("--device"))
                #expect(result.stderr == "")
            }
        }
    }

    @Test(
        .disabled(
            "WDY-1952: explicit-target removal needs seeded managed-agent application state without a physical device."
        )
    )
    func `uses explicit device selection without prompting`() async throws {}

    @Test
    func `reports missing device selection in non-interactive mode`() async throws {
        try await self.scenario.run(authenticated: false) { cli, _ in
            try await cli.sh("wendy device apps remove example --force --json") { result in
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
            "WDY-1952: confirmation and successful removal need seeded managed-agent application state."
        )
    )
    func `removes an application after confirmation`() async throws {}

    @Test(
        .disabled(
            "WDY-1952: cleanup isolation needs seeded application, image, and persistent-volume state."
        )
    )
    func `honors cleanup and volume deletion flags`() async throws {}

    @Test(
        .disabled(
            "WDY-1952: unknown-application behavior needs seeded neighboring application and resource state."
        )
    )
    func `reports unknown applications without deleting resources`() async throws {}

    @Test
    func `rejects undocumented flags`() async throws {
        try await self.scenario.run(authenticated: false) { cli, _ in
            try await cli.sh("wendy device apps remove example --bogus") { result in
                #expect(result.status.isFailure)
                #expect(result.stdout == "")
                #expect(result.stderr.contains("unknown flag"))
            }
        }
    }

    @Test
    func `rejects extra positional arguments`() async throws {
        try await self.scenario.run(authenticated: false) { cli, _ in
            try await cli.sh("wendy device apps remove one two") { result in
                #expect(result.status.isFailure)
                #expect(result.stdout == "")
                #expect(result.stderr.contains("accepts at most 1 arg(s), received 2"))
            }
        }
    }
}
