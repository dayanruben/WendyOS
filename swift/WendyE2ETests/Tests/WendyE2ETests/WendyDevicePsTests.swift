import Testing
import WendyE2ETesting

/// Hidden compatibility alias for `wendy device apps list`.
@Suite
struct `'wendy device ps'` {
    let scenario = CLIAndAgentScenario()

    /** The hidden alias remains directly invocable and identifies its canonical command. */
    @Test
    func `prints '... device ps' alias help`() async throws {
        try await self.scenario.run(authenticated: false) { cli, _ in
            try await cli.sh("wendy device --help") { result in
                #expect(result.status.isSuccess)
                #expect(!result.stdout.contains("  ps"))
            }
            try await cli.sh("wendy device ps --help") { result in
                let stdout = result.stdout
                #expect(result.status.isSuccess)
                #expect(stdout.contains("alias for 'apps list'"))
                #expect(stdout.contains("wendy device ps [flags]"))
                #expect(stdout.contains("--device"))
                #expect(stdout.contains("--json"))
                #expect(result.stderr == "")
            }
        }
    }

    @Test(
        .disabled(
            "WDY-1952: human inventory equivalence needs seeded managed-agent application state without a physical device."
        )
    )
    func `aliases '... device apps list'`() async throws {
        // TODO: enable with seeded managed-agent app fixtures (WDY-1952).
    }

    @Test(
        .disabled(
            "WDY-1952: JSON inventory schema equivalence needs seeded managed-agent application state without a physical device."
        )
    )
    func `'--json' keeps '... device apps list' output clean`() async throws {
        // TODO: enable with seeded managed-agent app fixtures (WDY-1952).
    }

    /** Missing target fails before prompts or alias-specific output. */
    @Test
    func `reports missing device without prompting`() async throws {
        try await self.scenario.run(authenticated: false) { cli, _ in
            try await cli.sh("wendy device ps --json") { result in
                #expect(result.status.isFailure)
                #expect(result.stdout == "")
                #expect(result.stderr.contains("no device specified"))
                #expect(!result.stderr.contains("Select a device"))
            }
        }
    }
}
