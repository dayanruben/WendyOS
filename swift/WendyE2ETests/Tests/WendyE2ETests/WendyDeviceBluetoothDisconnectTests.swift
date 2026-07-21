import Testing
import WendyE2ETesting

@Suite
struct `'wendy device bluetooth disconnect'` {
    let scenario = CLIAndAgentScenario()

    @Test
    func `prints command help`() async throws {
        try await self.scenario.run(authenticated: false) { cli, _ in
            try await cli.sh("wendy device bluetooth disconnect --help") { result in
                #expect(result.status.isSuccess)
                #expect(result.stdout.contains("Disconnect a Bluetooth peripheral"))
                #expect(
                    result.stdout.contains("wendy device bluetooth disconnect [address] [flags]")
                )
                #expect(result.stdout.contains("--device"))
                #expect(result.stderr == "")
            }
        }
    }

    @Test(
        .disabled(
            "WDY-1952: explicit-target disconnection needs a seeded managed agent and simulated Bluetooth capability without physical hardware."
        )
    )
    func `uses explicit device selection without prompting`() async throws {}

    @Test
    func `reports missing device selection in non-interactive mode`() async throws {
        try await self.scenario.run(authenticated: false) { cli, _ in
            try await cli.sh("wendy device bluetooth disconnect AA:BB:CC:DD:EE:FF --json") {
                result in
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
            "WDY-1952: successful disconnection needs simulated managed-agent Bluetooth connection state."
        )
    )
    func `disconnects a Bluetooth peripheral`() async throws {}

    @Test
    func `requires a peripheral address`() async throws {
        try await self.scenario.run(authenticated: false) { cli, _ in
            try await cli.sh("wendy device bluetooth disconnect") { result in
                #expect(result.status.isFailure)
                #expect(result.stdout == "")
                #expect(result.stderr.contains("accepts 1 arg(s), received 0"))
            }
        }
    }

    @Test(
        .disabled(
            "WDY-1957: malformed Bluetooth addresses are forwarded to target resolution/RPC without local validation."
        )
    )
    func `rejects malformed peripheral addresses`() async throws {}

    @Test(
        .disabled(
            "WDY-1952: already-disconnected semantics need simulated managed-agent Bluetooth state."
        )
    )
    func `handles already disconnected peripherals predictably`() async throws {}

    @Test
    func `rejects undocumented flags`() async throws {
        try await self.scenario.run(authenticated: false) { cli, _ in
            try await cli.sh("wendy device bluetooth disconnect AA:BB:CC:DD:EE:FF --bogus") {
                result in
                #expect(result.status.isFailure)
                #expect(result.stdout == "")
                #expect(result.stderr.contains("unknown flag"))
            }
        }
    }

    @Test
    func `rejects extra positional arguments`() async throws {
        try await self.scenario.run(authenticated: false) { cli, _ in
            try await cli.sh("wendy device bluetooth disconnect one two") { result in
                #expect(result.status.isFailure)
                #expect(result.stdout == "")
                #expect(result.stderr.contains("accepts 1 arg(s), received 2"))
            }
        }
    }
}
