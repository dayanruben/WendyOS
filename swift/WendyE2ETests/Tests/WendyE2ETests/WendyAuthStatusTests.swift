import Testing
import WendyE2ETesting

@Suite
struct `'wendy auth status'` {
    let scenario = CLIAndAgentScenario()

    /** Displays local auth-status usage without reading credentials. */
    @Test
    func `prints command help`() async throws {
        try await self.scenario.run(authenticated: false) { cli, _ in
            try await cli.sh("wendy auth status --help") { result in
                #expect(result.status.isSuccess)
                #expect(result.stdout.contains("Show current authentication status"))
                #expect(result.stdout.contains("wendy auth status [flags]"))
                #expect(result.stdout.contains("--json"))
                #expect(result.stderr == "")
            }
        }
    }

    /** Reports logged-out state locally without creating a config file. */
    @Test
    func `prints logged-out status without contacting the cloud`() async throws {
        try await self.scenario.run(authenticated: false) { cli, _ in
            try await cli.sh("wendy auth status") { result in
                #expect(result.status.isSuccess)
                #expect(result.stdout.contains("Not logged in"))
                #expect(result.stdout.contains("wendy auth login"))
                #expect(result.stderr == "")
            }
            try await cli.sh(
                posix: "test ! -f \"$HOME/.wendy/config.json\"",
                power:
                    "if (Test-Path -LiteralPath (Join-Path $env:HOME '.wendy/config.json')) { throw 'config created' }"
            )
        }
    }

    /** Prints non-secret endpoint, user, and organization metadata from local config. */
    @Test
    func `prints logged-in status from stored credentials`() async throws {
        try await self.scenario.run(authenticated: false) { cli, _ in
            try await cli.sh(
                posix: """
                    mkdir -p "$HOME/.wendy"
                    printf '%s\n' '{"auth":[{"cloudDashboard":"https://fixture.invalid","cloudGRPC":"fixture.invalid:443","certificates":[{"organizationId":42,"userId":"fixture-user"}]}]}' > "$HOME/.wendy/config.json"
                    """,
                power: """
                    New-Item -ItemType Directory -Force -Path (Join-Path $env:HOME '.wendy') | Out-Null
                    Set-Content -LiteralPath (Join-Path $env:HOME '.wendy/config.json') -Value '{"auth":[{"cloudDashboard":"https://fixture.invalid","cloudGRPC":"fixture.invalid:443","certificates":[{"organizationId":42,"userId":"fixture-user"}]}]}'
                    """
            )
            try await cli.sh("wendy auth status") { result in
                #expect(result.status.isSuccess)
                #expect(result.stdout.contains("Cloud:  https://fixture.invalid"))
                #expect(result.stdout.contains("gRPC: fixture.invalid:443"))
                #expect(result.stdout.contains("User: fixture-user"))
                #expect(result.stdout.contains("Org:  42"))
                #expect(!result.stdout.lowercased().contains("private key"))
                #expect(result.stderr == "")
            }
        }
    }

    @Test(
        .disabled(
            "WDY-1948: auth status does not consistently classify malformed/incomplete certificate material; malformed PEM can be silently ignored."
        )
    )
    func `reports expired or unusable credentials clearly`() async throws {
        // TODO: enable when local credential states are explicit and actionable (WDY-1948).
    }

    @Test(
        .disabled(
            "WDY-1909: 'wendy auth status --json' ignores JSON mode and prints human status; WDY-1948 tracks missing structured credential validity states."
        )
    )
    func `prints JSON auth status for automation`() async throws {
        // TODO: enable when auth status implements structured JSON (WDY-1909, WDY-1948).
    }

    /** Malformed CLI config fails locally and remains unchanged. */
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
            try await cli.sh("wendy auth status") { result in
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

    /** Unknown flags fail before config access. */
    @Test
    func `rejects undocumented flags`() async throws {
        try await self.scenario.run(authenticated: false) { cli, _ in
            try await cli.sh("wendy auth status --bogus") { result in
                #expect(result.status.isFailure)
                #expect(result.stdout == "")
                #expect(result.stderr.contains("unknown flag"))
                #expect(result.stderr.contains("--bogus"))
            }
        }
    }

    @Test(
        .disabled(
            "WDY-1934: 'wendy auth status' silently accepts extra positional arguments because the leaf command has no cobra.NoArgs validator."
        )
    )
    func `rejects undocumented positional arguments`() async throws {
        // TODO: enable when auth status rejects positional arguments (WDY-1934).
    }
}
