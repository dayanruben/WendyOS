import Testing
import WendyE2ETesting

@Suite
struct `'wendy cloud device bluetooth forget'` {
    let scenario = CLIAndAgentScenario()

    @Test
    func `prints command help`() async throws {
        try await self.scenario.run(authenticated: false) { cli, _ in
            try await cli.sh("wendy cloud device bluetooth forget --help") { result in
                #expect(result.status.isSuccess)
                #expect(result.stdout.contains("Forget a paired Bluetooth peripheral"))
                #expect(
                    result.stdout.contains("wendy cloud device bluetooth forget [address] [flags]")
                )
                #expect(result.stdout.contains("--device"))
                #expect(result.stderr == "")
            }
        }
    }

    @Test(
        .disabled(
            "WDY-1949/WDY-1952: explicit cloud-target forget needs a seeded managed agent and simulated Bluetooth capability without physical hardware."
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
                "wendy cloud device bluetooth forget AA:BB:CC:DD:EE:FF --device target --json"
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
            "WDY-1952: successful forget needs simulated managed-agent pairing/trust state without physical radios."
        )
    )
    func `forgets a paired Bluetooth peripheral`() async throws {}

    @Test
    func `requires a peripheral address`() async throws {
        try await self.scenario.run(authenticated: false) { cli, _ in
            try await cli.sh("wendy cloud device bluetooth forget") { result in
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
            "WDY-1952: unknown-address isolation needs seeded neighboring managed-agent pairing state."
        )
    )
    func `reports unknown peripherals without changing known devices`() async throws {}

    @Test
    func `rejects undocumented flags`() async throws {
        try await self.scenario.run(authenticated: false) { cli, _ in
            try await cli.sh("wendy cloud device bluetooth forget AA:BB:CC:DD:EE:FF --bogus") {
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
            try await cli.sh("wendy cloud device bluetooth forget one two") { result in
                #expect(result.status.isFailure)
                #expect(result.stdout == "")
                #expect(result.stderr.contains("accepts 1 arg(s), received 2"))
            }
        }
    }
}
