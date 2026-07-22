import Testing
import WendyE2ETesting

@Suite
struct `'wendy device wifi disconnect'` {
    let scenario = CLIAndAgentScenario()

    @Test
    func `prints command help`() async throws {
        try await self.scenario.run(authenticated: false) { cli, _ in
            try await cli.sh("wendy device wifi disconnect --help") { result in
                #expect(result.status.isSuccess)
                #expect(result.stdout.contains("Disconnect from the current WiFi network"))
                #expect(result.stdout.contains("wendy device wifi disconnect [flags]"))
                #expect(result.stdout.contains("--device"))
                #expect(result.stderr == "")
            }
        }
    }

    @Test(
        .disabled(
            "WDY-1952: explicit-target disconnection needs a seeded managed agent and simulated WiFi capability without physical radios."
        )
    )
    func `uses explicit device selection without prompting`() async throws {}

    @Test
    func `reports missing device selection in non-interactive mode`() async throws {
        try await self.scenario.run(authenticated: false) { cli, _ in
            try await cli.sh("wendy device wifi disconnect --json") { result in
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
            "WDY-1952: successful disconnection needs simulated managed-agent WiFi connection state."
        )
    )
    func `disconnects from the current WiFi network`() async throws {}

    @Test(
        .disabled(
            "WDY-1952: already-disconnected semantics need simulated managed-agent WiFi state."
        )
    )
    func `handles already disconnected devices predictably`() async throws {}

    @Test
    func `rejects undocumented flags`() async throws {
        try await self.scenario.run(authenticated: false) { cli, _ in
            try await cli.sh("wendy device wifi disconnect --bogus") { result in
                #expect(result.status.isFailure)
                #expect(result.stdout == "")
                #expect(result.stderr.contains("unknown flag"))
            }
        }
    }

    @Test(
        .disabled(
            "WDY-1934: 'wendy device wifi disconnect' silently accepts positional arguments because the leaf command has no cobra.NoArgs validator."
        )
    )
    func `rejects undocumented positional arguments`() async throws {}
}
