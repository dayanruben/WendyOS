import Testing
import WendyE2ETesting

@Suite
struct `'wendy auth login'` {
    let scenario = CLIAndAgentScenario()

    /** Displays browser and API-key login modes without opening a browser. */
    @Test
    func `prints command help`() async throws {
        try await self.scenario.run(authenticated: false) { cli, _ in
            try await cli.sh("wendy auth login --help") { result in
                let stdout = result.stdout
                #expect(result.status.isSuccess)
                #expect(stdout.contains("opens a browser for authentication"))
                #expect(stdout.contains("wendy auth login [flags]"))
                #expect(stdout.contains("--api-key"))
                #expect(stdout.contains("--cloud"))
                #expect(stdout.contains("--cloud-grpc"))
                #expect(stdout.contains("--org"))
                #expect(result.stderr == "")
            }
        }
    }

    @Test(
        .disabled(
            "WDY-1949: browser login needs a protected callback/token/PKI fixture and injectable browser; real browsers and personal cloud auth are prohibited."
        )
    )
    func `starts browser-based login and stores the auth session`() async throws {
        // TODO: enable with protected browser and auth fixtures (WDY-1949).
    }

    @Test(
        .disabled(
            "WDY-1949: API-key login needs an ephemeral protected pki-core endpoint with recorder redaction; production or personal keys cannot be used."
        )
    )
    func `logs in with an API key without opening a browser`() async throws {
        // TODO: enable with protected PKI and secret-redaction fixtures (WDY-1949).
    }

    @Test(
        .disabled(
            "WDY-1949: endpoint selection needs multiple protected cloud/PKI fixtures so stored identity can be verified without production auth."
        )
    )
    func `selects the intended cloud endpoint explicitly`() async throws {
        // TODO: enable with protected multi-cloud fixtures (WDY-1949).
    }

    @Test(
        .disabled(
            "WDY-1949: cancellation and timeout require controllable browser callback process state rather than opening a real browser."
        )
    )
    func `reports cancelled browser login without storing credentials`() async throws {
        // TODO: enable with the protected browser callback fixture (WDY-1949).
    }

    @Test(
        .disabled(
            "WDY-1949: atomic session replacement needs protected certificate issuance for two cloud identities without personal auth state."
        )
    )
    func `replaces an existing session for the same cloud`() async throws {
        // TODO: enable with protected multi-session auth fixtures (WDY-1949).
    }

    /** Malformed config fails before browser or network activity and remains unchanged. */
    @Test
    func `reports invalid CLI configuration before acting`() async throws {
        try await self.scenario.run(authenticated: false) { cli, _ in
            try await cli.sh(
                posix:
                    "mkdir -p \"$HOME/.wendy\"; printf '{ invalid json\\n' > \"$HOME/.wendy/config.json\"",
                power: """
                    New-Item -ItemType Directory -Force -Path (Join-Path $env:HOME '.wendy') | Out-Null
                    Set-Content -NoNewline -LiteralPath (Join-Path $env:HOME '.wendy/config.json') -Value '{ invalid json'
                    """
            )
            try await cli.sh("wendy auth login --api-key fixture-value --cloud-grpc localhost:1") {
                result in
                #expect(result.status.isFailure)
                #expect(result.stdout == "")
                #expect(result.stderr.contains("parsing config"))
                #expect(!result.stderr.contains("fixture-value"))
            }
            try await cli.sh(
                posix: "cat \"$HOME/.wendy/config.json\"",
                power: "Get-Content -Raw -LiteralPath (Join-Path $env:HOME '.wendy/config.json')"
            ) { result in
                #expect(result.stdout.contains("{ invalid json"))
            }
        }
    }

    /** API-key mode requires a local/cloud gRPC endpoint before network access. */
    @Test
    func `requires an endpoint for API key login`() async throws {
        try await self.scenario.run(authenticated: false) { cli, _ in
            try await cli.sh("wendy auth login --api-key fixture-value") { result in
                #expect(result.status.isFailure)
                #expect(result.stdout == "")
                #expect(result.stderr.contains("--cloud-grpc is required"))
                #expect(!result.stderr.contains("fixture-value"))
            }
        }
    }

    /** Unknown flags fail before browser or network access. */
    @Test
    func `rejects unknown flags`() async throws {
        try await self.scenario.run(authenticated: false) { cli, _ in
            try await cli.sh("wendy auth login --bogus") { result in
                #expect(result.status.isFailure)
                #expect(result.stdout == "")
                #expect(result.stderr.contains("unknown flag"))
                #expect(result.stderr.contains("--bogus"))
            }
        }
    }

    @Test(
        .disabled(
            "WDY-1934: 'wendy auth login' silently accepts extra positional arguments because the leaf command has no cobra.NoArgs validator."
        )
    )
    func `rejects undocumented positional arguments`() async throws {
        // TODO: enable when auth login rejects positional arguments (WDY-1934).
    }
}
