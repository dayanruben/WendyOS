import Testing
import WendyE2ETesting

@Suite
struct `'wendy device camera view'` {
    let scenario = CLIAndAgentScenario()

    @Test
    func `prints command help`() async throws {
        try await self.scenario.run(authenticated: false) { cli, _ in
            try await cli.sh("wendy device camera view --help") { result in
                #expect(result.status.isSuccess)
                #expect(result.stdout.contains("Stream H.264 video from a device camera"))
                #expect(result.stdout.contains("wendy device camera view [flags]"))
                #expect(result.stdout.contains("--id"))
                #expect(result.stdout.contains("--width"))
                #expect(result.stdout.contains("--height"))
                #expect(result.stdout.contains("--fps"))
                #expect(result.stdout.contains("--stdout"))
                #expect(result.stdout.contains("--device"))
                #expect(result.stderr == "")
            }
        }
    }

    @Test(
        .disabled(
            "WDY-1952: explicit-target viewing needs a seeded managed agent and simulated camera capability without physical hardware."
        )
    )
    func `uses explicit device selection without prompting`() async throws {}

    @Test
    func `reports missing device selection in non-interactive mode`() async throws {
        try await self.scenario.run(authenticated: false) { cli, _ in
            try await cli.sh("wendy device camera view --stdout --json") { result in
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
            "WDY-1952: camera playback needs seeded encoded frames plus controlled local viewer dependencies."
        )
    )
    func `streams camera video`() async throws {}

    @Test(
        .disabled(
            "WDY-1952: encoded stdout routing needs seeded frames and bounded stream process control."
        )
    )
    func `writes encoded video to stdout when requested`() async throws {}

    @Test(
        .disabled(
            "WDY-1958: semantic camera dimensions and frame rates are not validated locally before target connection/RPC."
        )
    )
    func `validates camera parameters before streaming`() async throws {}

    @Test(
        .disabled(
            "WDY-1952: viewer cancellation cleanup needs seeded streaming RPC state and harness process control."
        )
    )
    func `shuts down cleanly on cancellation`() async throws {}

    @Test
    func `rejects undocumented flags`() async throws {
        try await self.scenario.run(authenticated: false) { cli, _ in
            try await cli.sh("wendy device camera view --bogus") { result in
                #expect(result.status.isFailure)
                #expect(result.stdout == "")
                #expect(result.stderr.contains("unknown flag"))
            }
        }
    }

    @Test(
        .disabled(
            "WDY-1934: 'wendy device camera view' silently accepts positional arguments because the leaf command has no cobra.NoArgs validator."
        )
    )
    func `rejects undocumented positional arguments`() async throws {}
}
