import Testing
import WendyE2ETesting

@Suite
struct `'wendy build'` {
    let scenario = CLIAndAgentScenario()

    /** Displays builder selection usage without inspecting the project or toolchains. */
    @Test
    func `prints command help`() async throws {
        try await self.scenario.run(authenticated: false) { cli, _ in
            try await cli.sh("wendy build --help") { result in
                let stdout = result.stdout
                #expect(result.status.isSuccess)
                #expect(stdout.contains("Detects the project type"))
                #expect(stdout.contains("wendy build [flags]"))
                #expect(stdout.contains("--build-type"))
                #expect(stdout.contains("--builder"))
                #expect(stdout.contains("--dockerfile"))
                #expect(stdout.contains("--json"))
                #expect(result.stderr == "")
            }
        }
    }

    @Test(
        .disabled(
            "WDY-1950: successful builds need pinned sample projects, builders/toolchains, image namespaces, and cleanup rather than ambient runner installations."
        )
    )
    func `builds the project in the current directory`() async throws {
        // TODO: enable with isolated build fixtures (WDY-1950).
    }

    @Test(
        .disabled(
            "WDY-1950: overlapping-marker strategy coverage needs maintained Docker/Swift/Python fixtures and controlled builder availability."
        )
    )
    func `uses the requested build type when markers overlap`() async throws {
        // TODO: enable with overlapping project fixtures (WDY-1950).
    }

    /** A directory with no supported project marker fails without creating artifacts. */
    @Test
    func `reports missing project configuration`() async throws {
        try await self.scenario.run(authenticated: false) { cli, _ in
            try await cli.sh("wendy build") { result in
                #expect(result.status.isFailure)
                #expect(result.stdout == "")
                #expect(result.stderr.contains("no supported build type found"))
            }
            try await cli.sh(
                posix: "test -z \"$(find . -mindepth 1 -maxdepth 1 -print -quit)\"",
                power:
                    "if (Get-ChildItem -Force | Select-Object -First 1) { throw 'build artifacts created' }"
            )
        }
    }

    /** Invalid builder and mutually exclusive builder options fail before toolchain access. */
    @Test
    func `validates builder selection locally`() async throws {
        try await self.scenario.run(authenticated: false) { cli, _ in
            try await cli.sh("wendy build --builder nonsense") { result in
                #expect(result.status.isFailure)
                #expect(result.stdout == "")
                #expect(result.stderr.contains("invalid value \"nonsense\" for --builder"))
            }
            try await cli.sh("wendy build --dockerfile Dockerfile.prod --build-type swift") {
                result in
                #expect(result.status.isFailure)
                #expect(result.stdout == "")
                #expect(
                    result.stderr.contains("--dockerfile cannot be used with --build-type=swift")
                )
            }
        }
    }

    @Test(
        .disabled(
            "WDY-1909: 'wendy build' does not implement global --json; WDY-1950 tracks the isolated successful build fixture needed for metadata."
        )
    )
    func `prints JSON build metadata for automation`() async throws {
        // TODO: enable when build implements JSON and has isolated fixtures (WDY-1909, WDY-1950).
    }

    /** Unknown flags fail before project or toolchain access. */
    @Test
    func `rejects undocumented flags`() async throws {
        try await self.scenario.run(authenticated: false) { cli, _ in
            try await cli.sh("wendy build --bogus") { result in
                #expect(result.status.isFailure)
                #expect(result.stdout == "")
                #expect(result.stderr.contains("unknown flag"))
                #expect(result.stderr.contains("--bogus"))
            }
        }
    }

    @Test(
        .disabled(
            "WDY-1934: 'wendy build' silently accepts extra positional arguments because the command has no cobra.NoArgs validator."
        )
    )
    func `rejects undocumented positional arguments`() async throws {
        // TODO: enable when build rejects positional arguments (WDY-1934).
    }
}
