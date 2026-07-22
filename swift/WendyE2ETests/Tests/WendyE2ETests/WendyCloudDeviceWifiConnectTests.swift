import Testing
import WendyE2ETesting

@Suite
struct `'wendy cloud device wifi connect'` {
    let scenario = CLIAndAgentScenario()

    @Test
    func `prints command help`() async throws {
        try await self.scenario.run(authenticated: false) { cli, _ in
            try await cli.sh("wendy cloud device wifi connect --help") { result in
                #expect(result.status.isSuccess)
                #expect(result.stdout.contains("Connect to a WiFi network"))
                #expect(result.stdout.contains("wendy cloud device wifi connect [flags]"))
                #expect(result.stdout.contains("--ssid"))
                #expect(result.stdout.contains("--password"))
                #expect(result.stdout.contains("--device"))
                #expect(result.stderr == "")
            }
        }
    }

    @Test(
        .disabled(
            "WDY-1949/WDY-1952: explicit cloud-target connection needs a seeded managed agent and simulated WiFi capability without physical radios."
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
                "wendy cloud device wifi connect --ssid Example --device target --json"
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
            "WDY-1952: successful connection needs simulated managed-agent WiFi association state without physical radios."
        )
    )
    func `connects to a WiFi network`() async throws {}

    @Test(
        .disabled(
            "WDY-1952: password prompting and redaction need a scripted PTY plus simulated secured-network state."
        )
    )
    func `prompts for missing password only in interactive mode`() async throws {}

    @Test(
        .disabled(
            "WDY-1952: omitted SSID intentionally opens a device-backed network picker and needs simulated scan results."
        )
    )
    func `selects a missing SSID interactively`() async throws {}

    @Test(
        .disabled(
            "WDY-1952: failed-credential preservation needs seeded neighboring managed-agent network profiles."
        )
    )
    func `does not save failed credentials`() async throws {}

    @Test
    func `rejects undocumented flags`() async throws {
        try await self.scenario.run(authenticated: false) { cli, _ in
            try await cli.sh("wendy cloud device wifi connect --bogus") { result in
                #expect(result.status.isFailure)
                #expect(result.stdout == "")
                #expect(result.stderr.contains("unknown flag"))
            }
        }
    }

    @Test(
        .disabled(
            "WDY-1934: 'wendy cloud device wifi connect' silently accepts positional arguments because the leaf command has no cobra.NoArgs validator."
        )
    )
    func `rejects undocumented positional arguments`() async throws {}
}
