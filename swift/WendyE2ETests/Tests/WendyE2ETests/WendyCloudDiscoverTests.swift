import Testing
import WendyE2ETesting

@Suite
struct `'wendy cloud discover'` {
    let scenario = CLIAndAgentScenario()

    @Test
    func `prints command help`() async throws {
        try await self.scenario.run(authenticated: false) { cli, _ in
            try await cli.sh("wendy cloud discover --help") { result in
                #expect(result.status.isSuccess)
                #expect(result.stdout.contains("List enrolled devices in Wendy Cloud"))
                #expect(result.stdout.contains("wendy cloud discover [flags]"))
                #expect(result.stdout.contains("--all"))
                #expect(result.stdout.contains("--broker-url"))
                #expect(result.stdout.contains("--cloud-grpc"))
                #expect(result.stdout.contains("--json"))
                #expect(result.stderr == "")
            }
        }
    }

    @Test(
        .disabled(
            "WDY-1949: enrolled-device discovery needs isolated cloud auth and seeded asset inventory."
        )
    )
    func `lists enrolled cloud devices`() async throws {}

    @Test(
        .disabled(
            "WDY-1949: offline-device filtering needs isolated cloud auth and seeded online/offline asset inventory."
        )
    )
    func `includes offline devices when requested`() async throws {}

    @Test
    func `reports missing auth before contacting discovery services`() async throws {
        try await self.scenario.run(authenticated: false) { cli, _ in
            try await cli.sh("wendy cloud discover --json") { result in
                #expect(result.status.isFailure)
                #expect(result.stdout == "")
                #expect(result.stderr.contains("not logged in"))
                #expect(result.stderr.contains("wendy auth login"))
            }
        }
    }

    @Test(
        .disabled(
            "WDY-1949: JSON cloud discovery schema needs isolated auth and seeded asset inventory."
        )
    )
    func `prints JSON cloud discovery results for automation`() async throws {}

    @Test
    func `reports invalid CLI configuration before acting`() async throws {
        try await self.scenario.run(authenticated: false) { cli, _ in
            try await cli.sh(
                posix:
                    "mkdir -p \"$HOME/.wendy\"; printf '{ broken\\n' > \"$HOME/.wendy/config.json\"",
                power: """
                    New-Item -ItemType Directory -Force -Path (Join-Path $env:HOME '.wendy') | Out-Null
                    Set-Content -NoNewline -LiteralPath (Join-Path $env:HOME '.wendy/config.json') -Value '{ broken'
                    """
            )
            try await cli.sh("wendy cloud discover --json") { result in
                #expect(result.status.isFailure)
                #expect(result.stdout == "")
                #expect(result.stderr.contains("parsing config"))
            }
            try await cli.sh(
                posix: "cat \"$HOME/.wendy/config.json\"",
                power: "Get-Content -Raw -LiteralPath (Join-Path $env:HOME '.wendy/config.json')"
            ) { result in
                #expect(result.stdout.contains("{ broken"))
            }
        }
    }

    @Test
    func `rejects undocumented flags`() async throws {
        try await self.scenario.run(authenticated: false) { cli, _ in
            try await cli.sh("wendy cloud discover --bogus") { result in
                #expect(result.status.isFailure)
                #expect(result.stdout == "")
                #expect(result.stderr.contains("unknown flag"))
                #expect(result.stderr.contains("--bogus"))
            }
        }
    }

    @Test(
        .disabled(
            "WDY-1934: 'wendy cloud discover' silently accepts positional arguments because the command has no cobra.NoArgs validator."
        )
    )
    func `rejects undocumented positional arguments`() async throws {}
}
