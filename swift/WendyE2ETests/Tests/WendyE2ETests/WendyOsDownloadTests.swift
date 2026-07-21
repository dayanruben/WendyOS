import Testing
import WendyE2ETesting

@Suite
struct `'wendy os download'` {
    let scenario = CLIAndAgentScenario()

    /** Displays image-download usage and cache controls without fetching a manifest. */
    @Test
    func `prints command help`() async throws {
        try await self.scenario.run(authenticated: false) { cli, _ in
            try await cli.sh("wendy os download --help") { result in
                let stdout = result.stdout
                #expect(result.status.isSuccess)
                #expect(stdout.contains("Download a WendyOS image"))
                #expect(stdout.contains("wendy os download [flags]"))
                #expect(stdout.contains("--version"))
                #expect(stdout.contains("--overwrite"))
                #expect(stdout.contains("--json"))
                #expect(result.stderr == "")
            }
        }
    }

    /** Downloads and verifies a pinned image into an isolated OS cache. */
    @Test(
        .disabled(
            "WDY-1944: deterministic download success requires a pinned local manifest/artifact server and checksum fixture rather than the mutable public catalog."
        )
    )
    func `downloads a selected WendyOS image into the cache`() async throws {
        // TODO: enable with the protected OS artifact fixture (WDY-1944).
    }

    /** Reuses a verified cached image unless overwrite is explicitly requested. */
    @Test(
        .disabled(
            "WDY-1944: cache-hit and overwrite coverage requires a pinned manifest plus known artifact/checksum bytes."
        )
    )
    func `uses cached images unless overwrite is requested`() async throws {
        // TODO: enable with the protected OS artifact fixture (WDY-1944).
    }

    /** Download and verification failures preserve an existing cached artifact. */
    @Test(
        .disabled(
            "WDY-1944: unavailable-version, network-failure, and checksum-failure paths need a controllable local manifest/artifact server."
        )
    )
    func `reports unavailable versions without changing the cache`() async throws {
        // TODO: enable with failure modes from the protected OS artifact fixture (WDY-1944).
    }

    /** Emits structured version, path, checksum, size, and cache-hit metadata. */
    @Test(
        .disabled(
            "WDY-1909: 'wendy os download' does not implement global --json; WDY-1944 tracks the deterministic artifact fixture needed to exercise a successful result."
        )
    )
    func `prints JSON download metadata for automation`() async throws {
        // TODO: enable when download implements JSON and has a pinned fixture (WDY-1909, WDY-1944).
    }

    /** Unknown flags fail before manifest or cache access. */
    @Test
    func `rejects undocumented flags`() async throws {
        try await self.scenario.run(authenticated: false) { cli, _ in
            try await cli.sh("wendy os download --bogus") { result in
                #expect(result.status.isFailure)
                #expect(result.stdout == "")
                #expect(result.stderr.contains("unknown flag"))
                #expect(result.stderr.contains("--bogus"))
            }
        }
    }

    /** Extra positional arguments are rejected before manifest or cache access. */
    @Test(
        .disabled(
            "WDY-1934: 'wendy os download' silently accepts extra positional arguments because the leaf command has no cobra.NoArgs validator."
        )
    )
    func `rejects undocumented positional arguments`() async throws {
        // TODO: enable when OS download rejects positional arguments (WDY-1934).
    }
}
