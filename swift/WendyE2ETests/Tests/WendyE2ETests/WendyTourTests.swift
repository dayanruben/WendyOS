import Testing
import WendyE2ETesting

@Suite
struct `'wendy tour'` {
    let scenario = CLIAndAgentScenario()

    /** Displays guided-tour usage without starting terminal UI. */
    @Test
    func `prints command help`() async throws {
        try await self.scenario.run(authenticated: false) { cli, _ in
            try await cli.sh("wendy tour --help") { result in
                #expect(result.status.isSuccess)
                #expect(result.stdout.contains("Walk through device setup"))
                #expect(result.stdout.contains("wendy tour [flags]"))
                #expect(result.stdout.contains("--help"))
                #expect(result.stdout.contains("--json"))
                #expect(result.stderr == "")
            }
        }
    }

    @Test(
        .disabled(
            "WDY-1951: the multi-step Bubble Tea tour needs scripted PTY screen synchronization plus isolated auth, project, discovery, install, MCP, and deployment fixtures."
        )
    )
    func `runs the guided setup tour interactively`() async throws {
        // TODO: enable with the scripted isolated tour fixture (WDY-1951).
    }

    @Test(
        .disabled(
            "WDY-1951: completed-step coverage needs a scripted PTY and isolated fixture state for every downstream tour integration."
        )
    )
    func `skips completed steps using existing state`() async throws {
        // TODO: enable with the scripted isolated tour fixture (WDY-1951).
    }

    @Test(
        .disabled(
            "WDY-1951: cancellation checkpoints need PTY process control and observable isolated side effects to prove later steps did not run."
        )
    )
    func `cancels without continuing later side effects`() async throws {
        // TODO: enable with scripted cancellation control (WDY-1951).
    }

    /** Non-interactive invocation fails promptly with terminal guidance instead of blocking. */
    @Test
    func `runs safely in non-interactive contexts`() async throws {
        try await self.scenario.run(authenticated: false) { cli, _ in
            try await cli.sh("wendy tour") { result in
                #expect(result.status.isFailure)
                #expect(result.stdout == "")
                #expect(result.stderr.contains("wendy tour requires an interactive terminal"))
            }
        }
    }

    /** Unknown flags fail before terminal detection or tour side effects. */
    @Test
    func `rejects undocumented flags`() async throws {
        try await self.scenario.run(authenticated: false) { cli, _ in
            try await cli.sh("wendy tour --bogus") { result in
                #expect(result.status.isFailure)
                #expect(result.stdout == "")
                #expect(result.stderr.contains("unknown flag"))
                #expect(result.stderr.contains("--bogus"))
            }
        }
    }

    @Test(
        .disabled(
            "WDY-1934: 'wendy tour' silently accepts extra positional arguments because the command has no cobra.NoArgs validator."
        )
    )
    func `rejects undocumented positional arguments`() async throws {
        // TODO: enable when tour rejects positional arguments (WDY-1934).
    }
}
