import Testing
import WendyE2ETesting

/// Hidden deprecated compatibility command for `wendy cloud device info`.
@Suite
struct `'wendy cloud device version'` {
    let scenario = CLIAndAgentScenario()

    @Test
    func `is hidden from parent help while direct help mirrors cloud device info`() async throws {
        try await self.scenario.run(authenticated: false) { cli, _ in
            try await cli.sh("wendy cloud device --help") { result in
                #expect(result.status.isSuccess)
                #expect(result.stdout.contains("info"))
                #expect(!result.stdout.contains("\n  version "))
                #expect(result.stderr == "")
            }
            try await cli.sh("wendy cloud device version --help") { result in
                #expect(result.status.isSuccess)
                #expect(
                    result.stdout.contains(
                        "Show agent version, OS, architecture, GPU, and hardware info"
                    )
                )
                #expect(result.stdout.contains("wendy cloud device version [flags]"))
                #expect(result.stdout.contains("--check-updates"))
                #expect(result.stdout.contains("--prerelease"))
                #expect(result.stdout.contains("--cloud-grpc"))
                #expect(result.stdout.contains("--broker-url"))
                #expect(result.stderr == "")
            }
        }
    }

    @Test(
        .disabled(
            "WDY-1952: cloud-routed info equivalence and deprecation output need seeded tunnel/auth and managed-agent metadata."
        )
    )
    func `aliases cloud device info with a deprecation notice`() async throws {}

    @Test(
        .disabled(
            "WDY-1952: cloud-routed JSON compatibility needs seeded tunnel/auth and managed-agent metadata."
        )
    )
    func `JSON keeps output clean`() async throws {}
}
