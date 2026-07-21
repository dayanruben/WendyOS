import Testing
import WendyE2ETesting

/// Public alias path for `wendy device bluetooth list`.
@Suite
struct `'wendy device bt list'` {
    let scenario = CLIAndAgentScenario()

    /** Resolves `bt` to the canonical Bluetooth list command and option surface. */
    @Test
    func `prints '... bluetooth list help' through the 'bt' alias`() async throws {
        try await self.scenario.run(authenticated: false) { cli, _ in
            try await cli.sh("wendy device bt list --help") { result in
                let stdout = result.stdout
                #expect(result.status.isSuccess)
                #expect(stdout.contains("Scan for Bluetooth peripherals"))
                #expect(stdout.contains("wendy device bluetooth list [flags]"))
                #expect(stdout.contains("--help"))
                #expect(stdout.contains("--device"))
                #expect(stdout.contains("--json"))
                #expect(result.stderr == "")
            }
        }
    }

    @Test(
        .disabled(
            "WDY-1952: canonical/alias peripheral inventory equivalence needs seeded managed-agent Bluetooth responses without physical hardware."
        )
    )
    func `aliases '... device bluetooth list'`() async throws {
        // TODO: enable with seeded managed-agent Bluetooth fixtures (WDY-1952).
    }

    /** Missing target fails before scanning or prompting. */
    @Test
    func `reports missing device without scanning`() async throws {
        try await self.scenario.run(authenticated: false) { cli, _ in
            try await cli.sh("wendy device bt list --json") { result in
                #expect(result.status.isFailure)
                #expect(result.stdout == "")
                #expect(result.stderr.contains("no device specified"))
                #expect(!result.stderr.contains("Select a device"))
            }
        }
    }
}
