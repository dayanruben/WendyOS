import Testing
import WendyE2ETesting

@Suite
struct `'wendy os update'` {
    let scenario = CLIAndAgentScenario()

    /** Displays local, remote, nightly, and PR update modes without connecting to a device. */
    @Test
    func `prints command help`() async throws {
        try await self.scenario.run(authenticated: false) { cli, _ in
            try await cli.sh("wendy os update --help") { result in
                let stdout = result.stdout
                #expect(result.status.isSuccess)
                #expect(stdout.contains("Update WendyOS using an OS update artifact"))
                #expect(stdout.contains("wendy os update [artifact-path] [flags]"))
                #expect(stdout.contains("--artifact-url"))
                #expect(stdout.contains("--nightly"))
                #expect(stdout.contains("--pr"))
                #expect(stdout.contains("--json"))
                #expect(result.stderr == "")
            }
        }
    }

    /** Serves a pinned local Mender artifact to a hosted update-capable WendyOS target. */
    @Test(
        .disabled(
            "WDY-1944: local-artifact OTA success requires a hosted update-capable WendyOS fixture and pinned Mender artifact."
        )
    )
    func `updates WendyOS from a local artifact`() async throws {
        // TODO: enable with the protected hosted OTA fixture (WDY-1944).
    }

    /** Validates a remote artifact URL locally before contacting the target device. */
    @Test(
        .disabled(
            "WDY-1945: --artifact-url is not validated until after device connection and version RPCs, so malformed URLs cannot fail locally as specified."
        )
    )
    func `updates WendyOS from a remote artifact URL`() async throws {
        // TODO: enable when URL validation precedes device access (WDY-1945).
    }

    /** Resolves pinned nightly artifacts and applies them to a hosted OTA target. */
    @Test(
        .disabled(
            "WDY-1944: nightly resolution and update success need a pinned manifest plus hosted update-capable WendyOS fixture."
        )
    )
    func `uses nightly artifacts when requested`() async throws {
        // TODO: enable with protected manifest and OTA fixtures (WDY-1944).
    }

    /** Cleans up a temporary local artifact server when the hosted target is unreachable. */
    @Test(
        .disabled(
            "WDY-1944: cleanup observation needs a controllable hosted OTA target and local artifact-server fixture rather than a physical device."
        )
    )
    func `reports unreachable devices without serving stale artifacts`() async throws {
        // TODO: enable with protected OTA and artifact-server fixtures (WDY-1944).
    }

    /** Emits structured device, artifact, version, and request-status metadata. */
    @Test(
        .disabled(
            "WDY-1909: 'wendy os update' does not implement global --json; WDY-1944 tracks the hosted fixture needed for update metadata."
        )
    )
    func `prints JSON update metadata for automation`() async throws {
        // TODO: enable when update implements JSON and has a hosted fixture (WDY-1909, WDY-1944).
    }

    /** Rejects conflicting artifact sources before device access. */
    @Test
    func `rejects conflicting artifact sources locally`() async throws {
        try await self.scenario.run(authenticated: false) { cli, _ in
            try await cli.sh(
                "wendy os update local.mender --artifact-url https://example.invalid/update.mender"
            ) { result in
                #expect(result.status.isFailure)
                #expect(result.stdout == "")
                #expect(result.stderr.contains("either a local artifact path or --artifact-url"))
            }
            try await cli.sh("wendy os update local.mender --pr 123") { result in
                #expect(result.status.isFailure)
                #expect(result.stdout == "")
                #expect(result.stderr.contains("--pr cannot be combined"))
            }
        }
    }

    /** Rejects too many positional arguments and unknown flags before device access. */
    @Test
    func `rejects undocumented arguments and flags`() async throws {
        try await self.scenario.run(authenticated: false) { cli, _ in
            try await cli.sh("wendy os update first.mender second.mender") { result in
                #expect(result.status.isFailure)
                #expect(result.stdout == "")
                #expect(result.stderr.contains("accepts at most 1 arg"))
            }
            try await cli.sh("wendy os update --bogus") { result in
                #expect(result.status.isFailure)
                #expect(result.stdout == "")
                #expect(result.stderr.contains("unknown flag"))
                #expect(result.stderr.contains("--bogus"))
            }
        }
    }
}
