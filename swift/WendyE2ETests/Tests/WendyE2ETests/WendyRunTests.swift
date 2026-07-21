import Testing
import WendyE2ETesting

@Suite
struct `'wendy run'` {
    let scenario = CLIAndAgentScenario()

    /** Displays build, deploy, target, and log-stream controls without resolving a device. */
    @Test
    func `prints command help`() async throws {
        try await self.scenario.run(authenticated: false) { cli, _ in
            try await cli.sh("wendy run --help") { result in
                let stdout = result.stdout
                #expect(result.status.isSuccess)
                #expect(stdout.contains("Reads wendy.json"))
                #expect(stdout.contains("wendy run [flags]"))
                #expect(stdout.contains("--deploy"))
                #expect(stdout.contains("--detach"))
                #expect(stdout.contains("--user-args"))
                #expect(stdout.contains("--prefix"))
                #expect(stdout.contains("--device"))
                #expect(stdout.contains("--json"))
                #expect(result.stderr == "")
            }
        }
    }

    @Test(
        .disabled(
            "WDY-1950: end-to-end build/deploy/start needs pinned sample projects, isolated image namespaces, and a disposable managed-agent target."
        )
    )
    func `builds, deploys, and starts the current project`() async throws {
        // TODO: enable with isolated build and managed-deploy fixtures (WDY-1950).
    }

    @Test(
        .disabled(
            "WDY-1950: stopped deployment state needs a disposable managed agent with observable container lifecycle and cleanup."
        )
    )
    func `deploys without starting when requested`() async throws {
        // TODO: enable with the managed-deploy fixture (WDY-1950).
    }

    @Test(
        .disabled(
            "WDY-1950: detach success and follow-up log guidance need an isolated built app and disposable managed-agent target."
        )
    )
    func `detaches after starting when requested`() async throws {
        // TODO: enable with isolated build and managed-deploy fixtures (WDY-1950).
    }

    @Test(
        .disabled(
            "WDY-1950: user-argument boundary verification needs a fixture app that reports received argv from a disposable managed target."
        )
    )
    func `passes user arguments to the container`() async throws {
        // TODO: enable with an argv-reporting app fixture (WDY-1950).
    }

    @Test(
        .disabled(
            "WDY-1950: explicit prefix/device success needs isolated projects and a disposable managed target without physical device selection."
        )
    )
    func `uses explicit project and device selection`() async throws {
        // TODO: enable with isolated project and managed-target fixtures (WDY-1950).
    }

    @Test(
        .disabled(
            "WDY-1950: build/deploy failure cleanup needs controllable builder and managed-agent failure modes with observable remote resources."
        )
    )
    func `reports build or deployment failure without claiming success`() async throws {
        // TODO: enable with isolated build/deploy failure fixtures (WDY-1950).
    }

    @Test(
        .disabled(
            "WDY-1909: 'wendy run' does not implement a complete global --json result; WDY-1950 tracks the isolated deployment fixture needed to verify log separation."
        )
    )
    func `prints JSON run metadata for automation`() async throws {
        // TODO: enable when run JSON and managed-deploy fixtures exist (WDY-1909, WDY-1950).
    }

    /** Invalid local prefix and chunking options fail before device selection or build work. */
    @Test
    func `validates local options before selecting a device`() async throws {
        try await self.scenario.run(authenticated: false) { cli, _ in
            try await cli.sh("wendy run --prefix missing-project") { result in
                #expect(result.status.isFailure)
                #expect(result.stdout == "")
                #expect(result.stderr.contains("resolving working directory"))
                #expect(result.stderr.contains("does not exist"))
            }
            try await cli.sh("wendy run --chunking nonsense") { result in
                #expect(result.status.isFailure)
                #expect(result.stdout == "")
                #expect(result.stderr.contains("invalid --chunking value"))
                #expect(result.stderr.contains("auto, force, or off"))
            }
        }
    }

    /** Missing device selection fails without building or creating project artifacts. */
    @Test
    func `reports missing target before building`() async throws {
        try await self.scenario.run(authenticated: false) { cli, _ in
            try await cli.sh("wendy run") { result in
                #expect(result.status.isFailure)
                #expect(result.stdout == "")
                #expect(result.stderr.contains("no device specified"))
                #expect(result.stderr.contains("--device"))
            }
        }
    }

    /** Unknown flags fail before project, build, or device access. */
    @Test
    func `rejects unknown flags`() async throws {
        try await self.scenario.run(authenticated: false) { cli, _ in
            try await cli.sh("wendy run --bogus") { result in
                #expect(result.status.isFailure)
                #expect(result.stdout == "")
                #expect(result.stderr.contains("unknown flag"))
                #expect(result.stderr.contains("--bogus"))
            }
        }
    }

    @Test(
        .disabled(
            "WDY-1934: 'wendy run' silently accepts extra positional arguments because the command has no cobra.NoArgs validator."
        )
    )
    func `rejects undocumented positional arguments`() async throws {
        // TODO: enable when run rejects positional arguments (WDY-1934).
    }
}
