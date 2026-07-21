import Foundation
import Testing
import WendyE2ETesting

@Suite
struct `'wendy os list-drives'` {
    let scenario = CLIAndAgentScenario()

    /** Displays drive-listing usage and flags without enumerating disks. */
    @Test
    func `prints command help`() async throws {
        try await self.scenario.run(authenticated: false) { cli, _ in
            try await cli.sh("wendy os list-drives --help") { result in
                let stdout = result.stdout
                #expect(result.status.isSuccess)
                #expect(stdout.contains("List available drives"))
                #expect(stdout.contains("wendy os list-drives [flags]"))
                #expect(stdout.contains("--all"))
                #expect(stdout.contains("--json"))
                #expect(result.stderr == "")
            }
        }
    }

    /**
     Lists only candidate external drives by default. The operation is
     read-only and an empty runner inventory is a successful result.
     */
    @Test
    func `lists removable drives by default`() async throws {
        try await self.scenario.run(authenticated: false) { cli, _ in
            try await cli.sh("wendy os list-drives --json") { result in
                #expect(result.status.isSuccess)
                #expect(result.stderr == "")
                let drives = try #require(
                    try JSONSerialization.jsonObject(with: Data(result.stdout.utf8))
                        as? [[String: Any]]
                )
                for drive in drives {
                    #expect((drive["id"] as? String)?.isEmpty == false)
                    #expect((drive["name"] as? String)?.isEmpty == false)
                    #expect(
                        (drive["capacity"] as? Int64) != nil || (drive["capacity"] as? Int) != nil
                    )
                    #expect(drive["isExternal"] as? Bool == true)
                }
            }
        }
    }

    /**
     `--all` safely inventories the broader drive set and marks each entry's
     external classification without writing to any disk.
     */
    @Test
    func `includes non-removable drives when requested`() async throws {
        try await self.scenario.run(authenticated: false) { cli, _ in
            try await cli.sh("wendy os list-drives --all --json") { result in
                #expect(result.status.isSuccess)
                #expect(result.stderr == "")
                let drives = try #require(
                    try JSONSerialization.jsonObject(with: Data(result.stdout.utf8))
                        as? [[String: Any]]
                )
                for drive in drives {
                    #expect(drive["isExternal"] is Bool)
                }
            }
        }
    }

    /**
     JSON inventory includes mount, removability, and explicit safety metadata
     in addition to stable identifiers, names, and capacity.
     */
    @Test(
        .disabled(
            "WDY-1946: list-drives JSON currently exposes only id, name, capacity, and isExternal; mount state, removability, and safety classification are absent."
        )
    )
    func `prints JSON drive inventory for automation`() async throws {
        // TODO: enable when structured drive safety metadata is available (WDY-1946).
    }

    /** Unknown flags fail before disk enumeration. */
    @Test
    func `rejects undocumented flags`() async throws {
        try await self.scenario.run(authenticated: false) { cli, _ in
            try await cli.sh("wendy os list-drives --bogus") { result in
                #expect(result.status.isFailure)
                #expect(result.stdout == "")
                #expect(result.stderr.contains("unknown flag"))
                #expect(result.stderr.contains("--bogus"))
            }
        }
    }

    /** Extra positional arguments are rejected before disk enumeration. */
    @Test(
        .disabled(
            "WDY-1934: 'wendy os list-drives' silently accepts extra positional arguments because the leaf command has no cobra.NoArgs validator."
        )
    )
    func `rejects undocumented positional arguments`() async throws {
        // TODO: enable when list-drives rejects positional arguments (WDY-1934).
    }
}
