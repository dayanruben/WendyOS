import Testing
import WendyE2ETesting

@Suite
struct `'wendy cloud enroll-device'` {
    let scenario = CLIAndAgentScenario()

    @Test
    func `prints command help`() async throws {
        try await self.scenario.run(authenticated: false) { cli, _ in
            try await cli.sh("wendy cloud enroll-device --help") { result in
                #expect(result.status.isSuccess)
                #expect(result.stdout.contains("Alias for 'wendy device enroll'"))
                #expect(result.stdout.contains("wendy cloud enroll-device [flags]"))
                #expect(result.stdout.contains("--cloud-grpc"))
                #expect(result.stdout.contains("--name"))
                #expect(result.stdout.contains("--org"))
                #expect(result.stdout.contains("--device"))
                #expect(result.stdout.contains("--json"))
                #expect(result.stderr == "")
            }
        }
    }

    @Test(
        .disabled(
            "WDY-1949/WDY-1952: alias parity needs isolated PKI auth and a disposable seeded device provisioning target."
        )
    )
    func `acts as the cloud alias for device enrollment`() async throws {}

    @Test(
        .disabled(
            "WDY-1949: endpoint selection needs multiple isolated cloud/PKI auth sessions and token issuance fixtures."
        )
    )
    func `uses the requested cloud endpoint`() async throws {}

    @Test(
        .disabled(
            "WDY-1959: cloud enroll-device connects to and may inspect WiFi on the local device before resolving auth; WDY-1949 tracks isolated failure fixtures."
        )
    )
    func `reports missing auth or unreachable devices without partial enrollment`() async throws {}

    @Test(
        .disabled(
            "WDY-1949: JSON enrollment output needs isolated successful PKI/cloud and disposable device fixtures."
        )
    )
    func `prints JSON enrollment result for automation`() async throws {}

    @Test(
        .disabled(
            "WDY-1959: invalid auth configuration is not read until after local device connection and optional WiFi inspection."
        )
    )
    func `reports invalid CLI configuration before acting`() async throws {}

    @Test
    func `rejects undocumented flags`() async throws {
        try await self.scenario.run(authenticated: false) { cli, _ in
            try await cli.sh("wendy cloud enroll-device --bogus") { result in
                #expect(result.status.isFailure)
                #expect(result.stdout == "")
                #expect(result.stderr.contains("unknown flag"))
                #expect(result.stderr.contains("--bogus"))
            }
        }
    }

    @Test(
        .disabled(
            "WDY-1934: 'wendy cloud enroll-device' silently accepts positional arguments because the command has no cobra.NoArgs validator."
        )
    )
    func `rejects undocumented positional arguments`() async throws {}
}
