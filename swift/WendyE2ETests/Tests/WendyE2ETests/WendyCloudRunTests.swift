import Testing
import WendyE2ETesting

@Suite
struct `'wendy cloud run'` {
    let scenario = CLIAndAgentScenario()

    @Test
    func `prints command help`() async throws {
        try await self.scenario.run(authenticated: false) { cli, _ in
            try await cli.sh("wendy cloud run --help") { result in
                #expect(result.status.isSuccess)
                #expect(result.stdout.contains("Deprecated: use 'wendy run' instead"))
                #expect(result.stdout.contains("wendy cloud run [flags]"))
                #expect(result.stdout.contains("--build-type"))
                #expect(result.stdout.contains("--deploy"))
                #expect(result.stdout.contains("--detach"))
                #expect(result.stdout.contains("--user-args"))
                #expect(result.stdout.contains("--prefix"))
                #expect(result.stdout.contains("--device"))
                #expect(result.stderr == "")
            }
        }
    }

    @Test(
        .disabled(
            "WDY-1949/WDY-1950: cloud build/deploy/start needs isolated projects, builders, auth, tunnels, images, and a disposable managed-agent target."
        )
    )
    func `builds, deploys, and starts the current project`() async throws {}

    @Test(
        .disabled(
            "WDY-1949/WDY-1950: stopped cloud deployment needs an isolated project and disposable managed-agent container state."
        )
    )
    func `deploys without starting when requested`() async throws {}

    @Test(
        .disabled(
            "WDY-1949/WDY-1950: detached cloud startup needs an isolated built app and disposable managed-agent target."
        )
    )
    func `detaches after starting when requested`() async throws {}

    @Test(
        .disabled(
            "WDY-1949/WDY-1950: user-argument boundaries need an argv-reporting fixture app deployed through an isolated cloud tunnel."
        )
    )
    func `passes user arguments to the container`() async throws {}

    @Test(
        .disabled(
            "WDY-1949/WDY-1950: explicit project/device success needs isolated project, auth, tunnel, and managed-target fixtures."
        )
    )
    func `uses explicit project and device selection`() async throws {}

    @Test
    func `uses cloud authentication before connecting`() async throws {
        try await self.scenario.run(authenticated: false) { cli, _ in
            try await cli.sh("wendy cloud run --device target --json") { result in
                #expect(result.status.isFailure)
                #expect(result.stdout == "")
                #expect(result.stderr.contains("not logged in"))
                #expect(result.stderr.contains("wendy auth login"))
            }
        }
    }

    @Test(
        .disabled(
            "WDY-1949/WDY-1950: build/deployment failure cleanup needs controllable builder, tunnel, and managed-agent failure modes."
        )
    )
    func `reports build or deployment failure without claiming success`() async throws {}

    @Test(
        .disabled(
            "WDY-1909/WDY-1949/WDY-1950: cloud run lacks complete JSON results and needs isolated deployment fixtures to verify stream separation."
        )
    )
    func `prints JSON run metadata for automation`() async throws {}

    @Test
    func `rejects a missing project prefix before cloud access`() async throws {
        try await self.scenario.run(authenticated: false) { cli, _ in
            try await cli.sh("wendy cloud run --prefix missing-project") { result in
                #expect(result.status.isFailure)
                #expect(result.stdout == "")
                #expect(result.stderr.contains("resolving working directory"))
                #expect(result.stderr.contains("does not exist"))
                #expect(!result.stderr.contains("not logged in"))
            }
        }
    }

    @Test
    func `rejects undocumented flags`() async throws {
        try await self.scenario.run(authenticated: false) { cli, _ in
            try await cli.sh("wendy cloud run --bogus") { result in
                #expect(result.status.isFailure)
                #expect(result.stdout == "")
                #expect(result.stderr.contains("unknown flag"))
                #expect(result.stderr.contains("--bogus"))
            }
        }
    }

    @Test(
        .disabled(
            "WDY-1934: 'wendy cloud run' silently accepts positional arguments because the command has no cobra.NoArgs validator."
        )
    )
    func `rejects undocumented positional arguments`() async throws {}
}
