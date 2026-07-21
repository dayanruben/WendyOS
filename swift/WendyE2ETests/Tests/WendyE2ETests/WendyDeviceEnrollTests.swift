import Testing
import WendyE2ETesting

@Suite
struct `'wendy device enroll'` {
    let scenario = CLIAndAgentScenario()

    @Test
    func `prints command help`() async throws {
        try await self.scenario.run(authenticated: false) { cli, _ in
            try await cli.sh("wendy device enroll --help") { result in
                #expect(result.status.isSuccess)
                #expect(result.stdout.contains("Creates an enrollment token"))
                #expect(result.stdout.contains("wendy device enroll [flags]"))
                #expect(result.stdout.contains("--name"))
                #expect(result.stdout.contains("--org"))
                #expect(result.stdout.contains("--cloud-grpc"))
                #expect(result.stdout.contains("--device"))
                #expect(result.stderr == "")
            }
        }
    }

    @Test(
        .disabled(
            "WDY-1949: explicit-target enrollment needs isolated auth, PKI/cloud, and disposable provisioning fixtures."
        )
    )
    func `uses explicit device selection without prompting`() async throws {}

    @Test
    func `reports missing device selection in non-interactive mode`() async throws {
        try await self.scenario.run(authenticated: false) { cli, _ in
            try await cli.sh("wendy device enroll --name Example --org 1 --json") { result in
                #expect(result.status.isFailure)
                #expect(result.stdout == "")
                #expect(result.stderr.contains("no device specified"))
                #expect(!result.stderr.contains("Select a device"))
            }
        }
    }

    @Test(
        .disabled(
            "WDY-1949: connection and incompatible-RPC failures need disposable provisioning and auth fixtures."
        )
    )
    func `reports unreachable devices without partial success`() async throws {}

    @Test(
        .disabled(
            "WDY-1949: successful enrollment needs isolated auth, token issuance, certificate, and disposable device fixtures."
        )
    )
    func `enrolls the selected device with Wendy Cloud`() async throws {}

    @Test(
        .disabled(
            "WDY-1959: auth is currently resolved only after target connection and optional WiFi inspection."
        )
    )
    func `reports missing auth before touching the device`() async throws {}

    @Test(
        .disabled(
            "WDY-1949: partial-failure rollback needs controllable token, certificate, and provisioning failure fixtures."
        )
    )
    func `does not leave partial credentials on enrollment failure`() async throws {}

    @Test(
        .disabled(
            "WDY-1949: JSON enrollment metadata needs isolated successful PKI/cloud and device fixtures."
        )
    )
    func `prints JSON enrollment metadata`() async throws {}

    @Test
    func `rejects undocumented flags`() async throws {
        try await self.scenario.run(authenticated: false) { cli, _ in
            try await cli.sh("wendy device enroll --bogus") { result in
                #expect(result.status.isFailure)
                #expect(result.stdout == "")
                #expect(result.stderr.contains("unknown flag"))
            }
        }
    }

    @Test(
        .disabled(
            "WDY-1934: 'wendy device enroll' silently accepts positional arguments because the leaf command has no cobra.NoArgs validator."
        )
    )
    func `rejects undocumented positional arguments`() async throws {}
}
