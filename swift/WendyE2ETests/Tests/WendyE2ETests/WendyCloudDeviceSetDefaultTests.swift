import Foundation
import Testing
import WendyE2ETesting

@Suite
struct `'wendy cloud device set-default'` {
    let scenario = CLIAndAgentScenario()

    /** Displays positional hostname usage without auth, discovery, or config access. */
    @Test
    func `prints command help`() async throws {
        try await self.scenario.run(authenticated: false) { cli, _ in
            try await cli.sh("wendy cloud device set-default --help") { result in
                #expect(result.status.isSuccess)
                #expect(result.stdout.contains("Set the default device hostname"))
                #expect(
                    result.stdout.contains(
                        "wendy cloud device set-default [hostname] [flags]"
                    )
                )
                #expect(result.stdout.contains("--cloud-grpc"))
                #expect(result.stdout.contains("--json"))
                #expect(result.stderr == "")
            }
        }
    }

    @Test(
        .disabled(
            "WDY-1943: omitted-hostname selection enters physical discovery/picker flow; deterministic non-interactive behavior needs injected discovery fixtures."
        )
    )
    func `reports missing device selection in non-interactive mode`() async throws {
        // TODO: enable with deterministic picker/discovery fixtures (WDY-1943).
    }

    /** Stores an offline hostname locally; cloud reachability pinning remains best-effort. */
    @Test
    func `saves the default device hostname without cloud authentication`() async throws {
        try await self.scenario.run(authenticated: false) { cli, _ in
            try await cli.sh("wendy cloud device set-default 127.0.0.1:1") { result in
                #expect(result.status.isSuccess)
                #expect(result.stdout.contains("Default device set to: 127.0.0.1:1"))
                #expect(result.stderr == "")
            }
            try await cli.sh(
                posix: "cat \"$HOME/.wendy/config.json\"",
                power: "Get-Content -Raw -LiteralPath (Join-Path $env:HOME '.wendy/config.json')"
            ) { result in
                #expect(result.stdout.contains("\"defaultDevice\": \"127.0.0.1:1\""))
            }
        }
    }

    /** Preserves unrelated known config while changing only the default hostname. */
    @Test
    func `preserves unrelated known configuration keys`() async throws {
        try await self.scenario.run(authenticated: false) { cli, _ in
            try await cli.sh(
                posix: """
                    mkdir -p "$HOME/.wendy"
                    printf '%s\n' '{"analytics":{"enabled":false},"lastCLIUpdateCheck":"2026-07-20T12:00:00Z","completionPromptDismissed":true}' > "$HOME/.wendy/config.json"
                    """,
                power: """
                    New-Item -ItemType Directory -Force -Path (Join-Path $env:HOME '.wendy') | Out-Null
                    Set-Content -LiteralPath (Join-Path $env:HOME '.wendy/config.json') -Value '{"analytics":{"enabled":false},"lastCLIUpdateCheck":"2026-07-20T12:00:00Z","completionPromptDismissed":true}'
                    """
            )
            try await cli.sh("wendy cloud device set-default 127.0.0.1:1") { result in
                #expect(result.status.isSuccess)
            }
            try await cli.sh(
                posix: "cat \"$HOME/.wendy/config.json\"",
                power: "Get-Content -Raw -LiteralPath (Join-Path $env:HOME '.wendy/config.json')"
            ) { result in
                let json = try #require(
                    try JSONSerialization.jsonObject(with: Data(result.stdout.utf8))
                        as? [String: Any]
                )
                #expect(json["defaultDevice"] as? String == "127.0.0.1:1")
                #expect((json["analytics"] as? [String: Any])?["enabled"] as? Bool == false)
                #expect(json["lastCLIUpdateCheck"] as? String == "2026-07-20T12:00:00Z")
                #expect(json["completionPromptDismissed"] as? Bool == true)
            }
        }
    }

    @Test(
        .disabled(
            "WDY-1940: typed config load/save discards unknown top-level keys during default-device mutation."
        )
    )
    func `preserves unknown future configuration keys`() async throws {
        // TODO: enable when config mutations round-trip unknown keys (WDY-1940).
    }

    @Test(
        .disabled(
            "WDY-1953: an explicit empty hostname is accepted, saved as an empty default, and reported as success instead of failing validation."
        )
    )
    func `requires a nonempty hostname`() async throws {
        // TODO: enable when empty/whitespace hostnames are rejected (WDY-1953).
    }

    /** Too many positional arguments and unknown flags fail before config mutation. */
    @Test
    func `rejects undocumented arguments and flags`() async throws {
        try await self.scenario.run(authenticated: false) { cli, _ in
            try await cli.sh("wendy cloud device set-default first second") { result in
                #expect(result.status.isFailure)
                #expect(result.stdout == "")
                #expect(result.stderr.contains("accepts at most 1 arg"))
            }
            try await cli.sh("wendy cloud device set-default --bogus") { result in
                #expect(result.status.isFailure)
                #expect(result.stdout == "")
                #expect(result.stderr.contains("unknown flag"))
            }
            try await cli.sh(
                posix: "test ! -f \"$HOME/.wendy/config.json\"",
                power:
                    "if (Test-Path -LiteralPath (Join-Path $env:HOME '.wendy/config.json')) { throw 'config created' }"
            )
        }
    }
}
